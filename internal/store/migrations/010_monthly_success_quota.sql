-- success_calls now represents the current UTC calendar month rather than a
-- lifetime counter. Existing counters are kept for the month in which this
-- migration runs; later months reset lazily when users are read or quota is
-- reserved.

ALTER TABLE users ADD COLUMN success_period TEXT NOT NULL DEFAULT '1970-01';

UPDATE users
SET success_period = strftime('%Y-%m', 'now')
WHERE success_period = '1970-01';
