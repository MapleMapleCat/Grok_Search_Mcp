-- Optional per-call debug capture stored when Server Settings debug mode is enabled.

ALTER TABLE usage_log ADD COLUMN debug_json TEXT NOT NULL DEFAULT '';
