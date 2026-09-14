package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMigrationFifteenBackfillsSingleFamily(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "server.db")
	db := openDatabaseAtMigrationFourteen(t, databasePath)
	stamp := "2026-09-13T12:00:00Z"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO admin_user(id, login, password_hash, active, created_at, updated_at)
		VALUES ('admin-1', 'admin', 'hash', 1, ?, ?);
		INSERT INTO device(id, name, device_token_hash, policy_revision, created_at, updated_at)
		VALUES ('device-1', 'Computador', 'token-hash', 1, ?, ?);
	`, stamp, stamp, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var familyID, familyName, familyState string
	if err := store.db.QueryRowContext(ctx,
		`SELECT id, name, state FROM family`,
	).Scan(&familyID, &familyName, &familyState); err != nil {
		t.Fatal(err)
	}
	if familyID != "admin-1" || familyName != "Família" || familyState != "active" {
		t.Fatalf("family id=%q name=%q state=%q", familyID, familyName, familyState)
	}
	var memberFamilyID, deviceFamilyID string
	var generation int
	if err := store.db.QueryRowContext(ctx,
		`SELECT family_id FROM family_member WHERE admin_user_id='admin-1'`,
	).Scan(&memberFamilyID); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx,
		`SELECT family_id, credential_generation FROM device WHERE id='device-1'`,
	).Scan(&deviceFamilyID, &generation); err != nil {
		t.Fatal(err)
	}
	if memberFamilyID != familyID || deviceFamilyID != familyID || generation != 1 {
		t.Fatalf("member family=%q device family=%q generation=%d", memberFamilyID, deviceFamilyID, generation)
	}
	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO device(id, name, device_token_hash, policy_revision, created_at, updated_at)
		VALUES ('orphan', 'Órfão', '', 1, ?, ?)
	`, stamp, stamp); err == nil || !strings.Contains(err.Error(), "family_id is required") {
		t.Fatalf("orphan insert error=%v", err)
	}
}

func TestMigrationFifteenKeepsFreshDatabaseEmpty(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var families, members, devices int
	if err := store.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM family),
		       (SELECT COUNT(*) FROM family_member),
		       (SELECT COUNT(*) FROM device)
	`).Scan(&families, &members, &devices); err != nil {
		t.Fatal(err)
	}
	if families != 0 || members != 0 || devices != 0 {
		t.Fatalf("families=%d members=%d devices=%d", families, members, devices)
	}
}

func TestMigrationFifteenRejectsAmbiguousAdministrators(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "server.db")
	db := openDatabaseAtMigrationFourteen(t, databasePath)
	stamp := "2026-09-13T12:00:00Z"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO admin_user(id, login, password_hash, active, created_at, updated_at)
		VALUES
			('admin-1', 'one', 'hash', 1, ?, ?),
			('admin-2', 'two', 'hash', 1, ?, ?)
	`, stamp, stamp, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, databasePath)
	if store != nil {
		_ = store.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "0015_multi_family.sql") {
		t.Fatalf("migration error=%v", err)
	}
}

func openDatabaseAtMigrationFourteen(t *testing.T, databasePath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), "0015_") || strings.HasPrefix(entry.Name(), "0016_") {
			continue
		}
		script, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			_ = db.Close()
			t.Fatalf("apply %s: %v", entry.Name(), err)
		}
	}
	return db
}

func TestMigrationElevenAddsAndBackfillsVisualIdentities(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "server.db")
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE device (id TEXT PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE routine (id TEXT PRIMARY KEY, device_id TEXT NOT NULL, name TEXT NOT NULL);
		-- This focused fixture exercises migration 11 only. Version 12 depends on
		-- the complete command/activity schema built by the earlier migrations.
		INSERT INTO schema_migrations(version) VALUES (1),(2),(3),(4),(5),(6),(7),(8),(9),(10),(12),(15),(16);
		INSERT INTO device(id,name) VALUES ('device-1','PC antigo');
		INSERT INTO routine(id,device_id,name) VALUES ('sleep','device-1','Hora de dormir'),('reading','device-1','Leitura');
	`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var avatarKey, sleepIcon, readingIcon string
	if err := store.db.QueryRowContext(ctx, `SELECT avatar_key FROM device WHERE id='device-1'`).Scan(&avatarKey); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT icon_key FROM routine WHERE id='sleep'`).Scan(&sleepIcon); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT icon_key FROM routine WHERE id='reading'`).Scan(&readingIcon); err != nil {
		t.Fatal(err)
	}
	if avatarKey != "cat" || sleepIcon != "sleep" || readingIcon != "reading" {
		t.Fatalf("backfill avatar=%q sleep=%q reading=%q", avatarKey, sleepIcon, readingIcon)
	}
}

func TestMigrationThirteenPreservesExistingAvatarAndAcceptsSecondCollection(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "server.db")
	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	bootstrapTestOwner(t, store, now)
	existing, err := store.CreateDeviceWithAvatar(ctx, "Avatar antigo", "cat_bow", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `
		DELETE FROM schema_migrations WHERE version=13;
		PRAGMA user_version=12;
	`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	preserved, _, err := reopened.LoadDevice(ctx, existing.ID)
	if err != nil || preserved.AvatarKey != "cat_bow" {
		t.Fatalf("preserved avatar=%q err=%v", preserved.AvatarKey, err)
	}
	for index, avatarKey := range []string{"lion", "sheep", "tiger"} {
		device, createErr := reopened.CreateDeviceWithAvatar(
			ctx, "Avatar novo "+avatarKey, avatarKey, now.Add(time.Duration(index+1)*time.Second),
		)
		if createErr != nil || device.AvatarKey != avatarKey {
			t.Fatalf("create avatar %q: device=%+v err=%v", avatarKey, device, createErr)
		}
	}
}

func TestMigrationFourteenAddsChickToAnExistingCollection(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "server.db")
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE device (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			avatar_key TEXT NOT NULL DEFAULT 'cat' CHECK (avatar_key IN (
				'cat', 'dog', 'fox', 'rabbit', 'panda', 'owl', 'penguin', 'capybara',
				'lion', 'sheep', 'tiger',
				'cat_bow', 'rabbit_flower', 'panda_flower', 'fox_bow'
			))
		);
		INSERT INTO schema_migrations(version)
		VALUES (1),(2),(3),(4),(5),(6),(7),(8),(9),(10),(11),(12),(13),(15),(16);
		INSERT INTO device(id, name, avatar_key)
		VALUES ('existing-device', 'Avatar existente', 'cat_bow');
		PRAGMA user_version=13;
	`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var preserved string
	if err := store.db.QueryRowContext(ctx,
		`SELECT avatar_key FROM device WHERE id='existing-device'`,
	).Scan(&preserved); err != nil || preserved != "cat_bow" {
		t.Fatalf("preserved avatar=%q err=%v", preserved, err)
	}
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO device(id, name, avatar_key) VALUES ('chick-device', 'Pintinho', 'chick')`,
	); err != nil {
		t.Fatalf("migration 14 did not accept chick: %v", err)
	}
}

func TestMigrationTwelveCompactsControlQueueAndNormalizesState(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "server.db")
	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	bootstrapTestOwner(t, store, now)
	device, err := store.CreateDevice(ctx, "Trabalho", now)
	if err != nil {
		t.Fatal(err)
	}
	oldStamp := formatTime(now.Add(time.Second))
	newStamp := formatTime(now.Add(2 * time.Second))
	if _, err := store.db.ExecContext(ctx, `
		DROP INDEX device_command_one_pending_control_idx;
		DROP TRIGGER device_control_exclusive_insert;
		DROP TRIGGER device_control_exclusive_update;
		DELETE FROM schema_migrations WHERE version=12;
		PRAGMA user_version=11;
		UPDATE device_control
		SET monitoring_paused=1, manual_block=1, revision=5
		WHERE device_id=?;
		INSERT INTO device_command(id, device_id, kind, payload_json, created_at, acknowledged_at)
		VALUES
			('completed-block', ?, 'block_now', '{}', ?, ?),
			('older-pause', ?, 'pause_monitoring', '{}', ?, NULL),
			('latest-block', ?, 'block_now', '{}', ?, NULL),
			('pending-bonus', ?, 'add_bonus', '{"seconds":900}', ?, NULL);
		INSERT INTO activity(id, device_id, kind, origin, status, details_json, occurred_at, observed_at)
		VALUES
			('older-pause', ?, 'pause_monitoring', 'admin', 'waiting_device', '{}', ?, ?),
			('latest-block', ?, 'block_now', 'admin', 'waiting_device', '{}', ?, ?);
		INSERT INTO audit_event(uuid, device_id, kind, origin, payload_json, created_at)
		VALUES ('old-control-audit', ?, 'pause_monitoring', 'web', '{}', ?);
	`, device.ID,
		device.ID, oldStamp, oldStamp,
		device.ID, oldStamp,
		device.ID, newStamp,
		device.ID, newStamp,
		device.ID, oldStamp, oldStamp,
		device.ID, newStamp, newStamp,
		device.ID, oldStamp,
	); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rows, err := reopened.db.QueryContext(ctx, `SELECT id FROM device_command ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var commandIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		commandIDs = append(commandIDs, id)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(commandIDs) != 2 || commandIDs[0] != "latest-block" || commandIDs[1] != "pending-bonus" {
		t.Fatalf("commands after compaction=%v", commandIDs)
	}
	if _, err := reopened.LoadDeviceActivity(ctx, device.ID, "older-pause"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("superseded activity remained: %v", err)
	}
	if _, err := reopened.LoadDeviceActivity(ctx, device.ID, "latest-block"); err != nil {
		t.Fatalf("latest activity removed: %v", err)
	}
	control, err := reopened.LoadControl(ctx, device.ID)
	if err != nil || !control.MonitoringPaused || control.ManualBlock || control.Revision != 6 {
		t.Fatalf("normalized control=%+v err=%v", control, err)
	}
	var audits int
	if err := reopened.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_event WHERE uuid='old-control-audit'`).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("legacy control audit count=%d err=%v", audits, err)
	}
	if _, err := reopened.db.ExecContext(ctx, `
		INSERT INTO device_command(id, device_id, kind, payload_json, created_at)
		VALUES ('second-control', ?, 'pause_monitoring', '{}', ?)`, device.ID, newStamp); err == nil {
		t.Fatal("unique pending-control index accepted a second control command")
	}
}

func TestMigrationNineRepairsActivitySchemaRecordedButMissing(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "server.db")
	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 3, 0, 0, 0, time.UTC)
	bootstrapTestOwner(t, store, now)
	device, err := store.CreateDevice(ctx, "Servidor parcialmente migrado", now)
	if err != nil {
		t.Fatal(err)
	}
	commandID, err := store.QueueRemoteBonus(ctx, device.ID, 15*60, now)
	if err != nil {
		t.Fatal(err)
	}

	// Reproduce the production state: version 8 was recorded although its
	// human-history tables were not present. Version 9 must rebuild them from
	// the durable command and audit records.
	if _, err := store.db.ExecContext(ctx, `
		DROP TABLE activity_step;
		DROP TABLE activity;
		DROP TABLE activity_history_maintenance;
		DELETE FROM schema_migrations WHERE version IN (9, 10);
	`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	repaired, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open did not repair partial activity migration: %v", err)
	}
	defer repaired.Close()
	activity, err := repaired.LoadDeviceActivity(ctx, device.ID, commandID)
	if err != nil || activity.Status != "waiting_device" || findActivityStep(activity.Steps, "requested") == nil {
		t.Fatalf("reconstructed activity=%+v err=%v", activity, err)
	}
	var version int
	if err := repaired.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != 10 {
		t.Fatalf("repaired user_version=%d err=%v", version, err)
	}
}

func TestMigrationTenBackfillsExistingAdministrativeAudit(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "server.db")
	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 24, 3, 0, 0, 0, time.UTC)
	bootstrapTestOwner(t, store, now)
	device, err := store.CreateDevice(ctx, "Zorin", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `
		DELETE FROM activity;
		INSERT INTO audit_event(uuid, device_id, kind, origin, payload_json, created_at)
		VALUES ('historical-policy-change', ?, 'quotas_updated', 'web',
		        '{"warning_minutes":15}', ?);
		DELETE FROM schema_migrations WHERE version=10;
	`, device.ID, formatTime(now.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	activities, err := reopened.ListDeviceActivities(ctx, device.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, activity := range activities {
		counts[activity.Kind]++
	}
	if counts["device_created"] != 1 || counts["quotas_updated"] != 1 {
		t.Fatalf("historical activities were not backfilled: %+v", activities)
	}
}
