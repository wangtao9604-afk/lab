package kmq

import (
	"testing"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type stubStoreCommit struct {
	stored    []kafka.TopicPartition
	committed []kafka.TopicPartition
}

func (s *stubStoreCommit) StoreOffsets(tps []kafka.TopicPartition) ([]kafka.TopicPartition, error) {
	s.stored = append(s.stored, tps...)
	return tps, nil
}

func (s *stubStoreCommit) CommitOffsets(tps []kafka.TopicPartition) ([]kafka.TopicPartition, error) {
	s.committed = append(s.committed, tps...)
	return tps, nil
}

func TestPartitionCommitGateSequential(t *testing.T) {
	gate := NewPartitionCommitGate("topic", 2, 0)
	stub := &stubStoreCommit{}

	// 标记偏移 0 完成，并尝试存储连续偏移。
	gate.MarkDone(0)

	if backlog := gate.GetBacklog(); backlog != 1 {
		t.Fatalf("expected backlog 1, got %d", backlog)
	}

	stored, err := gate.StoreContiguous(stub)
	if err != nil {
		t.Fatalf("StoreContiguous failed: %v", err)
	}
	if stored != 1 {
		t.Fatalf("expected stored offset 1, got %d", stored)
	}

	if len(stub.stored) != 1 {
		t.Fatalf("expected 1 stored record, got %d", len(stub.stored))
	}
	if stub.stored[0].Offset != 1 {
		t.Fatalf("expected stored offset value 1, got %v", stub.stored[0].Offset)
	}

	// 存储完成后，积压应为 0。
	if backlog := gate.GetBacklog(); backlog != 0 {
		t.Fatalf("expected backlog 0 after store, got %d", backlog)
	}

	// 先标记偏移 2 完成，验证闸门会等待偏移 1。
	gate.MarkDone(2)
	if backlog := gate.GetBacklog(); backlog != 1 {
		t.Fatalf("expected backlog 1 (offset 2 pending), got %d", backlog)
	}

	stored, err = gate.StoreContiguous(stub)
	if err != nil {
		t.Fatalf("StoreContiguous failed for non-contiguous offsets: %v", err)
	}
	if stored != -1 {
		t.Fatalf("expected no progress for non-contiguous completion, got %d", stored)
	}

	// 再完成偏移 1，应推进至偏移 2 之后。
	gate.MarkDone(1)
	stored, err = gate.StoreContiguous(stub)
	if err != nil {
		t.Fatalf("StoreContiguous failed: %v", err)
	}
	if stored != 3 {
		t.Fatalf("expected stored offset 3 (next offset), got %d", stored)
	}

	if len(stub.stored) != 2 {
		t.Fatalf("expected 2 stored batches, got %d", len(stub.stored))
	}
	if stub.stored[1].Offset != 3 {
		t.Fatalf("expected second stored offset 3, got %v", stub.stored[1].Offset)
	}
}

func TestPartitionCommitGateEnsureInit(t *testing.T) {
	gate := NewPartitionCommitGate("topic", 1, 0)
	gate.EnsureInit(5)

	gate.MarkDone(5)
	stub := &stubStoreCommit{}
	stored, err := gate.StoreContiguous(stub)
	if err != nil {
		t.Fatalf("StoreContiguous failed: %v", err)
	}
	if stored != 6 {
		t.Fatalf("expected stored offset 6 after calibration, got %d", stored)
	}
}

func TestPartitionCommitGateCommitContiguous(t *testing.T) {
	gate := NewPartitionCommitGate("topic", 3, 0)
	stub := &stubStoreCommit{}

	gate.MarkDone(0)
	if err := gate.CommitContiguous(stub); err != nil {
		t.Fatalf("CommitContiguous failed: %v", err)
	}
	if len(stub.committed) != 1 {
		t.Fatalf("expected 1 committed record, got %d", len(stub.committed))
	}
	if stub.committed[0].Offset != 1 {
		t.Fatalf("expected commit offset 1, got %v", stub.committed[0].Offset)
	}
}
