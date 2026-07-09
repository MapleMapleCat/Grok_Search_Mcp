ALTER TABLE server_settings ADD COLUMN registration_mode TEXT NOT NULL DEFAULT 'free';

CREATE TABLE IF NOT EXISTS invite_codes (
    id                 TEXT PRIMARY KEY,
    code_hash          TEXT NOT NULL UNIQUE,
    code_prefix        TEXT NOT NULL,
    registration_limit INTEGER NOT NULL,
    registration_count INTEGER NOT NULL DEFAULT 0,
    enabled            INTEGER NOT NULL DEFAULT 1,
    created_by_user_id TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    CHECK (registration_limit > 0),
    CHECK (registration_count >= 0),
    CHECK (registration_count <= registration_limit)
);

CREATE INDEX IF NOT EXISTS idx_invite_codes_created_at ON invite_codes(created_at);
