ALTER TABLE server_settings
    ADD COLUMN turnstile_enabled INTEGER NOT NULL DEFAULT 0 CHECK (turnstile_enabled IN (0, 1));

ALTER TABLE server_settings
    ADD COLUMN turnstile_site_key TEXT NOT NULL DEFAULT '';

ALTER TABLE server_settings
    ADD COLUMN turnstile_secret_key_ciphertext TEXT NOT NULL DEFAULT '';

ALTER TABLE server_settings
    ADD COLUMN turnstile_secret_key_nonce TEXT NOT NULL DEFAULT '';

ALTER TABLE server_settings
    ADD COLUMN turnstile_secret_key_encryption_version INTEGER NOT NULL DEFAULT 0;
