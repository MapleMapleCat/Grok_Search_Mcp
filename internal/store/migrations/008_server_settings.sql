-- Runtime-tunable upstream configuration shown in the Server Settings panel.

CREATE TABLE IF NOT EXISTS server_settings (
    id              TEXT PRIMARY KEY,
    cpa_base_url    TEXT NOT NULL,
    cpa_api_key     TEXT NOT NULL,
    model           TEXT NOT NULL,
    timeout_seconds INTEGER NOT NULL,
    proxy_url       TEXT NOT NULL DEFAULT '',
    proxy_enabled   INTEGER NOT NULL DEFAULT 0,
    debug           INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
