// sqlite.go 实现 Store 接口：WAL 模式 SQLite、UUID 风格主键、grok_ 前缀的随机 API Key。
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/grok-mcp/internal/keyhash"

	_ "modernc.org/sqlite"
)

// timeLayout 为库内 UTC 时间字符串格式（与 SQLite datetime 列一致）。
const timeLayout = "2006-01-02 15:04:05"

// SQLiteStore 使用纯 Go 驱动 modernc.org/sqlite，MaxOpenConns=1 以配合 SQLite 写锁语义。
type SQLiteStore struct {
	db *sql.DB
}

// OpenSQLite 打开数据库、执行嵌入迁移并返回可用的 Store。
func OpenSQLite(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// randomID 生成 UUID v4 风格的十六进制 ID（无第三方依赖）。
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// generateRawKey 生成 grok_<64 hex> 形态的客户端密钥明文。
func generateRawKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "grok_" + hex.EncodeToString(b), nil
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.ParseInLocation(timeLayout, s, time.UTC)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// scanAPIKey 从 QueryRow 或 Rows 扫描一行 apikeys 表记录。
func scanAPIKey(row interface {
	Scan(dest ...any) error
}) (*APIKey, error) {
	var k APIKey
	var enabled int
	var createdAt, updatedAt string
	var lastUsed sql.NullString

	err := row.Scan(
		&k.ID, &k.UserID, &k.Name, &k.KeyHash, &k.KeyPrefix, &enabled,
		&createdAt, &updatedAt, &lastUsed, &k.TotalCalls,
	)
	if err != nil {
		return nil, err
	}
	k.Enabled = enabled != 0
	var err2 error
	k.CreatedAt, err2 = parseTime(createdAt)
	if err2 != nil {
		return nil, err2
	}
	k.UpdatedAt, err2 = parseTime(updatedAt)
	if err2 != nil {
		return nil, err2
	}
	if lastUsed.Valid {
		t, err := parseTime(lastUsed.String)
		if err != nil {
			return nil, err
		}
		k.LastUsedAt = &t
	}
	return &k, nil
}

const keyColumns = `id, user_id, name, key_hash, key_prefix, enabled, created_at, updated_at, last_used_at, total_calls`

// CreateKey 插入新密钥并返回元数据与一次性明文 raw（调用方须妥善保存）。
func (s *SQLiteStore) CreateKey(ctx context.Context, userID, name string) (*APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", fmt.Errorf("name is required")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, "", fmt.Errorf("user_id is required")
	}

	raw, err := generateRawKey()
	if err != nil {
		return nil, "", err
	}

	id, err := randomID()
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	prefix := raw
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO apikeys (id, user_id, name, key_hash, key_prefix, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		id, userID, name, keyhash.HashAPIKey(raw), prefix, formatTime(now), formatTime(now),
	)
	if err != nil {
		return nil, "", fmt.Errorf("insert apikey: %w", err)
	}

	k, err := s.GetKeyByID(ctx, id)
	if err != nil {
		return nil, "", err
	}
	return k, raw, nil
}

// GetKeyByHash 供鉴权使用；未找到时返回 (nil, nil)。
func (s *SQLiteStore) GetKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+keyColumns+` FROM apikeys WHERE key_hash = ?`, hash)
	k, err := scanAPIKey(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return k, err
}

func (s *SQLiteStore) ListKeysByUser(ctx context.Context, userID string) ([]*APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+keyColumns+` FROM apikeys WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *SQLiteStore) ListKeys(ctx context.Context) ([]*APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+keyColumns+` FROM apikeys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *SQLiteStore) GetKeyByID(ctx context.Context, id string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+keyColumns+` FROM apikeys WHERE id = ?`, id)
	k, err := scanAPIKey(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("api key not found")
	}
	return k, err
}

// UpdateKey 动态拼接 SET 子句，仅更新 KeyUpdates 中非 nil 字段。
func (s *SQLiteStore) UpdateKey(ctx context.Context, id string, updates KeyUpdates) (*APIKey, error) {
	if _, err := s.GetKeyByID(ctx, id); err != nil {
		return nil, err
	}

	var sets []string
	var args []any

	if updates.Name != nil {
		name := strings.TrimSpace(*updates.Name)
		if name == "" {
			return nil, fmt.Errorf("name must not be empty")
		}
		sets = append(sets, "name = ?")
		args = append(args, name)
	}
	if updates.Enabled != nil {
		en := 0
		if *updates.Enabled {
			en = 1
		}
		sets = append(sets, "enabled = ?")
		args = append(args, en)
	}

	if len(sets) == 0 {
		return s.GetKeyByID(ctx, id)
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, formatTime(time.Now().UTC()))
	args = append(args, id)

	q := `UPDATE apikeys SET ` + strings.Join(sets, ", ") + ` WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return nil, err
	}
	return s.GetKeyByID(ctx, id)
}

func (s *SQLiteStore) DeleteKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM apikeys WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("api key not found")
	}
	return nil
}

func (s *SQLiteStore) RecordUsage(ctx context.Context, record UsageRecord) error {
	success := 0
	if record.Success {
		success = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO usage_log (key_id, tool_name, timestamp, duration_ms, success) VALUES (?, ?, ?, ?, ?)`,
		record.KeyID, record.ToolName, formatTime(record.Timestamp.UTC()), record.DurationMs, success,
	)
	return err
}

// TouchKeyUsage 在 tools/call 后递增 total_calls 并刷新 last_used_at；
// 不触碰 updated_at——该字段只随 CreateKey/UpdateKey 的配置变更而更新。
func (s *SQLiteStore) TouchKeyUsage(ctx context.Context, keyID string) error {
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx,
		`UPDATE apikeys SET last_used_at = ?, total_calls = total_calls + 1 WHERE id = ?`,
		now, keyID,
	)
	return err
}

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

func (s *SQLiteStore) GetUsageStats(ctx context.Context, keyID string, since time.Time) (*UsageStats, error) {
	return s.queryUsageStats(ctx, usageStatsByKey, []any{keyID}, since)
}

func (s *SQLiteStore) GetUserUsageStats(ctx context.Context, userID string, since time.Time) (*UsageStats, error) {
	return s.queryUsageStats(ctx, usageStatsByUser, []any{userID}, since)
}

func (s *SQLiteStore) GetGlobalStats(ctx context.Context, since time.Time) (*UsageStats, error) {
	return s.queryUsageStats(ctx, usageStatsGlobal, nil, since)
}

// queryUsageStats 按条件聚合 usage_log，并拉取最近 500 条明细（按时间倒序）。
func (s *SQLiteStore) queryUsageStats(ctx context.Context, scope usageStatsScope, whereArgs []any, since time.Time) (*UsageStats, error) {
	where, ok := usageStatsWhere[scope]
	if !ok {
		return nil, fmt.Errorf("invalid usage stats scope")
	}
	stats := &UsageStats{ByTool: make(map[string]int64)}

	sinceStr := formatTime(since.UTC())
	args := append(whereArgs, sinceStr)

	rows, err := s.db.QueryContext(ctx,
		`SELECT tool_name, COUNT(*) FROM usage_log WHERE `+where+` AND timestamp >= ? GROUP BY tool_name`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tool string
		var count int64
		if err := rows.Scan(&tool, &count); err != nil {
			rows.Close()
			return nil, err
		}
		stats.ByTool[tool] = count
		stats.TotalCalls += count
	}
	rows.Close()

	recRows, err := s.db.QueryContext(ctx,
		`SELECT id, key_id, tool_name, timestamp, duration_ms, success FROM usage_log WHERE `+where+` AND timestamp >= ? ORDER BY timestamp DESC LIMIT 500`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer recRows.Close()

	for recRows.Next() {
		var r UsageRecord
		var ts string
		var success int
		if err := recRows.Scan(&r.ID, &r.KeyID, &r.ToolName, &ts, &r.DurationMs, &success); err != nil {
			return nil, err
		}
		r.Success = success != 0
		r.Timestamp, err = parseTime(ts)
		if err != nil {
			return nil, err
		}
		stats.Records = append(stats.Records, r)
	}
	if err := recRows.Err(); err != nil {
		return nil, err
	}

	var succCount int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM usage_log WHERE `+where+` AND timestamp >= ? AND success = 1`,
		args...,
	).Scan(&succCount); err != nil {
		return nil, err
	}
	stats.SuccessCalls = succCount
	return stats, nil
}
