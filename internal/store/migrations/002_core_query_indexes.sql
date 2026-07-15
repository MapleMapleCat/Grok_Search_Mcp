-- Add indexes required by usage time-range and ownership queries for databases
-- that already applied the squashed baseline migration.

DROP INDEX IF EXISTS idx_usage_log_key_id;

CREATE INDEX IF NOT EXISTS idx_usage_log_key_id_timestamp
    ON usage_log(key_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_apikeys_user_id
    ON apikeys(user_id);
CREATE INDEX IF NOT EXISTS idx_users_tier_id
    ON users(tier_id);
