package store

import (
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"time"
)

// SQLiteOperationMetrics is a cumulative latency and failure snapshot for one
// SQLite operation class. Durations are milliseconds to keep the admin API
// human-readable while retaining sub-millisecond precision.
type SQLiteOperationMetrics struct {
	Attempts           uint64  `json:"attempts"`
	Errors             uint64  `json:"errors"`
	BusyOrLockedErrors uint64  `json:"busy_or_locked_errors"`
	TotalDurationMs    float64 `json:"total_duration_ms"`
	AverageDurationMs  float64 `json:"average_duration_ms"`
	LastDurationMs     float64 `json:"last_duration_ms"`
	MaximumDurationMs  float64 `json:"maximum_duration_ms"`
}

// SQLiteConnectionPoolMetrics exposes database/sql queueing and connection
// utilization. WaitCount and WaitDuration directly show pressure caused by the
// deliberately single-connection write pool.
type SQLiteConnectionPoolMetrics struct {
	MaximumOpenConnections int     `json:"maximum_open_connections"`
	OpenConnections        int     `json:"open_connections"`
	InUseConnections       int     `json:"in_use_connections"`
	IdleConnections        int     `json:"idle_connections"`
	WaitCount              int64   `json:"wait_count"`
	WaitDurationMs         float64 `json:"wait_duration_ms"`
}

// SQLiteCheckpointMetrics records checkpoint latency and the most recent WAL
// frame counters returned by SQLite.
type SQLiteCheckpointMetrics struct {
	Operation        SQLiteOperationMetrics `json:"operation"`
	LastBusyFrames   int64                  `json:"last_busy_frames"`
	LastLogFrames    int64                  `json:"last_log_frames"`
	LastCheckpointed int64                  `json:"last_checkpointed_frames"`
}

// SQLiteUsageWriteMetrics supplements operation latency with record counts so
// transaction amortization can be observed after batching is enabled.
type SQLiteUsageWriteMetrics struct {
	Operation        SQLiteOperationMetrics `json:"operation"`
	RecordsAttempted uint64                 `json:"records_attempted"`
	RecordsSucceeded uint64                 `json:"records_succeeded"`
	RecordsFailed    uint64                 `json:"records_failed"`
}

// SQLiteMetricsSnapshot is a non-sensitive runtime snapshot intended for the
// authenticated administrator operations endpoint.
type SQLiteMetricsSnapshot struct {
	CapturedAt           time.Time                   `json:"captured_at"`
	PrimaryWritePool     SQLiteConnectionPoolMetrics `json:"primary_write_pool"`
	ReadPool             SQLiteConnectionPoolMetrics `json:"read_pool"`
	DebugWritePool       SQLiteConnectionPoolMetrics `json:"debug_write_pool"`
	BusyOrLockedErrors   uint64                      `json:"busy_or_locked_errors"`
	QuotaReserve         SQLiteOperationMetrics      `json:"quota_reserve"`
	QuotaRelease         SQLiteOperationMetrics      `json:"quota_release"`
	QuotaLimitRejections uint64                      `json:"quota_limit_rejections"`
	QuotaUserNotFound    uint64                      `json:"quota_user_not_found"`
	UsageWrite           SQLiteUsageWriteMetrics     `json:"usage_write"`
	UsageMaintenance     SQLiteOperationMetrics      `json:"usage_maintenance"`
	PrimaryWALCheckpoint SQLiteCheckpointMetrics     `json:"primary_wal_checkpoint"`
	DebugWALCheckpoint   SQLiteCheckpointMetrics     `json:"debug_wal_checkpoint"`
}

type sqliteOperationObserver struct {
	attempts             atomic.Uint64
	errors               atomic.Uint64
	busyOrLockedErrors   atomic.Uint64
	totalDurationNanos   atomic.Uint64
	lastDurationNanos    atomic.Uint64
	maximumDurationNanos atomic.Uint64
}

func (observer *sqliteOperationObserver) observe(duration time.Duration, operationError error) bool {
	durationNanos := uint64(max(duration.Nanoseconds(), 0))
	observer.attempts.Add(1)
	observer.totalDurationNanos.Add(durationNanos)
	observer.lastDurationNanos.Store(durationNanos)
	updateAtomicMaximum(&observer.maximumDurationNanos, durationNanos)
	if operationError == nil {
		return false
	}
	observer.errors.Add(1)
	if isSQLiteBusyOrLockedError(operationError) {
		observer.busyOrLockedErrors.Add(1)
		return true
	}
	return false
}

func (observer *sqliteOperationObserver) snapshot() SQLiteOperationMetrics {
	attempts := observer.attempts.Load()
	totalDurationNanos := observer.totalDurationNanos.Load()
	averageDurationNanos := float64(0)
	if attempts > 0 {
		averageDurationNanos = float64(totalDurationNanos) / float64(attempts)
	}
	return SQLiteOperationMetrics{
		Attempts:           attempts,
		Errors:             observer.errors.Load(),
		BusyOrLockedErrors: observer.busyOrLockedErrors.Load(),
		TotalDurationMs:    nanosecondsToMilliseconds(float64(totalDurationNanos)),
		AverageDurationMs:  nanosecondsToMilliseconds(averageDurationNanos),
		LastDurationMs:     nanosecondsToMilliseconds(float64(observer.lastDurationNanos.Load())),
		MaximumDurationMs:  nanosecondsToMilliseconds(float64(observer.maximumDurationNanos.Load())),
	}
}

type sqliteCheckpointObserver struct {
	operation        sqliteOperationObserver
	lastBusyFrames   atomic.Int64
	lastLogFrames    atomic.Int64
	lastCheckpointed atomic.Int64
}

func (observer *sqliteCheckpointObserver) observe(duration time.Duration, result WALCheckpointResult, operationError error) bool {
	observer.lastBusyFrames.Store(int64(result.BusyFrames))
	observer.lastLogFrames.Store(int64(result.LogFrames))
	observer.lastCheckpointed.Store(int64(result.CheckpointedFrames))
	return observer.operation.observe(duration, operationError)
}

func (observer *sqliteCheckpointObserver) snapshot() SQLiteCheckpointMetrics {
	return SQLiteCheckpointMetrics{
		Operation:        observer.operation.snapshot(),
		LastBusyFrames:   observer.lastBusyFrames.Load(),
		LastLogFrames:    observer.lastLogFrames.Load(),
		LastCheckpointed: observer.lastCheckpointed.Load(),
	}
}

type sqliteMetrics struct {
	busyOrLockedErrors    atomic.Uint64
	quotaReserve          sqliteOperationObserver
	quotaRelease          sqliteOperationObserver
	quotaLimitRejections  atomic.Uint64
	quotaUserNotFound     atomic.Uint64
	usageWrite            sqliteOperationObserver
	usageRecordsAttempted atomic.Uint64
	usageRecordsSucceeded atomic.Uint64
	usageRecordsFailed    atomic.Uint64
	usageMaintenance      sqliteOperationObserver
	primaryCheckpoint     sqliteCheckpointObserver
	debugCheckpoint       sqliteCheckpointObserver
}

func (metrics *sqliteMetrics) observeQuotaReserve(duration time.Duration, operationError error) {
	observedError := operationError
	switch {
	case errors.Is(operationError, ErrQuotaSuccess):
		metrics.quotaLimitRejections.Add(1)
		observedError = nil
	case errors.Is(operationError, ErrUserNotFound):
		metrics.quotaUserNotFound.Add(1)
		observedError = nil
	}
	metrics.addContention(metrics.quotaReserve.observe(duration, observedError))
}

func (metrics *sqliteMetrics) observeQuotaRelease(duration time.Duration, operationError error) {
	metrics.addContention(metrics.quotaRelease.observe(duration, operationError))
}

func (metrics *sqliteMetrics) observeUsageWrite(duration time.Duration, recordCount int, operationError error) {
	recordCountValue := uint64(max(recordCount, 0))
	metrics.usageRecordsAttempted.Add(recordCountValue)
	if operationError == nil {
		metrics.usageRecordsSucceeded.Add(recordCountValue)
	} else {
		metrics.usageRecordsFailed.Add(recordCountValue)
	}
	metrics.addContention(metrics.usageWrite.observe(duration, operationError))
}

func (metrics *sqliteMetrics) observeMaintenance(duration time.Duration, operationError error) {
	metrics.addContention(metrics.usageMaintenance.observe(duration, operationError))
}

func (metrics *sqliteMetrics) observeCheckpoint(primary bool, duration time.Duration, result WALCheckpointResult, operationError error) {
	if primary {
		metrics.addContention(metrics.primaryCheckpoint.observe(duration, result, operationError))
		return
	}
	metrics.addContention(metrics.debugCheckpoint.observe(duration, result, operationError))
}

func (metrics *sqliteMetrics) addContention(contentionObserved bool) {
	if contentionObserved {
		metrics.busyOrLockedErrors.Add(1)
	}
}

// SQLiteMetrics returns a lock-free snapshot of pool and operation metrics.
func (store *SQLiteStore) SQLiteMetrics() SQLiteMetricsSnapshot {
	return SQLiteMetricsSnapshot{
		CapturedAt:           time.Now().UTC(),
		PrimaryWritePool:     sqliteConnectionPoolMetrics(store.db),
		ReadPool:             sqliteConnectionPoolMetrics(store.readDB),
		DebugWritePool:       sqliteConnectionPoolMetrics(store.debugDB),
		BusyOrLockedErrors:   store.metrics.busyOrLockedErrors.Load(),
		QuotaReserve:         store.metrics.quotaReserve.snapshot(),
		QuotaRelease:         store.metrics.quotaRelease.snapshot(),
		QuotaLimitRejections: store.metrics.quotaLimitRejections.Load(),
		QuotaUserNotFound:    store.metrics.quotaUserNotFound.Load(),
		UsageWrite: SQLiteUsageWriteMetrics{
			Operation:        store.metrics.usageWrite.snapshot(),
			RecordsAttempted: store.metrics.usageRecordsAttempted.Load(),
			RecordsSucceeded: store.metrics.usageRecordsSucceeded.Load(),
			RecordsFailed:    store.metrics.usageRecordsFailed.Load(),
		},
		UsageMaintenance:     store.metrics.usageMaintenance.snapshot(),
		PrimaryWALCheckpoint: store.metrics.primaryCheckpoint.snapshot(),
		DebugWALCheckpoint:   store.metrics.debugCheckpoint.snapshot(),
	}
}

func sqliteConnectionPoolMetrics(database *sql.DB) SQLiteConnectionPoolMetrics {
	if database == nil {
		return SQLiteConnectionPoolMetrics{}
	}
	statistics := database.Stats()
	return SQLiteConnectionPoolMetrics{
		MaximumOpenConnections: statistics.MaxOpenConnections,
		OpenConnections:        statistics.OpenConnections,
		InUseConnections:       statistics.InUse,
		IdleConnections:        statistics.Idle,
		WaitCount:              statistics.WaitCount,
		WaitDurationMs:         float64(statistics.WaitDuration) / float64(time.Millisecond),
	}
}

func updateAtomicMaximum(destination *atomic.Uint64, candidate uint64) {
	for {
		currentMaximum := destination.Load()
		if candidate <= currentMaximum || destination.CompareAndSwap(currentMaximum, candidate) {
			return
		}
	}
}

func nanosecondsToMilliseconds(nanoseconds float64) float64 {
	return nanoseconds / float64(time.Millisecond)
}

func isSQLiteBusyOrLockedError(operationError error) bool {
	if operationError == nil {
		return false
	}
	errorMessage := strings.ToLower(operationError.Error())
	return strings.Contains(errorMessage, "database is locked") ||
		strings.Contains(errorMessage, "database is busy") ||
		strings.Contains(errorMessage, "sqlite_busy") ||
		strings.Contains(errorMessage, "sqlite_locked")
}
