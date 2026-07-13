CREATE TABLE IF NOT EXISTS usage_log_debug_body_chunks (
    usage_id    INTEGER NOT NULL,
    body_kind   TEXT NOT NULL CHECK (body_kind IN ('request', 'response')),
    chunk_index INTEGER NOT NULL,
    body_data   BLOB NOT NULL,
    PRIMARY KEY (usage_id, body_kind, chunk_index),
    FOREIGN KEY (usage_id) REFERENCES usage_log(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_usage_debug_body_chunks_usage_id
    ON usage_log_debug_body_chunks(usage_id);
