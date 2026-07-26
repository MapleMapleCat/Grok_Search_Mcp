ALTER TABLE tiers ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1));

-- Preserve the historical behavior when tier0 still exists. Databases where it
-- was renamed or removed fall back to the first configured tier instead of
-- becoming unable to register users after the migration.
UPDATE tiers
SET is_default = CASE
    WHEN id = COALESCE(
        (SELECT id FROM tiers WHERE name = 'tier0' COLLATE NOCASE LIMIT 1),
        (SELECT id FROM tiers ORDER BY level ASC, name ASC, id ASC LIMIT 1)
    ) THEN 1
    ELSE 0
END;

CREATE UNIQUE INDEX idx_tiers_single_default
ON tiers(is_default)
WHERE is_default = 1;
