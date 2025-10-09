package recorder

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ConversationRecord struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	UserID    string    `gorm:"column:user_id;type:varchar(64);not null"`
	Source    string    `gorm:"column:source;type:enum('USER','AI');not null"`
	Content   string    `gorm:"column:content;type:mediumtext;not null"`
	Occurred  time.Time `gorm:"column:occurred;type:datetime(3);not null"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime(3)"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:datetime(3)"`
}

func (ConversationRecord) TableName() string {
	return "conversation_records"
}

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) InsertBatch(ctx context.Context, rows []ConversationRecord, batchSize int) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(rows, batchSize).
		Error
}

// InsertBatchWithStats inserts records in batches and returns statistics.
// Returns: rowsAffected (actually inserted), ignored (OnConflict skipped), error
func (r *Repo) InsertBatchWithStats(ctx context.Context, rows []ConversationRecord, batchSize int) (int64, int64, error) {
	if len(rows) == 0 {
		return 0, 0, nil
	}

	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(rows, batchSize)

	if result.Error != nil {
		return 0, 0, result.Error
	}

	rowsAffected := result.RowsAffected
	ignored := int64(len(rows)) - rowsAffected

	return rowsAffected, ignored, nil
}
