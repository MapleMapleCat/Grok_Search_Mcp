package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *SQLiteStore) RecordUsage(ctx context.Context, record UsageRecord) error {
	return s.RecordUsageBatch(ctx, []UsageRecord{record})
}

type persistedUsageRecord struct {
	usageID int64
	record  UsageRecord
}

type apiKeyUsageUpdate struct {
	callCount       int64
	latestTimestamp string
}

// RecordUsageBatch persists all primary usage rows and API-key counters in one
// transaction. Debug payloads are then written to the separate debug database
// in one additional transaction, preserving the existing primary-first
// accounting semantics while greatly reducing write-lock acquisitions.
func (s *SQLiteStore) RecordUsageBatch(ctx context.Context, records []UsageRecord) (returnErr error) {
	if len(records) == 0 {
		return nil
	}
	operationStartedAt := time.Now()
	defer func() {
		s.metrics.observeUsageWrite(time.Since(operationStartedAt), len(records), returnErr)
	}()

	persistedRecords, err := s.persistPrimaryUsageBatch(ctx, records)
	if err != nil {
		return err
	}

	// Usage accounting is authoritative in the primary database. Debug capture
	// remains isolated so large diagnostic writes cannot expand the primary
	// transaction or retain its single writer lock.
	if err := s.persistUsageDebugRecords(ctx, persistedRecords); err != nil {
		return fmt.Errorf("persist usage debug batch: %w", err)
	}
	return nil
}

func (s *SQLiteStore) persistPrimaryUsageBatch(ctx context.Context, records []UsageRecord) ([]persistedUsageRecord, error) {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()

	insertStatement, err := transaction.PrepareContext(ctx,
		`INSERT INTO usage_log (key_id, tool_name, timestamp, duration_ms, success) VALUES (?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return nil, fmt.Errorf("prepare usage insert: %w", err)
	}
	defer insertStatement.Close()

	persistedRecords := make([]persistedUsageRecord, 0, len(records))
	keyUpdates := make(map[string]apiKeyUsageUpdate)
	for _, record := range records {
		usageTimestamp := formatTime(record.Timestamp.UTC())
		result, execErr := insertStatement.ExecContext(ctx,
			record.KeyID,
			record.ToolName,
			usageTimestamp,
			record.DurationMs,
			boolAsInteger(record.Success),
		)
		if execErr != nil {
			return nil, fmt.Errorf("insert usage record: %w", execErr)
		}
		usageID, insertIDErr := result.LastInsertId()
		if insertIDErr != nil {
			return nil, fmt.Errorf("usage insert id: %w", insertIDErr)
		}
		persistedRecords = append(persistedRecords, persistedUsageRecord{usageID: usageID, record: record})

		keyUpdate := keyUpdates[record.KeyID]
		keyUpdate.callCount++
		if keyUpdate.latestTimestamp == "" || usageTimestamp > keyUpdate.latestTimestamp {
			keyUpdate.latestTimestamp = usageTimestamp
		}
		keyUpdates[record.KeyID] = keyUpdate
	}

	if err := updateAPIKeyUsageBatch(ctx, transaction, keyUpdates); err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit usage batch: %w", err)
	}
	return persistedRecords, nil
}

func updateAPIKeyUsageBatch(ctx context.Context, transaction *sql.Tx, keyUpdates map[string]apiKeyUsageUpdate) error {
	updateStatement, err := transaction.PrepareContext(ctx, `
		UPDATE apikeys
		SET last_used_at = CASE
				WHEN last_used_at IS NULL OR last_used_at < ? THEN ?
				ELSE last_used_at
			END,
			total_calls = total_calls + ?
		WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare API key usage update: %w", err)
	}
	defer updateStatement.Close()

	for keyID, keyUpdate := range keyUpdates {
		if _, err := updateStatement.ExecContext(
			ctx,
			keyUpdate.latestTimestamp,
			keyUpdate.latestTimestamp,
			keyUpdate.callCount,
			keyID,
		); err != nil {
			return fmt.Errorf("update API key usage: %w", err)
		}
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
