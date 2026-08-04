CREATE TABLE success_quota_reservations (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    period     TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_success_quota_reservations_user_id
    ON success_quota_reservations(user_id);
