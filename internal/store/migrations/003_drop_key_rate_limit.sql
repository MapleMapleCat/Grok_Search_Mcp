-- SQLite 3.35+ supports DROP COLUMN; modernc.org/sqlite v1.52 bundles a recent SQLite.
-- Remove the unused key-level rate_limit column; rate limiting is enforced per-user (users.rpm).
ALTER TABLE apikeys DROP COLUMN rate_limit;
