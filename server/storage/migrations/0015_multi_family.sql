-- Introduce the tenant boundary without changing any device-owned table.
-- Existing installations are valid only when they have zero or one
-- administrator and no device can exist without that sole administrator.
CREATE TEMP TABLE migration_0015_guard (
  valid INTEGER NOT NULL CHECK (valid = 1)
);

INSERT INTO migration_0015_guard(valid)
SELECT CASE
  WHEN (SELECT COUNT(*) FROM admin_user) = 0
       AND (SELECT COUNT(*) FROM device) = 0 THEN 1
  WHEN (SELECT COUNT(*) FROM admin_user) = 1 THEN 1
  ELSE 0
END;

CREATE TABLE family (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
  state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'suspended')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE family_member (
  family_id TEXT NOT NULL REFERENCES family(id) ON DELETE CASCADE,
  admin_user_id TEXT NOT NULL REFERENCES admin_user(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role = 'owner'),
  created_at TEXT NOT NULL,
  PRIMARY KEY (family_id, admin_user_id),
  UNIQUE (admin_user_id)
);

ALTER TABLE admin_user ADD COLUMN email TEXT;
ALTER TABLE admin_user ADD COLUMN email_verified_at TEXT;
ALTER TABLE admin_user ADD COLUMN auth_generation INTEGER NOT NULL DEFAULT 1
  CHECK (auth_generation >= 1);

CREATE UNIQUE INDEX admin_user_email_unique_idx
ON admin_user(lower(email)) WHERE email IS NOT NULL;

ALTER TABLE device ADD COLUMN family_id TEXT REFERENCES family(id);
ALTER TABLE device ADD COLUMN credential_generation INTEGER NOT NULL DEFAULT 0
  CHECK (credential_generation >= 0);
ALTER TABLE device ADD COLUMN active_installation_id TEXT;
ALTER TABLE device ADD COLUMN installation_bound_at TEXT;
ALTER TABLE device ADD COLUMN online_until TEXT;

CREATE INDEX device_family_id_idx ON device(family_id, name, id);

-- SQLite cannot add a NOT NULL referenced column to a populated table. These
-- triggers make the invariant effective for every future insert or update.
CREATE TRIGGER device_family_required_insert
BEFORE INSERT ON device
WHEN NEW.family_id IS NULL
BEGIN
  SELECT RAISE(ABORT, 'device family_id is required');
END;

CREATE TRIGGER device_family_required_update
BEFORE UPDATE OF family_id ON device
WHEN NEW.family_id IS NULL
BEGIN
  SELECT RAISE(ABORT, 'device family_id is required');
END;

INSERT INTO family(id, name, state, created_at, updated_at)
SELECT id, 'Família', 'active', created_at, updated_at
FROM admin_user;

INSERT INTO family_member(family_id, admin_user_id, role, created_at)
SELECT id, id, 'owner', created_at
FROM admin_user;

UPDATE device
SET family_id = (SELECT id FROM family LIMIT 1),
    credential_generation = CASE WHEN device_token_hash = '' THEN 0 ELSE 1 END;

CREATE TABLE account_token (
  id TEXT PRIMARY KEY,
  admin_user_id TEXT NOT NULL REFERENCES admin_user(id) ON DELETE CASCADE,
  purpose TEXT NOT NULL CHECK (purpose IN ('verify_email', 'reset_password', 'change_email')),
  token_hash TEXT NOT NULL UNIQUE,
  pending_email TEXT,
  expires_at TEXT NOT NULL,
  consumed_at TEXT,
  created_at TEXT NOT NULL
);

CREATE INDEX account_token_admin_purpose_idx
ON account_token(admin_user_id, purpose, expires_at);

DROP TABLE migration_0015_guard;

INSERT INTO schema_migrations(version) VALUES (15);
PRAGMA user_version = 15;
