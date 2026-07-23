package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// usageStatsScope 限定 usage_log 聚合的 WHERE 条件，禁止任意 SQL 片段拼接。
type usageStatsScope int

const (
	usageStatsByKey usageStatsScope = iota
	usageStatsByUser
	usageStatsGlobal
)

var usageStatsWhere = map[usageStatsScope]string{
	usageStatsByKey:  `key_id = ?`,
	usageStatsByUser: `key_id IN (SELECT id FROM apikeys WHERE user_id = ?)`,
	usageStatsGlobal: `1=1`,
}

func buildUsageStatsAggregateQuery(where string) string {
	return `SELECT tool_name, COUNT(*), COALESCE(SUM(success), 0) FROM usage_log WHERE ` +
		where + ` AND timestamp >= ? GROUP BY tool_name`
}

type usageRollupSource struct {
	tableName       string
	timestampColumn string
	bucketDuration  time.Duration
}

var usageRollupSources = []usageRollupSource{
	{
		tableName:       "usage_hourly_rollups",
		timestampColumn: "bucket_start",
		bucketDuration:  time.Hour,
	},
	{
		tableName:       "usage_daily_rollups",
		timestampColumn: "bucket_start",
		bucketDuration:  24 * time.Hour,
	},
}

func buildUsageRollupStatsAggregateQuery(source usageRollupSource, where string) string {
	return `SELECT tool_name, COALESCE(SUM(total_calls), 0), COALESCE(SUM(success_calls), 0) FROM ` +
		source.tableName + ` WHERE ` + where + ` AND ` + source.timestampColumn + ` >= ? GROUP BY tool_name`
}

func (s *SQLiteStore) GetUsageStats(ctx context.Context, keyID string, since time.Time) (*UsageStats, error) {
	return s.queryUsageStats(ctx, usageStatsByKey, []any{keyID}, since, nil, usageRecordPageSize)
}

func (s *SQLiteStore) GetUserUsageStats(ctx context.Context, userID string, since time.Time) (*UsageStats, error) {
	return s.GetUserUsageStatsPage(ctx, userID, since, nil, usageRecordPageSize)
}

func (s *SQLiteStore) GetUserUsageStatsPage(
	ctx context.Context,
	userID string,
	since time.Time,
	cursor *UsageRecordCursor,
	limit int,
) (*UsageStats, error) {
	return s.queryUsageStats(ctx, usageStatsByUser, []any{userID}, since, cursor, limit)
}

func (s *SQLiteStore) GetGlobalStats(ctx context.Context, since time.Time) (*UsageStats, error) {
	return s.queryUsageStats(ctx, usageStatsGlobal, nil, since, nil, usageRecordPageSize)
}

const (
	usageTrafficBucketCount = 8
	usageRecordPageSize     = 50
)

// queryUsageStats 按条件聚合 usage_log，并拉取请求的明细页（按时间与 ID 倒序）。
// 流量桶与最近一分钟调用数均直接由 SQLite 对完整数据集聚合，避免被明细上限截断。
func (s *SQLiteStore) queryUsageStats(
	ctx context.Context,
	scope usageStatsScope,
	whereArgs []any,
	since time.Time,
	recordCursor *UsageRecordCursor,
	recordLimit int,
) (*UsageStats, error) {
	where, ok := usageStatsWhere[scope]
	if !ok {
		return nil, fmt.Errorf("invalid usage stats scope")
	}
	stats := &UsageStats{ByTool: make(map[string]int64)}

	queryEnd := time.Now().UTC().Truncate(time.Second)
	sinceUTC := since.UTC().Truncate(time.Second)
	sinceStr := formatTime(sinceUTC)
	args := appendUsageStatsArgs(whereArgs, sinceStr)

	if err := s.addRawUsageAggregates(ctx, stats, where, args); err != nil {
		return nil, err
	}
	for _, source := range usageRollupSources {
		rollupSince := truncateUsageRollupBoundary(sinceUTC, source.bucketDuration)
		rollupArgs := appendUsageStatsArgs(whereArgs, formatTime(rollupSince))
		if err := s.addRollupUsageAggregates(ctx, stats, source, where, rollupArgs); err != nil {
			return nil, err
		}
	}

	recordPage, err := s.queryUsageRecordPage(ctx, where, whereArgs, sinceUTC, recordCursor, recordLimit)
	if err != nil {
		return nil, err
	}
	stats.Records = recordPage.Records
	stats.RecordsPage = UsageRecordPageInfo{HasMore: recordPage.HasMore, NextCursor: recordPage.NextCursor}
	currentRPM, err := s.queryCurrentRPM(ctx, where, whereArgs, queryEnd)
	if err != nil {
		return nil, err
	}
	stats.CurrentRPM = currentRPM

	trafficRangeStart, err := s.resolveUsageTrafficRangeStart(ctx, where, whereArgs, sinceUTC, queryEnd)
	if err != nil {
		return nil, err
	}
	trafficBuckets, err := s.queryUsageTrafficBuckets(ctx, where, whereArgs, trafficRangeStart, queryEnd)
	if err != nil {
		return nil, err
	}
	stats.TrafficBuckets = trafficBuckets
	return stats, nil
}

func (s *SQLiteStore) addRawUsageAggregates(
	ctx context.Context,
	stats *UsageStats,
	where string,
	queryArgs []any,
) error {
	rows, err := s.readDB.QueryContext(ctx, buildUsageStatsAggregateQuery(where), queryArgs...)
	if err != nil {
		return err
	}
	defer rows.Close()
	return scanUsageAggregateRows(rows, stats)
}

func scanUsageAggregateRows(rows *sql.Rows, stats *UsageStats) error {
	for rows.Next() {
		var toolName string
		var callCount int64
		var successCount int64
		if err := rows.Scan(&toolName, &callCount, &successCount); err != nil {
			return err
		}
		addUsageAggregate(stats, toolName, callCount, successCount)
	}
	return rows.Err()
}

func (s *SQLiteStore) addRollupUsageAggregates(
	ctx context.Context,
	stats *UsageStats,
	source usageRollupSource,
	where string,
	queryArgs []any,
) error {
	rows, err := s.readDB.QueryContext(ctx, buildUsageRollupStatsAggregateQuery(source, where), queryArgs...)
	if err != nil {
		return err
	}
	defer rows.Close()
	return scanUsageAggregateRows(rows, stats)
}

func addUsageAggregate(stats *UsageStats, toolName string, callCount, successCount int64) {
	stats.ByTool[toolName] += callCount
	stats.TotalCalls += callCount
	stats.SuccessCalls += successCount
}

func truncateUsageRollupBoundary(timestamp time.Time, bucketDuration time.Duration) time.Time {
	if bucketDuration == 24*time.Hour {
		return truncateToUTCDay(timestamp)
	}
	return timestamp.UTC().Truncate(bucketDuration)
}

func appendUsageStatsArgs(whereArgs []any, trailingArgs ...any) []any {
	queryArgs := make([]any, 0, len(whereArgs)+len(trailingArgs))
	queryArgs = append(queryArgs, whereArgs...)
	queryArgs = append(queryArgs, trailingArgs...)
	return queryArgs
}

func (s *SQLiteStore) queryCurrentRPM(ctx context.Context, where string, whereArgs []any, queryEnd time.Time) (int64, error) {
	queryStart := queryEnd.Add(-time.Minute)
	queryArgs := appendUsageStatsArgs(whereArgs, formatTime(queryStart), formatTime(queryEnd))

	var currentRPM int64
	err := s.readDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM usage_log WHERE `+where+` AND timestamp >= ? AND timestamp <= ?`,
		queryArgs...,
	).Scan(&currentRPM)
	return currentRPM, err
}

func (s *SQLiteStore) resolveUsageTrafficRangeStart(
	ctx context.Context,
	where string,
	whereArgs []any,
	since time.Time,
	queryEnd time.Time,
) (time.Time, error) {
	if !since.IsZero() {
		if since.Before(queryEnd) {
			return since, nil
		}
		return queryEnd.Add(-24 * time.Hour), nil
	}

	earliestUsageTime, hasUsage, err := s.queryEarliestUsageTime(ctx, "usage_log", "timestamp", where, whereArgs)
	if err != nil {
		return time.Time{}, err
	}
	for _, source := range usageRollupSources {
		sourceEarliestTime, sourceHasUsage, err := s.queryEarliestUsageTime(
			ctx,
			source.tableName,
			source.timestampColumn,
			where,
			whereArgs,
		)
		if err != nil {
			return time.Time{}, err
		}
		if sourceHasUsage && (!hasUsage || sourceEarliestTime.Before(earliestUsageTime)) {
			earliestUsageTime = sourceEarliestTime
			hasUsage = true
		}
	}
	if !hasUsage {
		return queryEnd.Add(-24 * time.Hour), nil
	}

	earliestUsageTime = earliestUsageTime.UTC().Truncate(time.Second)
	if !earliestUsageTime.Before(queryEnd) {
		return queryEnd.Add(-24 * time.Hour), nil
	}
	return earliestUsageTime, nil
}

func (s *SQLiteStore) queryEarliestUsageTime(
	ctx context.Context,
	tableName string,
	timestampColumn string,
	where string,
	whereArgs []any,
) (time.Time, bool, error) {
	var earliestTimestamp sql.NullString
	if err := s.readDB.QueryRowContext(ctx,
		`SELECT MIN(`+timestampColumn+`) FROM `+tableName+` WHERE `+where,
		whereArgs...,
	).Scan(&earliestTimestamp); err != nil {
		return time.Time{}, false, err
	}
	if !earliestTimestamp.Valid || earliestTimestamp.String == "" {
		return time.Time{}, false, nil
	}

	parsedTimestamp, err := parseTime(earliestTimestamp.String)
	if err != nil {
		return time.Time{}, false, err
	}
	return parsedTimestamp, true, nil
}

func (s *SQLiteStore) queryUsageTrafficBuckets(
	ctx context.Context,
	where string,
	whereArgs []any,
	rangeStart time.Time,
	rangeEnd time.Time,
) ([]UsageBucket, error) {
	rangeStart = rangeStart.UTC().Truncate(time.Second)
	rangeEnd = rangeEnd.UTC().Truncate(time.Second)
	if !rangeStart.Before(rangeEnd) {
		rangeStart = rangeEnd.Add(-24 * time.Hour)
	}

	rangeDurationSeconds := int64(rangeEnd.Sub(rangeStart) / time.Second)
	if rangeDurationSeconds < 1 {
		rangeDurationSeconds = 1
	}

	buckets := createEmptyUsageTrafficBuckets(rangeStart, rangeEnd, rangeDurationSeconds)
	if err := s.addUsageTrafficSource(
		ctx,
		buckets,
		"usage_log",
		"timestamp",
		"COUNT(*)",
		where,
		whereArgs,
		rangeStart,
		rangeEnd,
		rangeDurationSeconds,
	); err != nil {
		return nil, err
	}
	for _, source := range usageRollupSources {
		sourceRangeStart := truncateUsageRollupBoundary(rangeStart, source.bucketDuration)
		if err := s.addUsageTrafficSource(
			ctx,
			buckets,
			source.tableName,
			source.timestampColumn,
			"SUM(total_calls)",
			where,
			whereArgs,
			sourceRangeStart,
			rangeEnd,
			rangeDurationSeconds,
		); err != nil {
			return nil, err
		}
	}
	return buckets, nil
}

func (s *SQLiteStore) addUsageTrafficSource(
	ctx context.Context,
	buckets []UsageBucket,
	tableName string,
	timestampColumn string,
	callCountExpression string,
	where string,
	whereArgs []any,
	sourceRangeStart time.Time,
	rangeEnd time.Time,
	rangeDurationSeconds int64,
) error {
	queryArgs := make([]any, 0, 2+len(whereArgs)+2)
	queryArgs = append(queryArgs, buckets[0].Start.Unix(), rangeDurationSeconds)
	queryArgs = append(queryArgs, whereArgs...)
	queryArgs = append(queryArgs, formatTime(sourceRangeStart), formatTime(rangeEnd))

	rows, err := s.readDB.QueryContext(ctx,
		`WITH bucket_window(start_unix, duration_seconds) AS (VALUES (?, ?))
		 SELECT MIN(7, MAX(0, CAST(((CAST(strftime('%s', `+timestampColumn+`) AS INTEGER) - bucket_window.start_unix) * 8) / bucket_window.duration_seconds AS INTEGER))) AS bucket_index,
		        `+callCountExpression+`
		 FROM `+tableName+`
		 CROSS JOIN bucket_window
		 WHERE `+where+` AND `+timestampColumn+` >= ? AND `+timestampColumn+` <= ?
		 GROUP BY bucket_index
		 ORDER BY bucket_index`,
		queryArgs...,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var bucketIndex int
		var callCount int64
		if err := rows.Scan(&bucketIndex, &callCount); err != nil {
			return err
		}
		if bucketIndex < 0 || bucketIndex >= len(buckets) {
			return fmt.Errorf("invalid usage traffic bucket index %d", bucketIndex)
		}
		buckets[bucketIndex].Calls += callCount
	}
	return rows.Err()
}

func createEmptyUsageTrafficBuckets(rangeStart, rangeEnd time.Time, rangeDurationSeconds int64) []UsageBucket {
	buckets := make([]UsageBucket, usageTrafficBucketCount)
	for bucketIndex := 0; bucketIndex < usageTrafficBucketCount; bucketIndex++ {
		bucketStartOffset := divideCeiling(
			rangeDurationSeconds*int64(bucketIndex),
			usageTrafficBucketCount,
		)
		bucketEndOffset := divideCeiling(
			rangeDurationSeconds*int64(bucketIndex+1),
			usageTrafficBucketCount,
		)
		buckets[bucketIndex] = UsageBucket{
			Start: rangeStart.Add(time.Duration(bucketStartOffset) * time.Second),
			End:   rangeStart.Add(time.Duration(bucketEndOffset) * time.Second),
		}
	}
	buckets[len(buckets)-1].End = rangeEnd
	return buckets
}

func divideCeiling(numerator int64, denominator int) int64 {
	return (numerator + int64(denominator) - 1) / int64(denominator)
}
