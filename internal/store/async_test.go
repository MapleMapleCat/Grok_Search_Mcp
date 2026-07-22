package store

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
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

func TestAsyncUsageWriterStatsAllowUntrackedRecordsAfterMetricsEnable(t *testing.T) {
	blockingStore := &blockingUsageStore{
		started: make(chan struct{}, 1),
		unblock: make(chan struct{}),
	}
	writer := NewAsyncUsageWriter(blockingStore, 2)
	var releaseStoreOnce sync.Once
	t.Cleanup(func() {
		releaseStoreOnce.Do(func() { close(blockingStore.unblock) })
		writer.Close()
	})

	writer.Enqueue(UsageRecord{KeyID: "in-flight", ToolName: "grok_web_search"})
	select {
	case <-blockingStore.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async writer to start blocked write")
	}

	// This record has no metrics timestamp because it enters while collection is disabled.
	writer.Enqueue(UsageRecord{KeyID: "untracked-queued", ToolName: "grok_web_search"})
	writer.SetMetricsEnabled(true)
	writer.Enqueue(UsageRecord{KeyID: "tracked-behind-untracked", ToolName: "grok_web_search"})

	stats := writer.Stats()
	if stats.QueueLength != 2 {
		t.Fatalf("queue length = %d, want 2", stats.QueueLength)
	}
	if stats.OldestQueuedAgeMs != 0 {
		t.Fatalf("oldest queued age = %f ms, want 0 while the queue head is untracked", stats.OldestQueuedAgeMs)
	}
}

func TestAsyncUsageWriterStatsTrackDroppedRecords(t *testing.T) {
	blockingStore := &blockingUsageStore{
		started: make(chan struct{}, 1),
		unblock: make(chan struct{}),
	}
	writer := NewAsyncUsageWriter(blockingStore, 1)
	writer.SetMetricsEnabled(true)

	writer.Enqueue(UsageRecord{KeyID: "key-1", ToolName: "grok_web_search"})
	select {
	case <-blockingStore.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async writer to start blocked write")
	}

	writer.Enqueue(UsageRecord{KeyID: "key-2", ToolName: "grok_web_search"})
	droppedCapture, err := os.CreateTemp(t.TempDir(), "queue-full-debug-*.body")
	if err != nil {
		t.Fatal(err)
	}
	droppedCapturePath := droppedCapture.Name()
	if err := droppedCapture.Close(); err != nil {
		t.Fatal(err)
	}
	writer.Enqueue(UsageRecord{
		KeyID:    "key-3",
		ToolName: "grok_web_search",
		Cleanup: func() {
			_ = os.Remove(droppedCapturePath)
		},
	})
	writer.Enqueue(UsageRecord{KeyID: "key-4", ToolName: "grok_x_search"})
	if _, err := os.Stat(droppedCapturePath); !os.IsNotExist(err) {
		t.Fatalf("queue-full capture was not removed: %v", err)
	}

	stats := writer.Stats()
	if stats.DroppedRecords != 2 {
		t.Fatalf("dropped records = %d, want 2", stats.DroppedRecords)
	}
	if stats.QueueCapacity != 1 {
		t.Fatalf("queue capacity = %d, want 1", stats.QueueCapacity)
	}

	close(blockingStore.unblock)
	writer.Close()
}

func TestAsyncUsageWriterRateLimitsQueueFullLogs(t *testing.T) {
	blockingStore := &blockingUsageStore{
		started: make(chan struct{}, 1),
		unblock: make(chan struct{}),
	}
	writer := NewAsyncUsageWriter(blockingStore, 1)
	writer.SetMetricsEnabled(true)

	writer.Enqueue(UsageRecord{KeyID: "in-flight", ToolName: "grok_web_search"})
	select {
	case <-blockingStore.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async writer to start blocked write")
	}
	writer.Enqueue(UsageRecord{KeyID: "queued", ToolName: "grok_web_search"})

	var logOutput bytes.Buffer
	originalLogWriter := log.Writer()
	log.SetOutput(&logOutput)
	t.Cleanup(func() {
		log.SetOutput(originalLogWriter)
	})

	const droppedRequestCount = 20
	for requestIndex := 0; requestIndex < droppedRequestCount; requestIndex++ {
		writer.Enqueue(UsageRecord{KeyID: "dropped", ToolName: "grok_web_search"})
	}

	if stats := writer.Stats(); stats.DroppedRecords != droppedRequestCount {
		t.Fatalf("dropped records = %d, want %d", stats.DroppedRecords, droppedRequestCount)
	}
	if logCount := strings.Count(logOutput.String(), "usage record dropped (buffer full)"); logCount != 1 {
		t.Fatalf("queue-full log count = %d, want 1; logs=%q", logCount, logOutput.String())
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
	temporaryCapture, err := os.CreateTemp(t.TempDir(), "write-failure-debug-*.body")
	if err != nil {
		t.Fatal(err)
	}
	temporaryCapturePath := temporaryCapture.Name()
	if err := temporaryCapture.Close(); err != nil {
		t.Fatal(err)
	}

	writer := NewAsyncUsageWriter(failingUsageStore{recordUsageErr: errors.New("db unavailable")}, 1)
	writer.SetMetricsEnabled(true)
	writer.Enqueue(UsageRecord{
		KeyID:    "key-1",
		ToolName: "grok_web_search",
		Cleanup: func() {
			_ = os.Remove(temporaryCapturePath)
		},
	})
	writer.Close()

	stats := writer.Stats()
	if stats.WriteFailures != 1 {
		t.Fatalf("write failures = %d, want 1", stats.WriteFailures)
	}
	if stats.WriteSuccesses != 0 {
		t.Fatalf("write successes = %d, want 0", stats.WriteSuccesses)
	}
	if _, err := os.Stat(temporaryCapturePath); !os.IsNotExist(err) {
		t.Fatalf("write-failure capture was not removed: %v", err)
	}
}

func TestAsyncUsageWriterCleansCaptureRejectedAfterClose(t *testing.T) {
	writer := NewAsyncUsageWriter(TestStore{}, 1)
	writer.Close()

	temporaryCapture, err := os.CreateTemp(t.TempDir(), "post-close-debug-*.body")
	if err != nil {
		t.Fatal(err)
	}
	temporaryCapturePath := temporaryCapture.Name()
	if err := temporaryCapture.Close(); err != nil {
		t.Fatal(err)
	}
	writer.Enqueue(UsageRecord{
		KeyID:    "post-close",
		ToolName: "grok_web_search",
		Cleanup: func() {
			_ = os.Remove(temporaryCapturePath)
		},
	})
	if _, err := os.Stat(temporaryCapturePath); !os.IsNotExist(err) {
		t.Fatalf("post-close capture was not removed: %v", err)
	}
}

type deadlineAwareUsageStore struct {
	TestStore
	started chan struct{}
}

func (s *deadlineAwareUsageStore) RecordUsage(ctx context.Context, _ UsageRecord) error {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestAsyncUsageWriterAppliesPerWriteDeadline(t *testing.T) {
	deadlineStore := &deadlineAwareUsageStore{started: make(chan struct{}, 1)}
	writer := newAsyncUsageWriter(deadlineStore, 1, 30*time.Millisecond, 250*time.Millisecond)
	writer.SetMetricsEnabled(true)
	writer.Enqueue(UsageRecord{KeyID: "key-1", ToolName: "grok_web_search"})

	startedAt := time.Now()
	writer.Close()
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("Close took %s, expected the write deadline to bound it", elapsed)
	}
	if stats := writer.Stats(); stats.WriteFailures != 1 {
		t.Fatalf("write failures = %d, want 1", stats.WriteFailures)
	}
}

func TestAsyncUsageWriterCloseIsBoundedAndCleansQueuedCapture(t *testing.T) {
	blockingStore := &blockingUsageStore{
		started: make(chan struct{}, 1),
		unblock: make(chan struct{}),
	}
	writer := newAsyncUsageWriter(blockingStore, 2, 20*time.Millisecond, 50*time.Millisecond)
	writer.SetMetricsEnabled(true)
	writer.Enqueue(UsageRecord{KeyID: "in-flight", ToolName: "grok_web_search"})
	select {
	case <-blockingStore.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the in-flight write")
	}

	temporaryCapture, err := os.CreateTemp(t.TempDir(), "queued-debug-*.json")
	if err != nil {
		t.Fatal(err)
	}
	temporaryCapturePath := temporaryCapture.Name()
	if err := temporaryCapture.Close(); err != nil {
		t.Fatal(err)
	}
	writer.Enqueue(UsageRecord{
		KeyID:    "queued",
		ToolName: "grok_x_search",
		Cleanup: func() {
			_ = os.Remove(temporaryCapturePath)
		},
	})

	startedAt := time.Now()
	writer.Close()
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("Close took %s, want a bounded shutdown", elapsed)
	}
	if _, err := os.Stat(temporaryCapturePath); !os.IsNotExist(err) {
		t.Fatalf("queued temporary capture was not removed, stat error: %v", err)
	}
	if stats := writer.Stats(); stats.DroppedRecords != 1 {
		t.Fatalf("dropped records = %d, want 1 queued record discarded at shutdown", stats.DroppedRecords)
	}

	close(blockingStore.unblock)
	select {
	case <-writer.workerDone:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after the blocking store returned")
	}
}

func TestAsyncUsageWriterCloseRacesSafelyWithAdmission(t *testing.T) {
	writer := newAsyncUsageWriter(TestStore{}, 8, 100*time.Millisecond, 250*time.Millisecond)
	const enqueueCount = 128
	start := make(chan struct{})
	var operations sync.WaitGroup
	var cleanupCount atomic.Int64

	operations.Add(enqueueCount + 1)
	for recordIndex := 0; recordIndex < enqueueCount; recordIndex++ {
		go func() {
			defer operations.Done()
			<-start
			writer.Enqueue(UsageRecord{
				KeyID:    "racing-key",
				ToolName: "grok_web_search",
				Cleanup: func() {
					cleanupCount.Add(1)
				},
			})
		}()
	}
	go func() {
		defer operations.Done()
		<-start
		writer.Close()
	}()

	close(start)
	operations.Wait()
	writer.Close()
	if got := cleanupCount.Load(); got != enqueueCount {
		t.Fatalf("cleanup count = %d, want %d", got, enqueueCount)
	}
}

type recordingBatchUsageStore struct {
	TestStore
	mu      sync.Mutex
	batches [][]UsageRecord
	err     error
	written chan struct{}
}

func TestAsyncUsageWriterMetricsAreDisabledByDefault(t *testing.T) {
	batchStore := &recordingBatchUsageStore{written: make(chan struct{}, 1)}
	writer := newAsyncUsageWriterWithBatch(batchStore, 1, time.Second, time.Second, 1, time.Millisecond)

	writer.Enqueue(UsageRecord{KeyID: "default-off-key", ToolName: "grok_web_search"})
	select {
	case <-batchStore.written:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for usage persistence")
	}
	writer.Close()

	if batches := batchStore.snapshotBatches(); len(batches) != 1 || len(batches[0]) != 1 {
		t.Fatalf("usage was not persisted while metrics were disabled: %+v", batches)
	}
	if stats := writer.Stats(); !reflect.DeepEqual(stats, AsyncUsageWriterStats{}) {
		t.Fatalf("metrics collected while disabled: %+v", stats)
	}
}

func (store *recordingBatchUsageStore) RecordUsageBatch(_ context.Context, records []UsageRecord) error {
	copiedRecords := append([]UsageRecord(nil), records...)
	store.mu.Lock()
	store.batches = append(store.batches, copiedRecords)
	store.mu.Unlock()
	select {
	case store.written <- struct{}{}:
	default:
	}
	return store.err
}

func (store *recordingBatchUsageStore) snapshotBatches() [][]UsageRecord {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([][]UsageRecord(nil), store.batches...)
}

func TestAsyncUsageWriterPersistsFullBatchInOneStoreCall(t *testing.T) {
	batchStore := &recordingBatchUsageStore{written: make(chan struct{}, 1)}
	writer := newAsyncUsageWriterWithBatch(batchStore, 8, time.Second, time.Second, 4, time.Second)
	writer.SetMetricsEnabled(true)

	for recordIndex := 0; recordIndex < 4; recordIndex++ {
		writer.Enqueue(UsageRecord{KeyID: "batch-key", ToolName: "grok_web_search"})
	}
	select {
	case <-batchStore.written:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for usage batch")
	}
	writer.Close()

	batches := batchStore.snapshotBatches()
	if len(batches) != 1 || len(batches[0]) != 4 {
		t.Fatalf("persisted batches = %+v, want one four-record batch", batches)
	}
	stats := writer.Stats()
	if stats.WriteBatches != 1 || stats.BatchedRecords != 4 || stats.WriteSuccesses != 4 {
		t.Fatalf("unexpected batch stats: %+v", stats)
	}
}

func TestAsyncUsageWriterFlushesPartialBatchAfterBoundedWait(t *testing.T) {
	batchStore := &recordingBatchUsageStore{written: make(chan struct{}, 1)}
	writer := newAsyncUsageWriterWithBatch(batchStore, 8, time.Second, time.Second, 8, 20*time.Millisecond)
	defer writer.Close()

	startedAt := time.Now()
	writer.Enqueue(UsageRecord{KeyID: "partial-key", ToolName: "grok_x_search"})
	select {
	case <-batchStore.written:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for partial usage batch")
	}
	if elapsed := time.Since(startedAt); elapsed < 10*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("partial batch flush delay = %s, want bounded wait near 20ms", elapsed)
	}

	batches := batchStore.snapshotBatches()
	if len(batches) != 1 || len(batches[0]) != 1 {
		t.Fatalf("persisted batches = %+v, want one single-record batch", batches)
	}
}

func TestAsyncUsageWriterFailedBatchCountsAndCleansEveryRecord(t *testing.T) {
	batchStore := &recordingBatchUsageStore{
		err:     errors.New("batch database unavailable"),
		written: make(chan struct{}, 1),
	}
	writer := newAsyncUsageWriterWithBatch(batchStore, 8, time.Second, time.Second, 3, time.Second)
	writer.SetMetricsEnabled(true)
	var cleanupCount atomic.Int64
	for recordIndex := 0; recordIndex < 3; recordIndex++ {
		writer.Enqueue(UsageRecord{
			KeyID:    "failed-batch-key",
			ToolName: "grok_web_search",
			Cleanup: func() {
				cleanupCount.Add(1)
			},
		})
	}
	writer.Close()

	stats := writer.Stats()
	if stats.WriteFailures != 3 || stats.FailedBatches != 1 || stats.WriteSuccesses != 0 {
		t.Fatalf("unexpected failed batch stats: %+v", stats)
	}
	if cleanupCount.Load() != 3 {
		t.Fatalf("cleanup count = %d, want 3", cleanupCount.Load())
	}
}
