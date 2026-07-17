ALTER TABLE server_settings
    ADD COLUMN operations_metrics_enabled INTEGER NOT NULL DEFAULT 0
        CHECK (operations_metrics_enabled IN (0, 1));
