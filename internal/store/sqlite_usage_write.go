package store

import (
	"context"
	"fmt"
	"time"
)

func (s *SQLiteStore) RecordUsage(ctx context.Context, record UsageRecord) error {
	success := 0
	if record.Success {
		success = 1
	}
	usageTimestamp := formatTime(record.Timestamp.UTC())

	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()

	result, err := transaction.ExecContext(ctx,
		`INSERT INTO usage_log (key_id, tool_name, timestamp, duration_ms, success) VALUES (?, ?, ?, ?, ?)`,
		record.KeyID, record.ToolName, usageTimestamp, record.DurationMs, success,
	)
	if err != nil {
		return err
	}
	usageID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("usage insert id: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE apikeys
		SET last_used_at = CASE
				WHEN last_used_at IS NULL OR last_used_at < ? THEN ?
				ELSE last_used_at
			END,
			total_calls = total_calls + 1
		WHERE id = ?`,
		usageTimestamp, usageTimestamp, record.KeyID,
	); err != nil {
		return fmt.Errorf("update API key usage: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return err
	}

	// Usage accounting is authoritative in the primary database. Debug capture
	// is persisted afterwards in its own SQLite file so large diagnostic writes
	// cannot expand or lock the primary database transaction.
	if err := s.persistUsageDebugRecord(ctx, usageID, record); err != nil {
		return fmt.Errorf("persist usage debug record: %w", err)
	}
	return nil
}

// TouchKeyUsage 保留给直接调用方；异步用量生产路径通过 RecordUsage 原子更新计数。
// 不触碰 updated_at——该字段只随 CreateKey/UpdateKey 的配置变更而更新。
func (s *SQLiteStore) TouchKeyUsage(ctx context.Context, keyID string) error {
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx,
		`UPDATE apikeys SET last_used_at = ?, total_calls = total_calls + 1 WHERE id = ?`,
		now, keyID,
	)
	return err
}
