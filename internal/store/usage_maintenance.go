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
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return UsageMaintenanceResult{}, err
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO usage_hourly_rollups (
			key_id, bucket_start, tool_name, total_calls, success_calls, duration_ms_total
		)
		SELECT key_id,
		       strftime('%Y-%m-%d %H:00:00', timestamp),
		       tool_name,
		       COUNT(*),
		       COALESCE(SUM(success), 0),
		       COALESCE(SUM(duration_ms), 0)
		FROM usage_log
		WHERE timestamp < ?
		GROUP BY key_id, strftime('%Y-%m-%d %H:00:00', timestamp), tool_name
		ON CONFLICT(key_id, bucket_start, tool_name) DO UPDATE SET
			total_calls = total_calls + excluded.total_calls,
			success_calls = success_calls + excluded.success_calls,
			duration_ms_total = duration_ms_total + excluded.duration_ms_total`,
		formatTime(rawCutoff),
	); err != nil {
		return UsageMaintenanceResult{}, fmt.Errorf("roll up raw usage by hour: %w", err)
	}

	rawDeleteResult, err := transaction.ExecContext(ctx,
		`DELETE FROM usage_log WHERE timestamp < ?`,
		formatTime(rawCutoff),
	)
	if err != nil {
		return UsageMaintenanceResult{}, fmt.Errorf("delete compacted raw usage: %w", err)
	}

	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO usage_daily_rollups (
			key_id, bucket_start, tool_name, total_calls, success_calls, duration_ms_total
		)
		SELECT key_id,
		       strftime('%Y-%m-%d 00:00:00', bucket_start),
		       tool_name,
		       SUM(total_calls),
		       SUM(success_calls),
		       SUM(duration_ms_total)
		FROM usage_hourly_rollups
		WHERE bucket_start < ?
		GROUP BY key_id, strftime('%Y-%m-%d 00:00:00', bucket_start), tool_name
		ON CONFLICT(key_id, bucket_start, tool_name) DO UPDATE SET
			total_calls = total_calls + excluded.total_calls,
			success_calls = success_calls + excluded.success_calls,
			duration_ms_total = duration_ms_total + excluded.duration_ms_total`,
		formatTime(hourlyCutoff),
	); err != nil {
		return UsageMaintenanceResult{}, fmt.Errorf("roll up hourly usage by day: %w", err)
	}

	hourlyDeleteResult, err := transaction.ExecContext(ctx,
		`DELETE FROM usage_hourly_rollups WHERE bucket_start < ?`,
		formatTime(hourlyCutoff),
	)
	if err != nil {
		return UsageMaintenanceResult{}, fmt.Errorf("delete compacted hourly usage: %w", err)
	}

	dailyDeleteResult, err := transaction.ExecContext(ctx,
		`DELETE FROM usage_daily_rollups WHERE bucket_start < ?`,
		formatTime(dailyCutoff),
	)
	if err != nil {
		return UsageMaintenanceResult{}, fmt.Errorf("delete expired daily usage: %w", err)
	}

	result := UsageMaintenanceResult{}
	if result.RawRowsCompacted, err = rawDeleteResult.RowsAffected(); err != nil {
		return UsageMaintenanceResult{}, fmt.Errorf("count compacted raw usage: %w", err)
	}
	if result.HourlyRowsCompacted, err = hourlyDeleteResult.RowsAffected(); err != nil {
		return UsageMaintenanceResult{}, fmt.Errorf("count compacted hourly usage: %w", err)
	}
	if result.DailyRowsDeleted, err = dailyDeleteResult.RowsAffected(); err != nil {
		return UsageMaintenanceResult{}, fmt.Errorf("count deleted daily usage: %w", err)
	}

	if err := transaction.Commit(); err != nil {
		return UsageMaintenanceResult{}, fmt.Errorf("commit usage maintenance: %w", err)
	}
	return result, nil
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
