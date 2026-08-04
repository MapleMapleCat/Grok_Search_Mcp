package store

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

type maintenanceBetweenUsageQueriesExecutor struct {
	queryExecutor     usageQueryExecutor
	queryCount        int
	beforeSecondQuery func() error
}

func (executor *maintenanceBetweenUsageQueriesExecutor) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (*sql.Rows, error) {
	executor.queryCount++
	if executor.queryCount == 2 && executor.beforeSecondQuery != nil {
		if err := executor.beforeSecondQuery(); err != nil {
			return nil, err
		}
	}
	return executor.queryExecutor.QueryContext(ctx, query, args...)
}

func (executor *maintenanceBetweenUsageQueriesExecutor) QueryRowContext(
	ctx context.Context,
	query string,
	args ...any,
) *sql.Row {
	return executor.queryExecutor.QueryRowContext(ctx, query, args...)
}

func TestUsageRollupMigrationCreatesHistoryTables(t *testing.T) {
	sqliteStore := openTestDB(t)

	for _, tableName := range []string{"usage_hourly_rollups", "usage_daily_rollups"} {
		var tableCount int
		if err := sqliteStore.db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
			tableName,
		).Scan(&tableCount); err != nil {
			t.Fatalf("query table %s: %v", tableName, err)
		}
		if tableCount != 1 {
			t.Fatalf("table %s count = %d, want 1", tableName, tableCount)
		}
	}
}

func TestUsageMaintenanceCompactsRetainsAndPreservesStatistics(t *testing.T) {
	sqliteStore := openTestDB(t)
	sqliteStore.SetMetricsEnabled(true)
	ctx := context.Background()
	userID := testUserID(t, sqliteStore)
	apiKey, _, err := sqliteStore.CreateKey(ctx, userID, "maintenance-key", 20)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	records := []UsageRecord{
		{
			KeyID: apiKey.ID, ToolName: "web_search", Timestamp: now.Add(-2 * time.Hour),
			DurationMs: 10, Success: true, DebugJSON: `{"tier":"raw"}`,
		},
		{
			KeyID: apiKey.ID, ToolName: "x_search", Timestamp: now.Add(-30 * time.Hour),
			DurationMs: 20, Success: false, DebugJSON: `{"tier":"hourly"}`,
		},
		{
			KeyID: apiKey.ID, ToolName: "web_search", Timestamp: now.Add(-100 * time.Hour),
			DurationMs: 30, Success: true, DebugJSON: `{"tier":"daily"}`,
		},
		{
			KeyID: apiKey.ID, ToolName: "list_models", Timestamp: now.Add(-300 * time.Hour),
			DurationMs: 40, Success: true, DebugJSON: `{"tier":"expired"}`,
		},
	}
	for _, record := range records {
		if err := sqliteStore.RecordUsage(ctx, record); err != nil {
			t.Fatalf("RecordUsage(%s): %v", record.ToolName, err)
		}
	}

	policy := UsageRetentionPolicy{
		RawRetention:    24 * time.Hour,
		HourlyRetention: 72 * time.Hour,
		DailyRetention:  240 * time.Hour,
	}
	result, err := sqliteStore.RunUsageMaintenance(ctx, policy, now)
	if err != nil {
		t.Fatalf("RunUsageMaintenance: %v", err)
	}
	if result.RawRowsCompacted != 3 {
		t.Fatalf("raw rows compacted = %d, want 3", result.RawRowsCompacted)
	}
	if result.HourlyRowsCompacted != 2 {
		t.Fatalf("hourly rows compacted = %d, want 2", result.HourlyRowsCompacted)
	}
	if result.DailyRowsDeleted != 1 {
		t.Fatalf("daily rows deleted = %d, want 1", result.DailyRowsDeleted)
	}
	if result.DebugRowsDeleted != 3 {
		t.Fatalf("debug rows deleted = %d, want 3", result.DebugRowsDeleted)
	}
	maintenanceMetrics := sqliteStore.SQLiteMetrics()
	if maintenanceMetrics.UsageMaintenance.Attempts != 1 {
		t.Fatalf("maintenance attempts = %d, want 1", maintenanceMetrics.UsageMaintenance.Attempts)
	}
	if maintenanceMetrics.PrimaryWALCheckpoint.Operation.Attempts != 1 ||
		maintenanceMetrics.DebugWALCheckpoint.Operation.Attempts != 1 {
		t.Fatalf("unexpected checkpoint metrics: primary=%+v debug=%+v",
			maintenanceMetrics.PrimaryWALCheckpoint,
			maintenanceMetrics.DebugWALCheckpoint,
		)
	}

	assertTableRowCount(t, sqliteStore, "usage_log", 1)
	assertTableRowCount(t, sqliteStore, "usage_hourly_rollups", 1)
	assertTableRowCount(t, sqliteStore, "usage_daily_rollups", 1)

	stats, err := sqliteStore.GetUsageStats(ctx, apiKey.ID, now.Add(-400*time.Hour))
	if err != nil {
		t.Fatalf("GetUsageStats: %v", err)
	}
	if stats.TotalCalls != 3 || stats.SuccessCalls != 2 {
		t.Fatalf("usage totals = (%d, %d), want (3, 2)", stats.TotalCalls, stats.SuccessCalls)
	}
	if stats.ByTool["web_search"] != 2 || stats.ByTool["x_search"] != 1 {
		t.Fatalf("unexpected tool totals: %+v", stats.ByTool)
	}
	if stats.ByTool["list_models"] != 0 {
		t.Fatalf("expired tool usage remained in statistics: %+v", stats.ByTool)
	}
	if len(stats.Records) != 1 || stats.Records[0].ToolName != "web_search" {
		t.Fatalf("raw records = %+v, want only the recent web_search call", stats.Records)
	}
	var trafficCalls int64
	for _, bucket := range stats.TrafficBuckets {
		trafficCalls += bucket.Calls
	}
	if trafficCalls != 3 {
		t.Fatalf("traffic bucket calls = %d, want 3", trafficCalls)
	}

	userStats, err := sqliteStore.GetUserUsageStatsPage(ctx, userID, now.Add(-400*time.Hour), nil, usageRecordPageSize)
	if err != nil {
		t.Fatalf("GetUserUsageStats: %v", err)
	}
	if userStats.TotalCalls != 3 || userStats.SuccessCalls != 2 {
		t.Fatalf("user usage totals = (%d, %d), want (3, 2)", userStats.TotalCalls, userStats.SuccessCalls)
	}
	secondResult, err := sqliteStore.RunUsageMaintenance(ctx, policy, now)
	if err != nil {
		t.Fatalf("second RunUsageMaintenance: %v", err)
	}
	if secondResult.RawRowsCompacted != 0 || secondResult.HourlyRowsCompacted != 0 || secondResult.DailyRowsDeleted != 0 {
		t.Fatalf("second maintenance pass was not idempotent: %+v", secondResult)
	}

	statsAfterSecondPass, err := sqliteStore.GetUsageStats(ctx, apiKey.ID, now.Add(-400*time.Hour))
	if err != nil {
		t.Fatalf("GetUsageStats after second pass: %v", err)
	}
	if statsAfterSecondPass.TotalCalls != stats.TotalCalls || statsAfterSecondPass.SuccessCalls != stats.SuccessCalls {
		t.Fatalf("statistics changed after idempotent pass: before=%+v after=%+v", stats, statsAfterSecondPass)
	}
}

func TestUsageMaintenanceProcessesMultipleTimeBucketsAndIsIdempotent(t *testing.T) {
	sqliteStore := openTestDB(t)
	ctx := context.Background()
	userID := testUserID(t, sqliteStore)
	apiKey, _, err := sqliteStore.CreateKey(ctx, userID, "bucketed-maintenance-key", 20)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	now := time.Date(2026, time.August, 4, 12, 30, 0, 0, time.UTC)
	policy := UsageRetentionPolicy{
		RawRetention:    24 * time.Hour,
		HourlyRetention: 72 * time.Hour,
		DailyRetention:  240 * time.Hour,
	}
	records := []UsageRecord{
		{
			KeyID: apiKey.ID, ToolName: "raw_boundary", Timestamp: time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC),
			DurationMs: 5, Success: true,
		},
		{
			KeyID: apiKey.ID, ToolName: "web_search", Timestamp: time.Date(2026, time.August, 3, 10, 5, 0, 0, time.UTC),
			DurationMs: 10, Success: true,
		},
		{
			KeyID: apiKey.ID, ToolName: "web_search", Timestamp: time.Date(2026, time.August, 3, 10, 45, 0, 0, time.UTC),
			DurationMs: 20, Success: false,
		},
		{
			KeyID: apiKey.ID, ToolName: "x_search", Timestamp: time.Date(2026, time.August, 3, 11, 15, 0, 0, time.UTC),
			DurationMs: 30, Success: true,
		},
		{
			KeyID: apiKey.ID, ToolName: "hourly_boundary", Timestamp: time.Date(2026, time.August, 1, 0, 15, 0, 0, time.UTC),
			DurationMs: 35, Success: true,
		},
		{
			KeyID: apiKey.ID, ToolName: "web_search", Timestamp: time.Date(2026, time.July, 31, 1, 10, 0, 0, time.UTC),
			DurationMs: 40, Success: true,
		},
		{
			KeyID: apiKey.ID, ToolName: "web_search", Timestamp: time.Date(2026, time.July, 31, 2, 20, 0, 0, time.UTC),
			DurationMs: 50, Success: false,
		},
		{
			KeyID: apiKey.ID, ToolName: "list_models", Timestamp: time.Date(2026, time.July, 30, 23, 25, 0, 0, time.UTC),
			DurationMs: 60, Success: true,
		},
		{
			KeyID: apiKey.ID, ToolName: "daily_boundary", Timestamp: time.Date(2026, time.July, 25, 3, 30, 0, 0, time.UTC),
			DurationMs: 65, Success: true,
		},
		{
			KeyID: apiKey.ID, ToolName: "expired_daily", Timestamp: time.Date(2026, time.July, 24, 12, 35, 0, 0, time.UTC),
			DurationMs: 70, Success: true,
		},
	}
	for _, record := range records {
		if err := sqliteStore.RecordUsage(ctx, record); err != nil {
			t.Fatalf("RecordUsage(%s at %s): %v", record.ToolName, formatTime(record.Timestamp), err)
		}
	}

	result, err := sqliteStore.RunUsageMaintenance(ctx, policy, now)
	if err != nil {
		t.Fatalf("RunUsageMaintenance: %v", err)
	}
	if result.RawRowsCompacted != 9 {
		t.Fatalf("raw rows compacted = %d, want 9", result.RawRowsCompacted)
	}
	if result.HourlyRowsCompacted != 5 {
		t.Fatalf("hourly rows compacted = %d, want 5", result.HourlyRowsCompacted)
	}
	if result.DailyRowsDeleted != 1 {
		t.Fatalf("daily rows deleted = %d, want 1", result.DailyRowsDeleted)
	}

	assertTableRowCount(t, sqliteStore, "usage_log", 1)
	assertTableRowCount(t, sqliteStore, "usage_hourly_rollups", 3)
	assertTableRowCount(t, sqliteStore, "usage_daily_rollups", 3)
	assertUsageRollup(t, sqliteStore, "usage_hourly_rollups", apiKey.ID,
		time.Date(2026, time.August, 3, 10, 0, 0, 0, time.UTC), "web_search", 2, 1, 30)
	assertUsageRollup(t, sqliteStore, "usage_hourly_rollups", apiKey.ID,
		time.Date(2026, time.August, 3, 11, 0, 0, 0, time.UTC), "x_search", 1, 1, 30)
	assertUsageRollup(t, sqliteStore, "usage_hourly_rollups", apiKey.ID,
		time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC), "hourly_boundary", 1, 1, 35)
	assertUsageRollup(t, sqliteStore, "usage_daily_rollups", apiKey.ID,
		time.Date(2026, time.July, 31, 0, 0, 0, 0, time.UTC), "web_search", 2, 1, 90)
	assertUsageRollup(t, sqliteStore, "usage_daily_rollups", apiKey.ID,
		time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC), "list_models", 1, 1, 60)
	assertUsageRollup(t, sqliteStore, "usage_daily_rollups", apiKey.ID,
		time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC), "daily_boundary", 1, 1, 65)

	secondResult, err := sqliteStore.RunUsageMaintenance(ctx, policy, now)
	if err != nil {
		t.Fatalf("second RunUsageMaintenance: %v", err)
	}
	if secondResult.RawRowsCompacted != 0 ||
		secondResult.HourlyRowsCompacted != 0 ||
		secondResult.DailyRowsDeleted != 0 {
		t.Fatalf("second maintenance pass was not idempotent: %+v", secondResult)
	}

	assertTableRowCount(t, sqliteStore, "usage_log", 1)
	assertTableRowCount(t, sqliteStore, "usage_hourly_rollups", 3)
	assertTableRowCount(t, sqliteStore, "usage_daily_rollups", 3)
}

func TestUsageStatsSnapshotPreventsDoubleCountingAcrossMaintenanceCommit(t *testing.T) {
	sqliteStore := openTestDB(t)
	ctx := context.Background()
	userID := testUserID(t, sqliteStore)
	apiKey, _, err := sqliteStore.CreateKey(ctx, userID, "snapshot-key", 20)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Hour).Add(30 * time.Minute)
	if err := sqliteStore.RecordUsage(ctx, UsageRecord{
		KeyID:      apiKey.ID,
		ToolName:   "web_search",
		Timestamp:  now.Add(-30 * time.Hour),
		DurationMs: 10,
		Success:    true,
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	readTransaction, err := sqliteStore.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin read transaction: %v", err)
	}
	defer func() { _ = readTransaction.Rollback() }()

	var maintenanceResult UsageMaintenanceResult
	queryExecutor := &maintenanceBetweenUsageQueriesExecutor{
		queryExecutor: readTransaction,
		beforeSecondQuery: func() error {
			var maintenanceErr error
			maintenanceResult, maintenanceErr = sqliteStore.RunUsageMaintenance(ctx, UsageRetentionPolicy{
				RawRetention:    24 * time.Hour,
				HourlyRetention: 72 * time.Hour,
				DailyRetention:  240 * time.Hour,
			}, now)
			return maintenanceErr
		},
	}

	stats, err := sqliteStore.queryUsageStatsWithExecutor(
		ctx,
		queryExecutor,
		usageStatsByKey,
		[]any{apiKey.ID},
		now.Add(-48*time.Hour),
		nil,
		usageRecordPageSize,
	)
	if err != nil {
		t.Fatalf("queryUsageStatsWithExecutor: %v", err)
	}
	if err := readTransaction.Commit(); err != nil {
		t.Fatalf("commit read transaction: %v", err)
	}
	if queryExecutor.queryCount < 2 {
		t.Fatalf("main database query count = %d, maintenance was not injected", queryExecutor.queryCount)
	}
	if maintenanceResult.RawRowsCompacted != 1 {
		t.Fatalf("raw rows compacted = %d, want 1", maintenanceResult.RawRowsCompacted)
	}

	assertTableRowCount(t, sqliteStore, "usage_log", 0)
	assertTableRowCount(t, sqliteStore, "usage_hourly_rollups", 1)
	if stats.TotalCalls != 1 || stats.SuccessCalls != 1 || stats.ByTool["web_search"] != 1 {
		t.Fatalf("usage totals double-counted across maintenance commit: %+v", stats)
	}
	if len(stats.Records) != 1 || stats.Records[0].ToolName != "web_search" {
		t.Fatalf("usage records did not use the original snapshot: %+v", stats.Records)
	}
	var trafficCalls int64
	for _, bucket := range stats.TrafficBuckets {
		trafficCalls += bucket.Calls
	}
	if trafficCalls != 1 {
		t.Fatalf("traffic bucket calls = %d, want 1", trafficCalls)
	}
}

func TestUsageMaintenanceKeepsRowsAtRawCutoff(t *testing.T) {
	sqliteStore := openTestDB(t)
	ctx := context.Background()
	userID := testUserID(t, sqliteStore)
	apiKey, _, err := sqliteStore.CreateKey(ctx, userID, "boundary-key", 20)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Hour).Add(30 * time.Minute)
	rawCutoff := now.Add(-24 * time.Hour).Truncate(time.Hour)
	if err := sqliteStore.RecordUsage(ctx, UsageRecord{
		KeyID: apiKey.ID, ToolName: "web_search", Timestamp: rawCutoff,
		DurationMs: 10, Success: true,
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	_, err = sqliteStore.RunUsageMaintenance(ctx, UsageRetentionPolicy{
		RawRetention:    24 * time.Hour,
		HourlyRetention: 72 * time.Hour,
		DailyRetention:  240 * time.Hour,
	}, now)
	if err != nil {
		t.Fatalf("RunUsageMaintenance: %v", err)
	}

	assertTableRowCount(t, sqliteStore, "usage_log", 1)
	assertTableRowCount(t, sqliteStore, "usage_hourly_rollups", 0)
}

func assertTableRowCount(t *testing.T, sqliteStore *SQLiteStore, tableName string, expectedCount int) {
	t.Helper()
	var actualCount int
	if err := sqliteStore.db.QueryRow(`SELECT COUNT(*) FROM ` + tableName).Scan(&actualCount); err != nil {
		t.Fatalf("count %s rows: %v", tableName, err)
	}
	if actualCount != expectedCount {
		t.Fatalf("%s row count = %d, want %d", tableName, actualCount, expectedCount)
	}
}

func assertUsageRollup(
	t *testing.T,
	sqliteStore *SQLiteStore,
	tableName string,
	keyID string,
	bucketStart time.Time,
	toolName string,
	expectedTotalCalls int64,
	expectedSuccessCalls int64,
	expectedDurationMsTotal int64,
) {
	t.Helper()

	var totalCalls int64
	var successCalls int64
	var durationMsTotal int64
	if err := sqliteStore.db.QueryRow(
		`SELECT total_calls, success_calls, duration_ms_total FROM `+tableName+
			` WHERE key_id = ? AND bucket_start = ? AND tool_name = ?`,
		keyID,
		formatTime(bucketStart),
		toolName,
	).Scan(&totalCalls, &successCalls, &durationMsTotal); err != nil {
		t.Fatalf("query %s rollup for %s at %s: %v", tableName, toolName, formatTime(bucketStart), err)
	}

	if totalCalls != expectedTotalCalls ||
		successCalls != expectedSuccessCalls ||
		durationMsTotal != expectedDurationMsTotal {
		t.Fatalf(
			"%s rollup for %s at %s = (calls=%d, successes=%d, duration=%d), want (calls=%d, successes=%d, duration=%d)",
			tableName,
			toolName,
			formatTime(bucketStart),
			totalCalls,
			successCalls,
			durationMsTotal,
			expectedTotalCalls,
			expectedSuccessCalls,
			expectedDurationMsTotal,
		)
	}
}
