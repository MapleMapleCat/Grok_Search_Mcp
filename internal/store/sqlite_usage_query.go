package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (s *SQLiteStore) ListUsageRecordsPage(
	ctx context.Context,
	scope UsageRecordListScope,
	since time.Time,
	cursor *UsageRecordCursor,
	limit int,
) (*UsageRecordPage, error) {
	where := ""
	whereArgs := make([]any, 0, 1)
	switch {
	case strings.TrimSpace(scope.KeyID) != "":
		where = usageStatsWhere[usageStatsByKey]
		whereArgs = append(whereArgs, strings.TrimSpace(scope.KeyID))
	case scope.IncludeAllUsers:
		where = usageStatsWhere[usageStatsGlobal]
	case strings.TrimSpace(scope.UserID) != "":
		where = usageStatsWhere[usageStatsByUser]
		whereArgs = append(whereArgs, strings.TrimSpace(scope.UserID))
	default:
		return nil, fmt.Errorf("usage record list scope is required")
	}
	return s.queryUsageRecordPage(ctx, where, whereArgs, since.UTC().Truncate(time.Second), cursor, limit)
}

func (s *SQLiteStore) queryUsageRecordPage(
	ctx context.Context,
	where string,
	whereArgs []any,
	since time.Time,
	cursor *UsageRecordCursor,
	limit int,
) (*UsageRecordPage, error) {
	pageLimit := normalizePanelPageLimit(limit)
	query := `SELECT id, key_id, tool_name, timestamp, duration_ms, success
		FROM usage_log WHERE ` + where + ` AND timestamp >= ?`
	queryArgs := appendUsageStatsArgs(whereArgs, formatTime(since.UTC()))
	if cursor != nil {
		cursorTimestamp := formatTime(cursor.Timestamp.UTC())
		query += ` AND (timestamp < ? OR (timestamp = ? AND id < ?))`
		queryArgs = append(queryArgs, cursorTimestamp, cursorTimestamp, cursor.ID)
	}
	query += ` ORDER BY timestamp DESC, id DESC LIMIT ?`
	queryArgs = append(queryArgs, pageLimit+1)

	rows, err := s.readDB.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]UsageRecord, 0, pageLimit+1)
	for rows.Next() {
		var record UsageRecord
		var timestamp string
		var success int
		if err := rows.Scan(&record.ID, &record.KeyID, &record.ToolName, &timestamp, &record.DurationMs, &success); err != nil {
			return nil, err
		}
		record.Success = success != 0
		record.Timestamp, err = parseTime(timestamp)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	page := &UsageRecordPage{}
	if len(records) > pageLimit {
		page.HasMore = true
		records = records[:pageLimit]
	}
	page.Records = records
	if page.HasMore && len(records) > 0 {
		lastRecord := records[len(records)-1]
		page.NextCursor = &UsageRecordCursor{Timestamp: lastRecord.Timestamp, ID: lastRecord.ID}
	}
	if err := s.loadUsageDebugBodySummaries(ctx, page.Records); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *SQLiteStore) GetUsageRecordDetail(ctx context.Context, usageID int64, scope UsageRecordScope) (*UsageRecord, error) {
	if usageID <= 0 {
		return nil, ErrUsageRecordNotFound
	}

	query := `SELECT usage_log.id, usage_log.key_id, usage_log.tool_name, usage_log.timestamp,
	                 usage_log.duration_ms, usage_log.success
	          FROM usage_log
	          INNER JOIN apikeys ON apikeys.id = usage_log.key_id
	          WHERE usage_log.id = ?`
	queryArgs := []any{usageID}
	if !scope.IncludeAllUsers {
		query += ` AND apikeys.user_id = ?`
		queryArgs = append(queryArgs, scope.UserID)
	}

	var record UsageRecord
	var timestamp string
	var success int
	err := s.readDB.QueryRowContext(ctx, query, queryArgs...).Scan(
		&record.ID,
		&record.KeyID,
		&record.ToolName,
		&timestamp,
		&record.DurationMs,
		&success,
	)
	if err == sql.ErrNoRows {
		return nil, ErrUsageRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	record.Success = success != 0
	record.Timestamp, err = parseTime(timestamp)
	if err != nil {
		return nil, err
	}

	if _, err := s.loadUsageDebugRecord(ctx, &record); err != nil {
		return nil, err
	}
	return &record, nil
}
