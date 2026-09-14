ALTER TABLE enrollment ADD COLUMN installation_id TEXT;
ALTER TABLE enrollment ADD COLUMN token_fingerprint TEXT;

INSERT INTO schema_migrations(version) VALUES (6);
PRAGMA user_version = 6;
