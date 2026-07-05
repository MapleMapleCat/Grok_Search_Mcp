-- Quota source is tier only. The per-user rpm/total_limit/success_limit columns
-- are removed so there is no hidden "historical" limit that could silently apply
-- when an admin clears a user's tier. First ensure every user is pinned to a tier
-- (NULL -> tier0, the default floor), then drop the redundant columns.

UPDATE users
SET tier_id = (SELECT id FROM tiers WHERE name = 'tier0' LIMIT 1)
WHERE tier_id IS NULL;

ALTER TABLE users DROP COLUMN rpm;
ALTER TABLE users DROP COLUMN total_limit;
ALTER TABLE users DROP COLUMN success_limit;
