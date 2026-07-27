ALTER TABLE invite_codes
    ADD COLUMN code_ciphertext TEXT NOT NULL DEFAULT '';

ALTER TABLE invite_codes
    ADD COLUMN code_nonce TEXT NOT NULL DEFAULT '';

ALTER TABLE invite_codes
    ADD COLUMN code_encryption_version INTEGER NOT NULL DEFAULT 0;
