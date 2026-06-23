-- Panel users, key ownership, usage success flag, legacy key migration.

CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    username        TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash   TEXT NOT NULL,
    role            TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    enabled         INTEGER NOT NULL DEFAULT 1,
    rpm             INTEGER NOT NULL DEFAULT 0,
    total_limit     INTEGER NOT NULL DEFAULT 0,
    success_limit   INTEGER NOT NULL DEFAULT 0,
    total_calls     INTEGER NOT NULL DEFAULT 0,
    success_calls   INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

ALTER TABLE apikeys ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE usage_log ADD COLUMN success INTEGER NOT NULL DEFAULT 1;

-- Assign pre-existing keys to a synthetic system user (disabled, not for login).
INSERT INTO users (
    id, username, password_hash, role, enabled,
    rpm, total_limit, success_limit, total_calls, success_calls,
    created_at, updated_at
)
SELECT
    '00000000-0000-4000-8000-000000000001',
    '__legacy__',
    '$2a$10$legacykeysnotloginhashplaceholder000000000000000000',
    'user',
    0,
    0, 0, 0, 0, 0,
    datetime('now'), datetime('now')
WHERE EXISTS (SELECT 1 FROM apikeys WHERE user_id IS NULL)
  AND NOT EXISTS (SELECT 1 FROM users WHERE id = '00000000-0000-4000-8000-000000000001');

UPDATE apikeys
SET user_id = '00000000-0000-4000-8000-000000000001'
WHERE user_id IS NULL;