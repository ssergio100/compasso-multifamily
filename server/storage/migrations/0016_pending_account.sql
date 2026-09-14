-- An unconfirmed account exists before its family. Keep the only additional
-- registration datum on that account until confirmation creates the tenant.
ALTER TABLE admin_user ADD COLUMN pending_family_name TEXT;

CREATE INDEX admin_user_pending_expiry_idx
ON admin_user(email_verified_at, updated_at)
WHERE email_verified_at IS NULL;

-- family_id was added to an existing SQLite table in migration 15 and could
-- not declare ON DELETE CASCADE there. Make family deletion cascade through
-- the existing device-owned foreign-key graph explicitly.
CREATE TRIGGER family_delete_devices
BEFORE DELETE ON family
BEGIN
  DELETE FROM device WHERE family_id=OLD.id;
END;

INSERT INTO schema_migrations(version) VALUES (16);
PRAGMA user_version = 16;
PRAGMA foreign_keys = ON;
