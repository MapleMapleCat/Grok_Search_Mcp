package store

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultAsyncUsageWriteTimeout = 2 * time.Second
	defaultAsyncUsageCloseTimeout = 3 * time.Second
	defaultAsyncUsageBatchSize    = 32
	defaultAsyncUsageBatchWait    = 10 * time.Millisecond
	asyncUsageCancellationGrace   = 250 * time.Millisecond
	asyncUsageDropLogInterval     = time.Second
)

type usageBatchRecorder interface {
	RecordUsageBatch(ctx context.Context, records []UsageRecord) error
}

type queuedUsageRecord struct {
	record     UsageRecord
	enqueuedAt time.Time
}

// AsyncUsageWriter 将用量写入从请求路径解耦：主线程只入队，后台 goroutine 调用 Store。
type AsyncUsageWriter struct {
	store        Store
	ch           chan queuedUsageRecord
	writeTimeout time.Duration
	closeTimeout time.Duration
	batchSize    int
	batchWait    time.Duration
	cancelWorker context.CancelFunc
	workerDone   chan struct{}

	admissionMu  sync.Mutex
	accepting    bool
	closeOnce    sync.Once
	queueStatsMu sync.Mutex
	queuedAt     []time.Time

	metricsEnabled atomic.Bool

	// 可观测计数：缓冲丢弃与写库失败/成功（原子累加，便于运维与测试断言）。
	acceptedRecords           atomic.Uint64
	droppedRecords            atomic.Uint64
	writeFailures             atomic.Uint64
	writeSuccesses            atomic.Uint64
	writeBatches              atomic.Uint64
	failedBatches             atomic.Uint64
	batchedRecords            atomic.Uint64
	inFlightRecords           atomic.Int64
	lastBatchSize             atomic.Int64
	totalWriteDurationNanos   atomic.Uint64
	lastWriteDurationNanos    atomic.Uint64
	maximumWriteDurationNanos atomic.Uint64
	totalQueueDelayNanos      atomic.Uint64
	lastQueueDelayNanos       atomic.Uint64
	maximumQueueDelayNanos    atomic.Uint64
	nextDropLogAt             atomic.Int64
}

// AsyncUsageWriterStats 是异步用量写入器的快照统计。
type AsyncUsageWriterStats struct {
	AcceptedRecords        uint64  `json:"accepted_records"`
	DroppedRecords         uint64  `json:"dropped_records"`
	WriteFailures          uint64  `json:"write_failures"`
	WriteSuccesses         uint64  `json:"write_successes"`
	WriteBatches           uint64  `json:"write_batches"`
	FailedBatches          uint64  `json:"failed_batches"`
	BatchedRecords         uint64  `json:"batched_records"`
	QueueLength            int     `json:"queue_length"`
	QueueCapacity          int     `json:"queue_capacity"`
	OldestQueuedAgeMs      float64 `json:"oldest_queued_age_ms"`
	InFlightRecords        int64   `json:"in_flight_records"`
	LastBatchSize          int64   `json:"last_batch_size"`
	AverageWriteDurationMs float64 `json:"average_write_duration_ms"`
	LastWriteDurationMs    float64 `json:"last_write_duration_ms"`
	MaximumWriteDurationMs float64 `json:"maximum_write_duration_ms"`
	AverageQueueDelayMs    float64 `json:"average_queue_delay_ms"`
	LastQueueDelayMs       float64 `json:"last_queue_delay_ms"`
	MaximumQueueDelayMs    float64 `json:"maximum_queue_delay_ms"`
}

// NewAsyncUsageWriter 启动消费者；buffer 为满时 Enqueue 会丢弃并限频记录日志（不阻塞 MCP）。
func NewAsyncUsageWriter(s Store, buffer int) *AsyncUsageWriter {
	return newAsyncUsageWriter(s, buffer, defaultAsyncUsageWriteTimeout, defaultAsyncUsageCloseTimeout)
}

// SetMetricsEnabled controls optional queue and write performance collection.
// Usage persistence remains active regardless of this setting.
func (w *AsyncUsageWriter) SetMetricsEnabled(enabled bool) {
	if w == nil {
		return
	}
	w.metricsEnabled.Store(enabled)
}

func newAsyncUsageWriter(s Store, buffer int, writeTimeout, closeTimeout time.Duration) *AsyncUsageWriter {
	return newAsyncUsageWriterWithBatch(
		s,
		buffer,
		writeTimeout,
		closeTimeout,
		defaultAsyncUsageBatchSize,
		defaultAsyncUsageBatchWait,
	)
}

func newAsyncUsageWriterWithBatch(
	s Store,
	buffer int,
	writeTimeout time.Duration,
	closeTimeout time.Duration,
	batchSize int,
	batchWait time.Duration,
) *AsyncUsageWriter {
	if buffer <= 0 {
		buffer = 256
	}
	if writeTimeout <= 0 {
		writeTimeout = defaultAsyncUsageWriteTimeout
	}
	if closeTimeout <= 0 {
		closeTimeout = defaultAsyncUsageCloseTimeout
	}
	if batchSize <= 0 {
		batchSize = defaultAsyncUsageBatchSize
	}
	if batchWait <= 0 {
		batchWait = defaultAsyncUsageBatchWait
	}
	workerContext, cancelWorker := context.WithCancel(context.Background())
	writer := &AsyncUsageWriter{
		store:        s,
		ch:           make(chan queuedUsageRecord, buffer),
		writeTimeout: writeTimeout,
		closeTimeout: closeTimeout,
		batchSize:    batchSize,
		batchWait:    batchWait,
		cancelWorker: cancelWorker,
		workerDone:   make(chan struct{}),
		accepting:    true,
	}
	go writer.run(workerContext)
	return writer
}

func (w *AsyncUsageWriter) run(ctx context.Context) {
	defer close(w.workerDone)
	for {
		select {
		case <-ctx.Done():
			w.discardQueuedRecords("shutdown deadline reached")
			return
		case queuedRecord, ok := <-w.ch:
			if !ok {
				return
			}
			w.markRecordDequeued()
			if ctx.Err() != nil {
				w.discardRecord(queuedRecord.record, "shutdown deadline reached")
				w.discardQueuedRecords("shutdown deadline reached")
				return
			}

			batch := []queuedUsageRecord{queuedRecord}
			channelClosed, collectionCancelled := w.collectBatch(ctx, &batch)
			if collectionCancelled {
				w.discardBatch(batch, "shutdown deadline reached")
				w.discardQueuedRecords("shutdown deadline reached")
				return
			}
			w.writeBatch(ctx, batch)
			if channelClosed {
				return
			}
		}
	}
}

func (w *AsyncUsageWriter) collectBatch(ctx context.Context, batch *[]queuedUsageRecord) (channelClosed bool, cancelled bool) {
	if len(*batch) >= w.batchSize {
		return false, false
	}

	batchTimer := time.NewTimer(w.batchWait)
	defer batchTimer.Stop()
	for len(*batch) < w.batchSize {
		select {
		case <-ctx.Done():
			return false, true
		case <-batchTimer.C:
			return false, false
		case queuedRecord, ok := <-w.ch:
			if !ok {
				return true, false
			}
			w.markRecordDequeued()
			*batch = append(*batch, queuedRecord)
		}
	}
	return false, false
}

func (w *AsyncUsageWriter) writeBatch(workerContext context.Context, batch []queuedUsageRecord) {
	if len(batch) == 0 {
		return
	}
	records := make([]UsageRecord, len(batch))
	for recordIndex, queuedRecord := range batch {
		records[recordIndex] = queuedRecord.record
	}
	defer func() {
		for _, record := range records {
			cleanupUsageRecord(record)
		}
	}()

	metricsEnabled := w.metricsEnabled.Load()
	if metricsEnabled {
		w.inFlightRecords.Store(int64(len(records)))
		w.lastBatchSize.Store(int64(len(records)))
		defer w.inFlightRecords.Store(0)
	}

	if metricsEnabled {
		w.observeQueueDelays(batch)
	}
	writeStartedAt := time.Now()
	successfulRecords := 0
	failedRecords := 0
	var firstWriteError error

	if batchRecorder, supportsBatching := w.store.(usageBatchRecorder); supportsBatching {
		writeContext, cancelWrite := context.WithTimeout(workerContext, w.writeTimeout)
		writeError := batchRecorder.RecordUsageBatch(writeContext, records)
		cancelWrite()
		if writeError == nil {
			successfulRecords = len(records)
		} else {
			failedRecords = len(records)
			firstWriteError = writeError
		}
	} else {
		// Test and third-party stores that only implement the legacy single-record
		// method retain per-record behavior. Production SQLite uses the atomic path.
		for _, record := range records {
			writeContext, cancelWrite := context.WithTimeout(workerContext, w.writeTimeout)
			writeError := w.store.RecordUsage(writeContext, record)
			cancelWrite()
			if writeError == nil {
				successfulRecords++
				continue
			}
			failedRecords++
			if firstWriteError == nil {
				firstWriteError = writeError
			}
		}
	}

	writeDuration := time.Since(writeStartedAt)
	if metricsEnabled {
		w.observeWriteDuration(writeDuration)
		w.writeBatches.Add(1)
		w.batchedRecords.Add(uint64(len(records)))
		if successfulRecords > 0 {
			w.writeSuccesses.Add(uint64(successfulRecords))
		}
	}
	if failedRecords > 0 {
		failures := uint64(0)
		if metricsEnabled {
			failures = w.writeFailures.Add(uint64(failedRecords))
			w.failedBatches.Add(1)
		}
		firstRecord := records[0]
		log.Printf(
			"usage batch write failed records=%d failed_records=%d key=%s tool=%s cumulative_failures=%d duration=%s: %v",
			len(records),
			failedRecords,
			firstRecord.KeyID,
			firstRecord.ToolName,
			failures,
			writeDuration,
			firstWriteError,
		)
	}
}

// Enqueue 非阻塞入队；channel 已满时丢弃本条记录并累加计数。
func (w *AsyncUsageWriter) Enqueue(rec UsageRecord) {
	if w == nil {
		cleanupUsageRecord(rec)
		return
	}

	w.admissionMu.Lock()
	if !w.accepting {
		w.admissionMu.Unlock()
		w.discardRecord(rec, "writer closed")
		return
	}

	queuedRecord := queuedUsageRecord{record: rec}
	metricsEnabled := w.metricsEnabled.Load()
	if metricsEnabled {
		queuedRecord.enqueuedAt = time.Now()
	}
	admitted := false
	w.queueStatsMu.Lock()
	select {
	case w.ch <- queuedRecord:
		admitted = true
		// Keep one queue-order marker per admitted record. A zero timestamp
		// means the record entered while metrics collection was disabled.
		w.queuedAt = append(w.queuedAt, queuedRecord.enqueuedAt)
	default:
	}
	w.queueStatsMu.Unlock()
	w.admissionMu.Unlock()
	if !admitted {
		w.discardRecord(rec, "buffer full")
		return
	}
	if metricsEnabled {
		w.acceptedRecords.Add(1)
	}
}

func (w *AsyncUsageWriter) discardQueuedRecords(reason string) {
	for {
		select {
		case queuedRecord, ok := <-w.ch:
			if !ok {
				return
			}
			w.markRecordDequeued()
			w.discardRecord(queuedRecord.record, reason)
		default:
			return
		}
	}
}

func (w *AsyncUsageWriter) discardBatch(batch []queuedUsageRecord, reason string) {
	for _, queuedRecord := range batch {
		w.discardRecord(queuedRecord.record, reason)
	}
}

func (w *AsyncUsageWriter) markRecordDequeued() {
	w.queueStatsMu.Lock()
	if len(w.queuedAt) > 0 {
		w.queuedAt[0] = time.Time{}
		w.queuedAt = w.queuedAt[1:]
	}
	w.queueStatsMu.Unlock()
}

func (w *AsyncUsageWriter) observeWriteDuration(duration time.Duration) {
	durationNanos := uint64(max(duration.Nanoseconds(), 0))
	w.totalWriteDurationNanos.Add(durationNanos)
	w.lastWriteDurationNanos.Store(durationNanos)
	updateAtomicMaximum(&w.maximumWriteDurationNanos, durationNanos)
}

func (w *AsyncUsageWriter) observeQueueDelays(batch []queuedUsageRecord) {
	if len(batch) == 0 {
		return
	}

	observedAt := time.Now()
	var totalDelayNanos uint64
	var maximumDelayNanos uint64
	for _, queuedRecord := range batch {
		if queuedRecord.enqueuedAt.IsZero() {
			continue
		}
		delayNanos := uint64(max(observedAt.Sub(queuedRecord.enqueuedAt).Nanoseconds(), 0))
		totalDelayNanos += delayNanos
		if delayNanos > maximumDelayNanos {
			maximumDelayNanos = delayNanos
		}
	}
	w.totalQueueDelayNanos.Add(totalDelayNanos)
	// The oldest record represents the user-visible wait for the latest batch.
	w.lastQueueDelayNanos.Store(maximumDelayNanos)
	updateAtomicMaximum(&w.maximumQueueDelayNanos, maximumDelayNanos)
}

func (w *AsyncUsageWriter) discardRecord(rec UsageRecord, reason string) {
	defer cleanupUsageRecord(rec)
	dropped := uint64(0)
	if w.metricsEnabled.Load() {
		dropped = w.droppedRecords.Add(1)
	}
	if !w.shouldLogDrop() {
		return
	}
	log.Printf("usage record dropped (%s) key=%s tool=%s dropped_records=%d queue_cap=%d",
		reason, rec.KeyID, rec.ToolName, dropped, cap(w.ch))
}

func (w *AsyncUsageWriter) shouldLogDrop() bool {
	now := time.Now()
	nowUnixNano := now.UnixNano()
	for {
		nextAllowedUnixNano := w.nextDropLogAt.Load()
		if nowUnixNano < nextAllowedUnixNano {
			return false
		}
		if w.nextDropLogAt.CompareAndSwap(nextAllowedUnixNano, now.Add(asyncUsageDropLogInterval).UnixNano()) {
			return true
		}
	}
}

func cleanupUsageRecord(rec UsageRecord) {
	if rec.Cleanup == nil {
		return
	}
	defer func() {
		if recoveredValue := recover(); recoveredValue != nil {
			log.Printf("usage record cleanup panicked key=%s tool=%s: %v", rec.KeyID, rec.ToolName, recoveredValue)
		}
	}()
	rec.Cleanup()
}

// Stats 返回丢弃/写库计数与当前队列深度快照。
func (w *AsyncUsageWriter) Stats() AsyncUsageWriterStats {
	if w == nil || !w.metricsEnabled.Load() {
		return AsyncUsageWriterStats{}
	}
	w.queueStatsMu.Lock()
	queueLength := len(w.ch)
	oldestQueuedAge := time.Duration(0)
	if queueLength > 0 && len(w.queuedAt) > 0 && !w.queuedAt[0].IsZero() {
		oldestQueuedAge = time.Since(w.queuedAt[0])
	}
	w.queueStatsMu.Unlock()

	writeBatches := w.writeBatches.Load()
	batchedRecords := w.batchedRecords.Load()
	averageWriteDurationNanos := float64(0)
	if writeBatches > 0 {
		averageWriteDurationNanos = float64(w.totalWriteDurationNanos.Load()) / float64(writeBatches)
	}
	averageQueueDelayNanos := float64(0)
	if batchedRecords > 0 {
		averageQueueDelayNanos = float64(w.totalQueueDelayNanos.Load()) / float64(batchedRecords)
	}

	return AsyncUsageWriterStats{
		AcceptedRecords:        w.acceptedRecords.Load(),
		DroppedRecords:         w.droppedRecords.Load(),
		WriteFailures:          w.writeFailures.Load(),
		WriteSuccesses:         w.writeSuccesses.Load(),
		WriteBatches:           writeBatches,
		FailedBatches:          w.failedBatches.Load(),
		BatchedRecords:         batchedRecords,
		QueueLength:            queueLength,
		QueueCapacity:          cap(w.ch),
		OldestQueuedAgeMs:      float64(oldestQueuedAge) / float64(time.Millisecond),
		InFlightRecords:        w.inFlightRecords.Load(),
		LastBatchSize:          w.lastBatchSize.Load(),
		AverageWriteDurationMs: nanosecondsToMilliseconds(averageWriteDurationNanos),
		LastWriteDurationMs:    nanosecondsToMilliseconds(float64(w.lastWriteDurationNanos.Load())),
		MaximumWriteDurationMs: nanosecondsToMilliseconds(float64(w.maximumWriteDurationNanos.Load())),
		AverageQueueDelayMs:    nanosecondsToMilliseconds(averageQueueDelayNanos),
		LastQueueDelayMs:       nanosecondsToMilliseconds(float64(w.lastQueueDelayNanos.Load())),
		MaximumQueueDelayMs:    nanosecondsToMilliseconds(float64(w.maximumQueueDelayNanos.Load())),
	}
}

// Close stops admission, gives the worker a bounded interval to flush queued
// records, then cancels any active write and cleans up records still queued.
// It must be called before Store.Close.
func (w *AsyncUsageWriter) Close() {
	if w == nil {
		return
	}
	w.closeOnce.Do(func() {
		w.admissionMu.Lock()
		w.accepting = false
		close(w.ch)
		w.admissionMu.Unlock()

		closeTimer := time.NewTimer(w.closeTimeout)
		defer closeTimer.Stop()
		select {
		case <-w.workerDone:
			w.cancelWorker()
			return
		case <-closeTimer.C:
		}

		w.cancelWorker()
		w.discardQueuedRecords("shutdown deadline reached")

		cancellationGrace := asyncUsageCancellationGrace
		if w.writeTimeout < cancellationGrace {
			cancellationGrace = w.writeTimeout
		}
		graceTimer := time.NewTimer(cancellationGrace)
		defer graceTimer.Stop()
		select {
		case <-w.workerDone:
		case <-graceTimer.C:
			log.Printf("async usage writer close timed out with an in-flight store write")
		}
	})
}
