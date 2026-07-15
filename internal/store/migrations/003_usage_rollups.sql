CREATE TABLE IF NOT EXISTS usage_hourly_rollups (
    key_id            TEXT NOT NULL REFERENCES apikeys(id) ON DELETE CASCADE,
    bucket_start      TEXT NOT NULL,
    tool_name         TEXT NOT NULL,
    total_calls       INTEGER NOT NULL,
    success_calls     INTEGER NOT NULL,
    duration_ms_total INTEGER NOT NULL,
    PRIMARY KEY (key_id, bucket_start, tool_name)
);

CREATE INDEX IF NOT EXISTS idx_usage_hourly_rollups_bucket_start
    ON usage_hourly_rollups(bucket_start);

CREATE INDEX IF NOT EXISTS idx_usage_hourly_rollups_key_id_bucket_start
    ON usage_hourly_rollups(key_id, bucket_start);

CREATE TABLE IF NOT EXISTS usage_daily_rollups (
    key_id            TEXT NOT NULL REFERENCES apikeys(id) ON DELETE CASCADE,
    bucket_start      TEXT NOT NULL,
    tool_name         TEXT NOT NULL,
    total_calls       INTEGER NOT NULL,
    success_calls     INTEGER NOT NULL,
    duration_ms_total INTEGER NOT NULL,
    PRIMARY KEY (key_id, bucket_start, tool_name)
);

CREATE INDEX IF NOT EXISTS idx_usage_daily_rollups_bucket_start
    ON usage_daily_rollups(bucket_start);

CREATE INDEX IF NOT EXISTS idx_usage_daily_rollups_key_id_bucket_start
    ON usage_daily_rollups(key_id, bucket_start);
