package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

type blockingUsageStore struct {
	TestStore
	started chan struct{}
	unblock chan struct{}
}

func (s *blockingUsageStore) RecordUsage(context.Context, UsageRecord) error {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-s.unblock
	return nil
}

func TestAsyncUsageWriterStatsTrackDroppedRecords(t *testing.T) {
	blockingStore := &blockingUsageStore{
		started: make(chan struct{}, 1),
		unblock: make(chan struct{}),
	}
	writer := NewAsyncUsageWriter(blockingStore, 1)

	writer.Enqueue(UsageRecord{KeyID: "key-1", ToolName: "grok_web_search"})
	select {
	case <-blockingStore.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async writer to start blocked write")
	}

	writer.Enqueue(UsageRecord{KeyID: "key-2", ToolName: "grok_web_search"})
	writer.Enqueue(UsageRecord{KeyID: "key-3", ToolName: "grok_web_search"})
	writer.Enqueue(UsageRecord{KeyID: "key-4", TouchKey: true})

	stats := writer.Stats()
	if stats.DroppedRecords != 1 {
		t.Fatalf("dropped records = %d, want 1", stats.DroppedRecords)
	}
	if stats.DroppedTouches != 1 {
		t.Fatalf("dropped touches = %d, want 1", stats.DroppedTouches)
	}
	if stats.QueueCapacity != 1 {
		t.Fatalf("queue capacity = %d, want 1", stats.QueueCapacity)
	}

	close(blockingStore.unblock)
	writer.Close()
}

type failingUsageStore struct {
	TestStore
	recordUsageErr error
}

func (s failingUsageStore) RecordUsage(context.Context, UsageRecord) error {
	return s.recordUsageErr
}

func TestAsyncUsageWriterStatsTrackWriteFailures(t *testing.T) {
	writer := NewAsyncUsageWriter(failingUsageStore{recordUsageErr: errors.New("db unavailable")}, 1)
	writer.Enqueue(UsageRecord{KeyID: "key-1", ToolName: "grok_web_search"})
	writer.Close()

	stats := writer.Stats()
	if stats.WriteFailures != 1 {
		t.Fatalf("write failures = %d, want 1", stats.WriteFailures)
	}
	if stats.WriteSuccesses != 0 {
		t.Fatalf("write successes = %d, want 0", stats.WriteSuccesses)
	}
}
