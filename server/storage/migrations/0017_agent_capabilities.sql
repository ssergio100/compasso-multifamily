ALTER TABLE device ADD COLUMN agent_capabilities TEXT NOT NULL DEFAULT '';

INSERT INTO schema_migrations(version) VALUES (17);
PRAGMA user_version = 17;
