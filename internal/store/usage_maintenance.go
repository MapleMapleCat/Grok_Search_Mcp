package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// UsageRetentionPolicy defines how long each usage-data resolution is kept.
// Raw records retain per-request detail, hourly rollups retain medium-term
// history, and daily rollups retain long-term aggregate history.
type UsageRetentionPolicy struct {
	RawRetention    time.Duration
	HourlyRetention time.Duration
	DailyRetention  time.Duration
}

// Validate rejects policies that could create gaps between storage tiers.
func (policy UsageRetentionPolicy) Validate() error {
	if policy.RawRetention <= 0 {
		return fmt.Errorf("raw usage retention must be positive")
	}
	if policy.HourlyRetention <= policy.RawRetention {
		return fmt.Errorf("hourly usage retention must exceed raw usage retention")
	}
	if policy.DailyRetention <= policy.HourlyRetention {
		return fmt.Errorf("daily usage retention must exceed hourly usage retention")
	}
	return nil
}

// WALCheckpointResult contains SQLite's wal_checkpoint result counters.
type WALCheckpointResult struct {
	BusyFrames         int
	LogFrames          int
	CheckpointedFrames int
}

// UsageMaintenanceResult summarizes one compaction and cleanup pass.
type UsageMaintenanceResult struct {
	RawRowsCompacted    int64
	HourlyRowsCompacted int64
	DailyRowsDeleted    int64
	DebugRowsDeleted    int64
	PrimaryCheckpoint   WALCheckpointResult
	DebugCheckpoint     WALCheckpointResult
}

// RunUsageMaintenance transfers expired usage rows to lower-resolution tiers,
// removes history beyond the final retention window, and checkpoints both WALs.
func (store *SQLiteStore) RunUsageMaintenance(
	ctx context.Context,
	policy UsageRetentionPolicy,
	now time.Time,
) (result UsageMaintenanceResult, returnErr error) {
	maintenanceStartedAt := time.Now()
	defer func() {
		store.metrics.observeMaintenance(time.Since(maintenanceStartedAt), returnErr)
	}()

	if err := policy.Validate(); err != nil {
		return UsageMaintenanceResult{}, err
	}

	maintenanceTime := now.UTC().Truncate(time.Second)
	rawCutoff := maintenanceTime.Add(-policy.RawRetention).Truncate(time.Hour)
	hourlyCutoff := truncateToUTCDay(maintenanceTime.Add(-policy.HourlyRetention))
	dailyCutoff := truncateToUTCDay(maintenanceTime.Add(-policy.DailyRetention))

	result, err := store.compactPrimaryUsage(ctx, rawCutoff, hourlyCutoff, dailyCutoff)
	if err != nil {
		return UsageMaintenanceResult{}, err
	}

	debugDeleteResult, debugDeleteErr := store.debugDB.ExecContext(ctx,
		`DELETE FROM usage_debug WHERE usage_timestamp < ?`,
		formatTime(rawCutoff),
	)
	if debugDeleteErr == nil {
		result.DebugRowsDeleted, debugDeleteErr = debugDeleteResult.RowsAffected()
	}

	primaryCheckpointStartedAt := time.Now()
	primaryCheckpoint, primaryCheckpointErr := checkpointWAL(ctx, store.db)
	store.metrics.observeCheckpoint(
		true,
		time.Since(primaryCheckpointStartedAt),
		primaryCheckpoint,
		primaryCheckpointErr,
	)
	result.PrimaryCheckpoint = primaryCheckpoint
	debugCheckpointStartedAt := time.Now()
	debugCheckpoint, debugCheckpointErr := checkpointWAL(ctx, store.debugDB)
	store.metrics.observeCheckpoint(
		false,
		time.Since(debugCheckpointStartedAt),
		debugCheckpoint,
		debugCheckpointErr,
	)
	result.DebugCheckpoint = debugCheckpoint

	return result, errors.Join(debugDeleteErr, primaryCheckpointErr, debugCheckpointErr)
}

func (store *SQLiteStore) compactPrimaryUsage(
	ctx context.Context,
	rawCutoff time.Time,
	hourlyCutoff time.Time,
	dailyCutoff time.Time,
) (UsageMaintenanceResult, error) {
	result := UsageMaintenanceResult{}

	var rawSearchStart time.Time
	for {
		bucketStart, found, err := store.findNextRawUsageHour(ctx, rawSearchStart, rawCutoff)
		if err != nil {
			return UsageMaintenanceResult{}, err
		}
		if !found {
			break
		}

		rowsCompacted, err := store.compactRawUsageHour(ctx, bucketStart)
		if err != nil {
			return UsageMaintenanceResult{}, err
		}
		result.RawRowsCompacted += rowsCompacted
		rawSearchStart = bucketStart.Add(time.Hour)
	}

	var hourlySearchStart time.Time
	for {
		bucketStart, found, err := store.findNextHourlyUsageDay(ctx, hourlySearchStart, hourlyCutoff)
		if err != nil {
			return UsageMaintenanceResult{}, err
		}
		if !found {
			break
		}

		rowsCompacted, err := store.compactHourlyUsageDay(ctx, bucketStart)
		if err != nil {
			return UsageMaintenanceResult{}, err
		}
		result.HourlyRowsCompacted += rowsCompacted
		hourlySearchStart = bucketStart.Add(24 * time.Hour)
	}

	var dailySearchStart time.Time
	for {
		bucketStart, found, err := store.findNextExpiredDailyUsageDay(ctx, dailySearchStart, dailyCutoff)
		if err != nil {
			return UsageMaintenanceResult{}, err
		}
		if !found {
			break
		}

		rowsDeleted, err := store.deleteDailyUsageDay(ctx, bucketStart)
		if err != nil {
			return UsageMaintenanceResult{}, err
		}
		result.DailyRowsDeleted += rowsDeleted
		dailySearchStart = bucketStart.Add(24 * time.Hour)
	}

	return result, nil
}

func (store *SQLiteStore) findNextRawUsageHour(
	ctx context.Context,
	searchStart time.Time,
	cutoff time.Time,
) (time.Time, bool, error) {
	query := `SELECT MIN(timestamp) FROM usage_log WHERE timestamp < ?`
	queryArguments := []any{formatTime(cutoff)}
	if !searchStart.IsZero() {
		query = `SELECT MIN(timestamp) FROM usage_log WHERE timestamp >= ? AND timestamp < ?`
		queryArguments = []any{formatTime(searchStart), formatTime(cutoff)}
	}

	var firstTimestamp sql.NullString
	if err := store.readDB.QueryRowContext(ctx, query, queryArguments...).Scan(&firstTimestamp); err != nil {
		return time.Time{}, false, fmt.Errorf("find next raw usage hour: %w", err)
	}
	if !firstTimestamp.Valid {
		return time.Time{}, false, nil
	}

	parsedTimestamp, err := parseTime(firstTimestamp.String)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse raw usage timestamp %q: %w", firstTimestamp.String, err)
	}
	return parsedTimestamp.UTC().Truncate(time.Hour), true, nil
}

func (store *SQLiteStore) compactRawUsageHour(ctx context.Context, bucketStart time.Time) (int64, error) {
	bucketEnd := bucketStart.Add(time.Hour)
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin raw usage hour %s: %w", formatTime(bucketStart), err)
	}
	defer func() { _ = transaction.Rollback() }()

	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO usage_hourly_rollups (
			key_id, bucket_start, tool_name, total_calls, success_calls, duration_ms_total
		)
		SELECT key_id,
		       ?,
		       tool_name,
		       COUNT(*),
		       COALESCE(SUM(success), 0),
		       COALESCE(SUM(duration_ms), 0)
		FROM usage_log
		WHERE timestamp >= ? AND timestamp < ?
		GROUP BY key_id, tool_name
		ON CONFLICT(key_id, bucket_start, tool_name) DO UPDATE SET
			total_calls = total_calls + excluded.total_calls,
			success_calls = success_calls + excluded.success_calls,
			duration_ms_total = duration_ms_total + excluded.duration_ms_total`,
		formatTime(bucketStart),
		formatTime(bucketStart),
		formatTime(bucketEnd),
	); err != nil {
		return 0, fmt.Errorf("roll up raw usage hour %s: %w", formatTime(bucketStart), err)
	}

	rawDeleteResult, err := transaction.ExecContext(ctx,
		`DELETE FROM usage_log WHERE timestamp >= ? AND timestamp < ?`,
		formatTime(bucketStart),
		formatTime(bucketEnd),
	)
	if err != nil {
		return 0, fmt.Errorf("delete compacted raw usage hour %s: %w", formatTime(bucketStart), err)
	}

	rowsCompacted, err := rawDeleteResult.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count compacted raw usage hour %s: %w", formatTime(bucketStart), err)
	}
	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("commit raw usage hour %s: %w", formatTime(bucketStart), err)
	}
	return rowsCompacted, nil
}

func (store *SQLiteStore) findNextHourlyUsageDay(
	ctx context.Context,
	searchStart time.Time,
	cutoff time.Time,
) (time.Time, bool, error) {
	query := `SELECT MIN(bucket_start) FROM usage_hourly_rollups WHERE bucket_start < ?`
	queryArguments := []any{formatTime(cutoff)}
	if !searchStart.IsZero() {
		query = `SELECT MIN(bucket_start) FROM usage_hourly_rollups WHERE bucket_start >= ? AND bucket_start < ?`
		queryArguments = []any{formatTime(searchStart), formatTime(cutoff)}
	}

	var firstBucketStart sql.NullString
	if err := store.readDB.QueryRowContext(ctx, query, queryArguments...).Scan(&firstBucketStart); err != nil {
		return time.Time{}, false, fmt.Errorf("find next hourly usage day: %w", err)
	}
	if !firstBucketStart.Valid {
		return time.Time{}, false, nil
	}

	parsedBucketStart, err := parseTime(firstBucketStart.String)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse hourly usage bucket %q: %w", firstBucketStart.String, err)
	}
	return truncateToUTCDay(parsedBucketStart), true, nil
}

func (store *SQLiteStore) compactHourlyUsageDay(ctx context.Context, bucketStart time.Time) (int64, error) {
	bucketEnd := bucketStart.Add(24 * time.Hour)
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin hourly usage day %s: %w", formatTime(bucketStart), err)
	}
	defer func() { _ = transaction.Rollback() }()

	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO usage_daily_rollups (
			key_id, bucket_start, tool_name, total_calls, success_calls, duration_ms_total
		)
		SELECT key_id,
		       ?,
		       tool_name,
		       SUM(total_calls),
		       SUM(success_calls),
		       SUM(duration_ms_total)
		FROM usage_hourly_rollups
		WHERE bucket_start >= ? AND bucket_start < ?
		GROUP BY key_id, tool_name
		ON CONFLICT(key_id, bucket_start, tool_name) DO UPDATE SET
			total_calls = total_calls + excluded.total_calls,
			success_calls = success_calls + excluded.success_calls,
			duration_ms_total = duration_ms_total + excluded.duration_ms_total`,
		formatTime(bucketStart),
		formatTime(bucketStart),
		formatTime(bucketEnd),
	); err != nil {
		return 0, fmt.Errorf("roll up hourly usage day %s: %w", formatTime(bucketStart), err)
	}

	hourlyDeleteResult, err := transaction.ExecContext(ctx,
		`DELETE FROM usage_hourly_rollups WHERE bucket_start >= ? AND bucket_start < ?`,
		formatTime(bucketStart),
		formatTime(bucketEnd),
	)
	if err != nil {
		return 0, fmt.Errorf("delete compacted hourly usage day %s: %w", formatTime(bucketStart), err)
	}

	rowsCompacted, err := hourlyDeleteResult.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count compacted hourly usage day %s: %w", formatTime(bucketStart), err)
	}
	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("commit hourly usage day %s: %w", formatTime(bucketStart), err)
	}
	return rowsCompacted, nil
}

func (store *SQLiteStore) findNextExpiredDailyUsageDay(
	ctx context.Context,
	searchStart time.Time,
	cutoff time.Time,
) (time.Time, bool, error) {
	query := `SELECT MIN(bucket_start) FROM usage_daily_rollups WHERE bucket_start < ?`
	queryArguments := []any{formatTime(cutoff)}
	if !searchStart.IsZero() {
		query = `SELECT MIN(bucket_start) FROM usage_daily_rollups WHERE bucket_start >= ? AND bucket_start < ?`
		queryArguments = []any{formatTime(searchStart), formatTime(cutoff)}
	}

	var firstBucketStart sql.NullString
	if err := store.readDB.QueryRowContext(ctx, query, queryArguments...).Scan(&firstBucketStart); err != nil {
		return time.Time{}, false, fmt.Errorf("find next expired daily usage day: %w", err)
	}
	if !firstBucketStart.Valid {
		return time.Time{}, false, nil
	}

	parsedBucketStart, err := parseTime(firstBucketStart.String)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse daily usage bucket %q: %w", firstBucketStart.String, err)
	}
	return truncateToUTCDay(parsedBucketStart), true, nil
}

func (store *SQLiteStore) deleteDailyUsageDay(ctx context.Context, bucketStart time.Time) (int64, error) {
	bucketEnd := bucketStart.Add(24 * time.Hour)
	deleteResult, err := store.db.ExecContext(ctx,
		`DELETE FROM usage_daily_rollups WHERE bucket_start >= ? AND bucket_start < ?`,
		formatTime(bucketStart),
		formatTime(bucketEnd),
	)
	if err != nil {
		return 0, fmt.Errorf("delete expired daily usage day %s: %w", formatTime(bucketStart), err)
	}

	rowsDeleted, err := deleteResult.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted daily usage day %s: %w", formatTime(bucketStart), err)
	}
	return rowsDeleted, nil
}

func checkpointWAL(ctx context.Context, database *sql.DB) (WALCheckpointResult, error) {
	var result WALCheckpointResult
	// PASSIVE checkpoints completed frames without waiting for active readers or
	// taking the stronger locks required by TRUNCATE. This keeps maintenance from
	// becoming a periodic latency spike on the shared write path.
	err := database.QueryRowContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`).Scan(
		&result.BusyFrames,
		&result.LogFrames,
		&result.CheckpointedFrames,
	)
	if err != nil {
		return WALCheckpointResult{}, fmt.Errorf("checkpoint SQLite WAL: %w", err)
	}
	return result, nil
}

func truncateToUTCDay(timestamp time.Time) time.Time {
	utcTimestamp := timestamp.UTC()
	return time.Date(utcTimestamp.Year(), utcTimestamp.Month(), utcTimestamp.Day(), 0, 0, 0, 0, time.UTC)
}

// UsageMaintenanceRunner executes maintenance immediately and then at a fixed
// interval. Runs are serialized by the worker goroutine and never overlap.
type UsageMaintenanceRunner struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// StartUsageMaintenance starts the background retention and rollup worker.
func StartUsageMaintenance(
	parentContext context.Context,
	store *SQLiteStore,
	policy UsageRetentionPolicy,
	interval time.Duration,
) (*UsageMaintenanceRunner, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if interval <= 0 {
		return nil, fmt.Errorf("usage maintenance interval must be positive")
	}

	workerContext, cancel := context.WithCancel(parentContext)
	runner := &UsageMaintenanceRunner{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go runner.run(workerContext, store, policy, interval)
	return runner, nil
}

func (runner *UsageMaintenanceRunner) run(
	ctx context.Context,
	store *SQLiteStore,
	policy UsageRetentionPolicy,
	interval time.Duration,
) {
	defer close(runner.done)

	runMaintenance := func() {
		result, err := store.RunUsageMaintenance(ctx, policy, time.Now().UTC())
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("usage maintenance failed: %v", err)
			}
			return
		}
		log.Printf(
			"usage maintenance completed: raw_compacted=%d hourly_compacted=%d daily_deleted=%d debug_deleted=%d primary_wal_busy=%d debug_wal_busy=%d",
			result.RawRowsCompacted,
			result.HourlyRowsCompacted,
			result.DailyRowsDeleted,
			result.DebugRowsDeleted,
			result.PrimaryCheckpoint.BusyFrames,
			result.DebugCheckpoint.BusyFrames,
		)
	}

	runMaintenance()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runMaintenance()
		}
	}
}

// Close stops the maintenance worker and waits for an active pass to return.
func (runner *UsageMaintenanceRunner) Close() {
	if runner == nil {
		return
	}
	runner.once.Do(func() {
		runner.cancel()
		<-runner.done
	})
}
