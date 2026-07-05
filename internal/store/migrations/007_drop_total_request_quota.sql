-- Total request quota has been removed. API key and usage-log total_calls stay
-- as analytics counters, but user-level total request quota state is dropped.

ALTER TABLE users DROP COLUMN total_calls;
ALTER TABLE tiers DROP COLUMN total_limit;
