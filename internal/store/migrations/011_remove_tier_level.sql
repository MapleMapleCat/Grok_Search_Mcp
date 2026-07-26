-- Tier already represents the quota classification. The former level field was
-- only a manual display order and duplicated that concept without affecting
-- authorization or quota behavior.
DROP INDEX IF EXISTS idx_tiers_level_name_id;
ALTER TABLE tiers DROP COLUMN level;

CREATE INDEX idx_tiers_created_id
ON tiers(created_at ASC, id ASC);
