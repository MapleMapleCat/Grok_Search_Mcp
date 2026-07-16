package store

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const debugCleanupTimeout = 5 * time.Second

const debugDatabaseSuffix = ".debug.sqlite"

func debugDatabasePath(mainDatabasePath string) string {
	if mainDatabasePath == ":memory:" {
		return ":memory:"
	}
	return mainDatabasePath + debugDatabaseSuffix
}

func openDebugSQLite(mainDatabasePath string) (*sql.DB, error) {
	debugPath := debugDatabasePath(mainDatabasePath)
	if debugPath != ":memory:" {
		debugFile, err := os.OpenFile(debugPath, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create debug sqlite: %w", err)
		}
		if closeErr := debugFile.Close(); closeErr != nil {
			return nil, fmt.Errorf("close debug sqlite file: %w", closeErr)
		}
		if err := os.Chmod(debugPath, 0o600); err != nil {
			return nil, fmt.Errorf("secure debug sqlite file: %w", err)
		}
	}

	debugDB, err := sql.Open("sqlite", debugPath+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("open debug sqlite: %w", err)
	}
	debugDB.SetMaxOpenConns(1)
	debugDB.SetMaxIdleConns(1)
	if _, err := debugDB.Exec(`
		CREATE TABLE IF NOT EXISTS usage_debug (
			usage_id INTEGER PRIMARY KEY,
			key_id TEXT NOT NULL,
			usage_timestamp TEXT NOT NULL,
			debug_json TEXT NOT NULL DEFAULT '',
			request_body BLOB,
			response_body BLOB,
			request_captured_bytes INTEGER NOT NULL DEFAULT 0,
			response_captured_bytes INTEGER NOT NULL DEFAULT 0,
			request_observed_bytes INTEGER NOT NULL DEFAULT 0,
			response_observed_bytes INTEGER NOT NULL DEFAULT 0,
			request_truncated INTEGER NOT NULL DEFAULT 0,
			response_truncated INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_usage_debug_key_id ON usage_debug(key_id);
		CREATE INDEX IF NOT EXISTS idx_usage_debug_created_at ON usage_debug(created_at);
	`); err != nil {
		_ = debugDB.Close()
		return nil, fmt.Errorf("initialize debug sqlite: %w", err)
	}
	return debugDB, nil
}

// deleteUsageDebugByKeyIDsBestEffort keeps auxiliary debug cleanup from
// changing the result of an already committed primary-database deletion.
func (s *SQLiteStore) deleteUsageDebugByKeyIDsBestEffort(keyIDs []string) {
	if len(keyIDs) == 0 {
		return
	}

	cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), debugCleanupTimeout)
	defer cancelCleanup()

	placeholders := make([]string, len(keyIDs))
	arguments := make([]any, len(keyIDs))
	for keyIndex, keyID := range keyIDs {
		placeholders[keyIndex] = "?"
		arguments[keyIndex] = keyID
	}

	_, _ = s.debugDB.ExecContext(cleanupContext,
		`DELETE FROM usage_debug WHERE key_id IN (`+strings.Join(placeholders, ", ")+`)`,
		arguments...,
	)
}

const maxPersistedDebugBodyBytes int64 = 1 << 20

func readBoundedDebugBody(path string) ([]byte, int64, bool, error) {
	if strings.TrimSpace(path) == "" {
		return nil, 0, false, nil
	}
	bodyFile, err := os.Open(path)
	if err != nil {
		return nil, 0, false, err
	}
	defer bodyFile.Close()
	bodyInfo, err := bodyFile.Stat()
	if err != nil {
		return nil, 0, false, err
	}
	observedBytes := bodyInfo.Size()

	body, err := io.ReadAll(io.LimitReader(bodyFile, maxPersistedDebugBodyBytes+1))
	if err != nil {
		return nil, 0, false, err
	}
	if int64(len(body)) > maxPersistedDebugBodyBytes {
		return body[:maxPersistedDebugBodyBytes], observedBytes, true, nil
	}
	return body, observedBytes, false, nil
}

func boolAsInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *SQLiteStore) persistUsageDebugRecord(ctx context.Context, usageID int64, record UsageRecord) error {
	if strings.TrimSpace(record.DebugJSON) == "" &&
		strings.TrimSpace(record.DebugRequestBodyPath) == "" &&
		strings.TrimSpace(record.DebugResponseBodyPath) == "" {
		return nil
	}

	requestBody, requestSpoolBytes, requestPersistenceTruncated, err := readBoundedDebugBody(record.DebugRequestBodyPath)
	if err != nil {
		return fmt.Errorf("read request debug body: %w", err)
	}
	responseBody, responseSpoolBytes, responsePersistenceTruncated, err := readBoundedDebugBody(record.DebugResponseBodyPath)
	if err != nil {
		return fmt.Errorf("read response debug body: %w", err)
	}

	requestObservedBytes := record.DebugRequestObservedBytes
	if requestObservedBytes < requestSpoolBytes {
		requestObservedBytes = requestSpoolBytes
	}
	responseObservedBytes := record.DebugResponseObservedBytes
	if responseObservedBytes < responseSpoolBytes {
		responseObservedBytes = responseSpoolBytes
	}

	_, err = s.debugDB.ExecContext(ctx, `
		INSERT INTO usage_debug (
			usage_id, key_id, usage_timestamp, debug_json,
			request_body, response_body,
			request_captured_bytes, response_captured_bytes,
			request_observed_bytes, response_observed_bytes,
			request_truncated, response_truncated, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(usage_id) DO UPDATE SET
			key_id = excluded.key_id,
			usage_timestamp = excluded.usage_timestamp,
			debug_json = excluded.debug_json,
			request_body = excluded.request_body,
			response_body = excluded.response_body,
			request_captured_bytes = excluded.request_captured_bytes,
			response_captured_bytes = excluded.response_captured_bytes,
			request_observed_bytes = excluded.request_observed_bytes,
			response_observed_bytes = excluded.response_observed_bytes,
			request_truncated = excluded.request_truncated,
			response_truncated = excluded.response_truncated,
			created_at = excluded.created_at`,
		usageID,
		record.KeyID,
		formatTime(record.Timestamp.UTC()),
		record.DebugJSON,
		requestBody,
		responseBody,
		len(requestBody),
		len(responseBody),
		requestObservedBytes,
		responseObservedBytes,
		boolAsInteger(record.DebugRequestTruncated || requestPersistenceTruncated),
		boolAsInteger(record.DebugResponseTruncated || responsePersistenceTruncated),
		formatTime(time.Now().UTC()),
	)
	return err
}

func (s *SQLiteStore) loadUsageDebugBodySummaries(ctx context.Context, records []UsageRecord) error {
	if len(records) == 0 {
		return nil
	}

	queryPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(records)), ",")
	queryArgs := make([]any, 0, len(records))
	recordIndexesByID := make(map[int64]int, len(records))
	for recordIndex := range records {
		queryArgs = append(queryArgs, records[recordIndex].ID)
		recordIndexesByID[records[recordIndex].ID] = recordIndex
	}

	rows, err := s.debugDB.QueryContext(ctx,
		`SELECT usage_id, debug_json,
		        request_captured_bytes, response_captured_bytes,
		        request_observed_bytes, response_observed_bytes,
		        request_truncated, response_truncated
		 FROM usage_debug
		 WHERE usage_id IN (`+queryPlaceholders+`)`,
		queryArgs...,
	)
	if err != nil {
		return err
	}
	for rows.Next() {
		var usageID int64
		var debugJSON string
		var requestBytes int64
		var responseBytes int64
		var requestObservedBytes int64
		var responseObservedBytes int64
		var requestTruncated int
		var responseTruncated int
		if err := rows.Scan(
			&usageID,
			&debugJSON,
			&requestBytes,
			&responseBytes,
			&requestObservedBytes,
			&responseObservedBytes,
			&requestTruncated,
			&responseTruncated,
		); err != nil {
			return err
		}
		recordIndex, exists := recordIndexesByID[usageID]
		if !exists {
			continue
		}
		record := &records[recordIndex]
		record.DebugJSON = debugJSON
		record.HasDebugRequestBody = requestBytes > 0
		record.HasDebugResponseBody = responseBytes > 0
		record.DebugRequestBytes = requestBytes
		record.DebugResponseBytes = responseBytes
		record.DebugRequestObservedBytes = requestObservedBytes
		record.DebugResponseObservedBytes = responseObservedBytes
		record.DebugRequestTruncated = requestTruncated != 0
		record.DebugResponseTruncated = responseTruncated != 0
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) loadUsageDebugRecord(ctx context.Context, record *UsageRecord) (bool, error) {
	var requestBody []byte
	var responseBody []byte
	var requestTruncated int
	var responseTruncated int
	err := s.debugDB.QueryRowContext(ctx, `
		SELECT debug_json, request_body, response_body,
		       request_captured_bytes, response_captured_bytes,
		       request_observed_bytes, response_observed_bytes,
		       request_truncated, response_truncated
		FROM usage_debug
		WHERE usage_id = ? AND key_id = ? AND usage_timestamp = ?`,
		record.ID,
		record.KeyID,
		formatTime(record.Timestamp.UTC()),
	).Scan(
		&record.DebugJSON,
		&requestBody,
		&responseBody,
		&record.DebugRequestBytes,
		&record.DebugResponseBytes,
		&record.DebugRequestObservedBytes,
		&record.DebugResponseObservedBytes,
		&requestTruncated,
		&responseTruncated,
	)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	record.HasDebugRequestBody = record.DebugRequestBytes > 0
	record.HasDebugResponseBody = record.DebugResponseBytes > 0
	record.DebugRequestTruncated = requestTruncated != 0
	record.DebugResponseTruncated = responseTruncated != 0
	record.DebugRequestBody = string(requestBody)
	record.DebugResponseBody = string(responseBody)
	return true, nil
}
