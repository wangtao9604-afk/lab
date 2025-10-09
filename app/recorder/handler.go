package main

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/go-sql-driver/mysql"

	"qywx/infrastructures/log"
	"qywx/infrastructures/mq/kmq"
	"qywx/infrastructures/utils"
	"qywx/models/recorder"
)

type Handler struct {
	repo *recorder.Repo
	dlq  *kmq.DLQ
}

func NewHandler(repo *recorder.Repo, dlq *kmq.DLQ) *Handler {
	return &Handler{
		repo: repo,
		dlq:  dlq,
	}
}

func (h *Handler) Handle(m *kafka.Message, ack func(success bool)) {
	t0 := time.Now()
	topic := *m.TopicPartition.Topic
	logger := log.GetInstance().Sugar

	// Ensure consume latency is always reported
	defer func() {
		ReportConsumeLatency(topic, time.Since(t0))
	}()

	// 1) Parse headers
	schema := header(m.Headers, "schema")
	userID := header(m.Headers, "user_id")

	if schema != "ipang.qa.v1" {
		logger.Warnf("invalid schema: schema=%s user_id=%s topic=%s", schema, userID, topic)
		if h.dlq != nil {
			_ = h.dlq.SendWithContext(m, "invalid_schema")
		}
		ReportBatchResult("decode_error")
		ack(true)
		return
	}

	if userID == "" {
		logger.Warnf("missing user_id: topic=%s", topic)
		if h.dlq != nil {
			_ = h.dlq.SendWithContext(m, "missing_user_id_header")
		}
		ReportBatchResult("decode_error")
		ack(true)
		return
	}

	// Prefer occurred header over Kafka message timestamp
	var occurredAt time.Time
	occ := parseUnix(header(m.Headers, "occurred"))
	if occ != 0 {
		occurredAt = utils.Unix(occ, 0)
	} else if !m.Timestamp.IsZero() {
		occurredAt = utils.ToShanghai(m.Timestamp)
	} else {
		occurredAt = utils.Now()
	}

	// 2) Deserialize QAs
	var qas []QA
	if err := json.Unmarshal(m.Value, &qas); err != nil {
		logger.Warnf("json unmarshal failed: user_id=%s schema=%s topic=%s error=%v", userID, schema, topic, err)
		if h.dlq != nil {
			_ = h.dlq.SendWithContext(m, "json_unmarshal_failed")
		}
		ReportBatchResult("decode_error")
		ack(true)
		return
	}

	// Report QA count
	ReportQAsParsed(len(qas))

	// 3) Build rows
	rows := make([]recorder.ConversationRecord, 0, 2*len(qas))
	for _, qa := range qas {
		if s := strings.TrimSpace(qa.Q); s != "" {
			rows = append(rows, recorder.ConversationRecord{
				UserID:   userID,
				Source:   SourceAI,
				Content:  s,
				Occurred: occurredAt,
			})
		}
		if s := strings.TrimSpace(qa.A); s != "" {
			rows = append(rows, recorder.ConversationRecord{
				UserID:   userID,
				Source:   SourceUser,
				Content:  s,
				Occurred: occurredAt,
			})
		}
	}

	if len(rows) == 0 {
		ReportBatchResult("ok")
		ack(true)
		return
	}

	// 4) DB batch insert with retry
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dbT0 := time.Now()
	inserted, ignored, didRetry, err := h.insertWithRetry(ctx, rows, 300)
	dbLatency := time.Since(dbT0)

	ReportDBInsertLatency(dbLatency)

	if err != nil {
		logger.Errorf("mysql insert failed after retry: user_id=%s qa_count=%d rows=%d occurred=%s error=%v",
			userID, len(qas), len(rows), occurredAt.Format(time.RFC3339), err)
		if h.dlq != nil {
			_ = h.dlq.SendWithContext(m, "mysql_write_failed:"+err.Error())
		}
		ReportDBInsertBatchResult("error")
		ReportDBInsertRows("error", int64(len(rows)))
		ReportBatchResult("db_error")
		ack(true)
		return
	}

	// 5) Success - commit first, then report metrics
	ack(true)

	// 6) Report metrics after successful commit
	if didRetry {
		ReportDBInsertBatchResult("retry_ok")
	} else {
		ReportDBInsertBatchResult("ok")
	}
	ReportDBInsertRows("inserted", inserted)
	ReportDBInsertRows("ignored", ignored)
	ReportBatchResult("ok")
}

// insertWithRetry attempts DB insert with retry logic for transient errors.
// Returns: inserted, ignored, didRetry, error
func (h *Handler) insertWithRetry(ctx context.Context, rows []recorder.ConversationRecord, batchSize int) (int64, int64, bool, error) {
	const maxRetries = 3
	var lastErr error
	didRetry := false

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			didRetry = true
			// Exponential backoff with jitter: 100ms, 200ms, 400ms (±20%)
			baseBackoff := time.Duration(100*(1<<uint(attempt-1))) * time.Millisecond
			jitter := time.Duration(float64(baseBackoff) * 0.2 * (rand.Float64()*2 - 1)) // ±20%
			backoff := baseBackoff + jitter

			select {
			case <-ctx.Done():
				return 0, 0, didRetry, ctx.Err()
			case <-time.After(backoff):
			}
		}

		inserted, ignored, err := h.repo.InsertBatchWithStats(ctx, rows, batchSize)
		if err == nil {
			return inserted, ignored, didRetry, nil
		}

		lastErr = err

		// Classify error for retry decision
		if isDeadlock(err) {
			ReportDBRetry("deadlock")
			continue // Retry deadlocks
		}

		if isTimeout(err) {
			ReportDBRetry("timeout")
			continue // Retry timeouts
		}

		// Other errors - don't retry (no metric report as this is not a retry)
		break
	}

	return 0, 0, didRetry, lastErr
}

// isDeadlock checks if error is a MySQL deadlock
func isDeadlock(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		// 1213: Deadlock found when trying to get lock
		return mysqlErr.Number == 1213
	}
	return strings.Contains(err.Error(), "Deadlock") || strings.Contains(err.Error(), "deadlock")
}

// isTimeout checks if error is a timeout or transient connection issue
func isTimeout(err error) bool {
	if err == nil {
		return false
	}

	// Context deadline exceeded
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// MySQL specific timeout errors
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case 1205: // ER_LOCK_WAIT_TIMEOUT
			return true
		case 2006: // CR_SERVER_GONE_ERROR
			return true
		case 2013: // CR_SERVER_LOST
			return true
		}
	}

	// Driver-level bad connection (transient network issues)
	if errors.Is(err, driver.ErrBadConn) {
		return true
	}

	// String matching as fallback
	errStr := err.Error()
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "Timeout") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset")
}

func header(hs []kafka.Header, key string) string {
	for _, h := range hs {
		if strings.EqualFold(h.Key, key) {
			return string(h.Value)
		}
	}
	return ""
}

func parseUnix(s string) int64 {
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
