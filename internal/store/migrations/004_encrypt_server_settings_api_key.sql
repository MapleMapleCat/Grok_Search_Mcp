ALTER TABLE server_settings ADD COLUMN cpa_api_key_ciphertext TEXT NOT NULL DEFAULT '';
ALTER TABLE server_settings ADD COLUMN cpa_api_key_nonce TEXT NOT NULL DEFAULT '';
ALTER TABLE server_settings ADD COLUMN cpa_api_key_encryption_version INTEGER NOT NULL DEFAULT 0;
