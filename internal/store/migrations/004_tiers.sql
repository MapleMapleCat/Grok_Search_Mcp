-- User tiers (tier0~tier6) with preset quotas; admin-managed.

CREATE TABLE IF NOT EXISTS tiers (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE COLLATE NOCASE,
    level         INTEGER NOT NULL,
    rpm           INTEGER NOT NULL DEFAULT 0,
    total_limit   INTEGER NOT NULL DEFAULT 0,
    success_limit INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

ALTER TABLE users ADD COLUMN tier_id TEXT REFERENCES tiers(id) ON DELETE SET NULL;

-- 预置 tier0~tier6（额度递增，0 = 不限，沿用现有约定）。
INSERT INTO tiers (id, name, level, rpm, total_limit, success_limit, created_at, updated_at) VALUES
('00000000-0000-4000-8000-tier0000000', 'tier0', 0,   10, 1000,   800,  datetime('now'), datetime('now')),
('00000000-0000-4000-8000-tier0000001', 'tier1', 1,   20, 5000,   4000, datetime('now'), datetime('now')),
('00000000-0000-4000-8000-tier0000002', 'tier2', 2,   40, 20000,  16000, datetime('now'), datetime('now')),
('00000000-0000-4000-8000-tier0000003', 'tier3', 3,   60, 50000,  40000, datetime('now'), datetime('now')),
('00000000-0000-4000-8000-tier0000004', 'tier4', 4,  120, 200000, 160000, datetime('now'), datetime('now')),
('00000000-0000-4000-8000-tier0000005', 'tier5', 5,  300, 1000000, 800000, datetime('now'), datetime('now')),
('00000000-0000-4000-8000-tier0000006', 'tier6', 6,    0, 0,      0,      datetime('now'), datetime('now'));

-- 已有用户回填到 tier0。
UPDATE users
SET tier_id = (SELECT id FROM tiers WHERE name = 'tier0' LIMIT 1)
WHERE tier_id IS NULL;
