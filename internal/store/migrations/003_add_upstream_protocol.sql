ALTER TABLE server_settings
ADD COLUMN upstream_protocol TEXT NOT NULL DEFAULT 'responses';
