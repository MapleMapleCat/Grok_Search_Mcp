-- Keep an immutable audit trail of accounts created through invite codes.
-- Identifiers and display values are snapshots rather than foreign keys so
-- deleting an invite code or user does not erase the registration history.

CREATE TABLE invite_code_redemptions (
    id                 TEXT PRIMARY KEY,
    invite_code_id     TEXT NOT NULL,
    invite_code_prefix TEXT NOT NULL,
    user_id            TEXT NOT NULL UNIQUE,
    username           TEXT NOT NULL,
    redeemed_at        TEXT NOT NULL
);

CREATE INDEX idx_invite_code_redemptions_invite_time
    ON invite_code_redemptions(invite_code_id, redeemed_at DESC, id DESC);

CREATE INDEX idx_invite_code_redemptions_redeemed_at
    ON invite_code_redemptions(redeemed_at DESC);
