-- Token version for JWT revocation & role-change invalidation.
-- JWT carries this value as the "tv" claim; the auth middleware rejects any
-- token whose tv no longer matches the DB. Bumping token_version on role/enabled
-- changes (or explicit revoke) immediately invalidates all outstanding tokens
-- without needing a blacklist or jti tracking.

ALTER TABLE users ADD COLUMN token_version INTEGER NOT NULL DEFAULT 0;
