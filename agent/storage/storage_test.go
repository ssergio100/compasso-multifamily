package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCreatesDatabaseAndAppliesAllMigrations(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "nested", "agent.db"))
	defer store.Close()

	var version int
	if err := store.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 6 {
		t.Fatalf("schema version = %d, want 6", version)
	}
	var integrity string
	if err := store.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity_check = %q, want ok", integrity)
	}
}

func TestPolicyReplacementSurvivesRestartAndRejectsStaleRevision(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	store := openTestStore(t, path)

	first := samplePolicy(10)
	if err := store.ReplacePolicy(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := samplePolicy(11)
	second.MonitoringPaused = true
	second.WeeklyQuota[time.Tuesday] = 45 * time.Minute
	second.Routines = []StoredRoutine{{
		ID: "study", Name: "Study", Days: selectedDays(time.Monday, time.Wednesday),
		Start: 18 * time.Hour, End: 19 * time.Hour, Enabled: true,
	}}
	if err := store.ReplacePolicy(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = openTestStore(t, path)
	defer store.Close()
	loaded, err := store.LoadPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertPolicy(t, loaded, second)

	if err := store.ReplacePolicy(ctx, first); !errors.Is(err, ErrStalePolicy) {
		t.Fatalf("stale replacement error = %v, want ErrStalePolicy", err)
	}
	loaded, err = store.LoadPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != 11 || loaded.WeeklyQuota[time.Tuesday] != 45*time.Minute {
		t.Fatalf("old policy reappeared: %+v", loaded)
	}
}

func TestPolicyReplacementRemovesAllRoutines(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "agent.db"))
	defer store.Close()
	withRoutine := samplePolicy(1)
	withRoutine.Routines = []StoredRoutine{{
		ID: "old-routine", Name: "Old", Days: selectedDays(time.Thursday),
		Start: 22 * time.Hour, End: 23 * time.Hour, Enabled: true,
	}}
	if err := store.ReplacePolicy(ctx, withRoutine); err != nil {
		t.Fatal(err)
	}
	withoutRoutines := samplePolicy(2)
	withoutRoutines.Routines = nil
	if err := store.ReplacePolicy(ctx, withoutRoutines); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadPolicy(ctx)
	if err != nil || len(loaded.Routines) != 0 {
		t.Fatalf("removed routine survived policy replacement: routines=%+v err=%v", loaded.Routines, err)
	}
}

func TestFailedPolicyReplacementRollsBackCompletely(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "agent.db"))
	defer store.Close()
	first := samplePolicy(20)
	if err := store.ReplacePolicy(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `
		CREATE TRIGGER fail_policy_write BEFORE INSERT ON weekly_quota
		WHEN NEW.weekday = 3 BEGIN SELECT RAISE(ABORT, 'simulated write failure'); END`); err != nil {
		t.Fatal(err)
	}

	second := samplePolicy(21)
	second.WeeklyQuota[time.Monday] = 15 * time.Minute
	if err := store.ReplacePolicy(ctx, second); err == nil {
		t.Fatal("expected simulated policy write to fail")
	}
	loaded, err := store.LoadPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertPolicy(t, loaded, first)
}

func TestCrashMidTransactionLeavesDatabaseIntact(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	store := openTestStore(t, path)
	first := samplePolicy(30)
	if err := store.ReplacePolicy(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(os.Args[0], "-test.run=TestCrashWriterHelper")
	command.Env = append(os.Environ(), "TEMPO_TEST_CRASH_HELPER=1", "TEMPO_TEST_DB="+path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("crash helper: %v: %s", err, output)
	}

	store = openTestStore(t, path)
	defer store.Close()
	loaded, err := store.LoadPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertPolicy(t, loaded, first)
	var integrity string
	if err := store.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("database integrity after crash = %q, err=%v", integrity, err)
	}
}

// TestCrashWriterHelper runs in a child process and intentionally exits with an
// open, partially modified transaction to model a daemon crash.
func TestCrashWriterHelper(t *testing.T) {
	if os.Getenv("TEMPO_TEST_CRASH_HELPER") != "1" {
		return
	}
	db, err := sql.Open(sqliteDriverName, sqliteDataSource(os.Getenv("TEMPO_TEST_DB")))
	if err != nil {
		os.Exit(2)
	}
	tx, err := db.Begin()
	if err != nil {
		os.Exit(3)
	}
	if _, err := tx.Exec(`DELETE FROM weekly_quota`); err != nil {
		os.Exit(4)
	}
	if _, err := tx.Exec(`UPDATE policy_state SET revision = 999 WHERE singleton_id = 1`); err != nil {
		os.Exit(5)
	}
	// Do not commit or close: this is the simulated process crash.
	os.Exit(0)
}

func TestUsageCheckpointBoundsLossAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	store := openTestStore(t, path)
	tracker, err := NewUsageTracker(ctx, store, "2026-08-08", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	if err := tracker.Add(ctx, 5*time.Second, now); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Add(ctx, 3*time.Second, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if tracker.Seconds() != 8 {
		t.Fatalf("in-memory usage = %d, want 8", tracker.Seconds())
	}
	// Simulate a crash: close storage without flushing the tracker. Only the
	// three seconds since the last checkpoint may be lost.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = openTestStore(t, path)
	defer store.Close()
	usage, err := store.LoadDailyUsage(ctx, "2026-08-08")
	if err != nil {
		t.Fatal(err)
	}
	if usage.SecondsUsed != 5 {
		t.Fatalf("durable usage after crash = %d, want 5", usage.SecondsUsed)
	}
	restarted, err := NewUsageTracker(ctx, store, "2026-08-08", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Seconds() != 5 {
		t.Fatalf("reconstructed usage = %d, want 5", restarted.Seconds())
	}
	// An older absolute checkpoint must never reduce the persisted counter.
	if err := store.CheckpointUsage(ctx, DailyUsage{
		LocalDate: "2026-08-08", SecondsUsed: 2, CheckpointAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	usage, err = store.LoadDailyUsage(ctx, "2026-08-08")
	if err != nil || usage.SecondsUsed != 5 {
		t.Fatalf("usage after stale checkpoint = %+v, err=%v", usage, err)
	}
}

func TestOfflineBonusAndEventSurviveRestartAndPolicyReplacement(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	store := openTestStore(t, path)
	if err := store.ReplacePolicy(ctx, samplePolicy(40)); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, time.August, 8, 15, 0, 0, 0, time.UTC)
	bonus := Bonus{UUID: "bonus-uuid-1", LocalDate: "2026-08-08", Seconds: 1800, Origin: "local", CreatedAt: createdAt}
	event := PendingEvent{
		UUID: bonus.UUID, Kind: "bonus_added", PayloadJSON: `{"seconds":1800}`, CreatedAt: createdAt,
	}
	if err := store.AddBonusWithEvent(ctx, bonus, event); err != nil {
		t.Fatal(err)
	}
	// Delivery retries may repeat the same local operation without duplicating it.
	if err := store.AddBonusWithEvent(ctx, bonus, event); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplacePolicy(ctx, samplePolicy(41)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = openTestStore(t, path)
	defer store.Close()
	total, err := store.TotalBonusSeconds(ctx, "2026-08-08")
	if err != nil || total != 1800 {
		t.Fatalf("bonus after restart = %d, err=%v, want 1800", total, err)
	}
	events, err := store.PendingEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].UUID != bonus.UUID {
		t.Fatalf("pending events after restart = %+v, want one bonus event", events)
	}
	if err := store.IncrementEventRetries(ctx, []string{bonus.UUID}); err != nil {
		t.Fatal(err)
	}
	events, err = store.PendingEvents(ctx, 10)
	if err != nil || len(events) != 1 || events[0].RetryCount != 1 {
		t.Fatalf("event retry state = %+v, err=%v", events, err)
	}
	if err := store.AcknowledgeEvent(ctx, bonus.UUID); err != nil {
		t.Fatal(err)
	}
	events, err = store.PendingEvents(ctx, 10)
	if err != nil || len(events) != 0 {
		t.Fatalf("events after acknowledgement = %+v, err=%v", events, err)
	}
}

func TestConfirmedSessionStatePersistsAndPreservesInFlightLocalBonus(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "agent.db")
	store := openTestStore(t, path)
	confirmedAt := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	initial := ConfirmedSessionState{
		Revision: 2, SessionID: "session-7", LocalDate: "2026-08-10",
		RemainingSeconds: 600, UsageSeconds: 10, ConfirmedAt: confirmedAt,
	}
	if err := store.SaveConfirmedSessionState(ctx, initial); err != nil {
		t.Fatal(err)
	}
	bonus := Bonus{
		UUID: "bonus-during-heartbeat", LocalDate: initial.LocalDate,
		Seconds: 300, Origin: "local", CreatedAt: confirmedAt.Add(time.Second),
	}
	event := PendingEvent{
		UUID: bonus.UUID, Kind: "bonus_added", PayloadJSON: `{"seconds":300}`,
		CreatedAt: bonus.CreatedAt,
	}
	if err := store.AddBonusWithEvent(ctx, bonus, event); err != nil {
		t.Fatal(err)
	}
	if err := store.AddBonusWithEvent(ctx, bonus, event); err != nil {
		t.Fatal(err)
	}
	state, ok := store.CurrentConfirmedSessionState()
	if !ok || state.RemainingSeconds != 900 {
		t.Fatalf("state after idempotent local bonus=%+v available=%t", state, ok)
	}

	// Model a response calculated before the local bonus was created. Because
	// the event is still pending, saving that response must retain the 300 s.
	inFlightResponse := ConfirmedSessionState{
		Revision: 2, SessionID: "session-7", LocalDate: initial.LocalDate,
		RemainingSeconds: 590, UsageSeconds: 20, ConfirmedAt: confirmedAt.Add(2 * time.Second),
	}
	if err := store.SaveConfirmedSessionState(ctx, inFlightResponse); err != nil {
		t.Fatal(err)
	}
	state, _ = store.CurrentConfirmedSessionState()
	if state.RemainingSeconds != 890 {
		t.Fatalf("in-flight response erased local bonus: %+v", state)
	}

	if err := store.AcknowledgeEvent(ctx, bonus.UUID); err != nil {
		t.Fatal(err)
	}
	serverIncludesBonus := inFlightResponse
	serverIncludesBonus.RemainingSeconds = 890
	serverIncludesBonus.ConfirmedAt = confirmedAt.Add(3 * time.Second)
	if err := store.SaveConfirmedSessionState(ctx, serverIncludesBonus); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = openTestStore(t, path)
	defer store.Close()
	state, ok = store.CurrentConfirmedSessionState()
	if !ok || state.RemainingSeconds != 890 || state.UsageSeconds != 20 {
		t.Fatalf("persisted confirmed state=%+v available=%t", state, ok)
	}
	usage, err := store.LoadDailyUsage(ctx, initial.LocalDate)
	if err != nil || usage.SecondsUsed != 20 {
		t.Fatalf("reconciled usage=%+v err=%v", usage, err)
	}
}

func TestStoreOperationTimesOutWhileConnectionIsBusyAndRecovers(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "agent.db"))
	defer store.Close()

	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	timeoutContext, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := store.LoadDailyUsage(timeoutContext, "2026-08-10"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("busy database error=%v, want deadline exceeded", err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadDailyUsage(ctx, "2026-08-10"); err != nil {
		t.Fatalf("database did not recover after releasing the connection: %v", err)
	}
}

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func samplePolicy(revision int64) PolicySnapshot {
	var quota [7]time.Duration
	for weekday := range quota {
		quota[weekday] = time.Duration(weekday+1) * time.Hour
	}
	return PolicySnapshot{
		Revision: revision, WarningMinutes: 10, LocalPasswordVerifier: "argon2id-verifier",
		WeeklyQuota: quota,
		Routines: []StoredRoutine{{
			ID: "sleep", Name: "Sleep", Days: selectedDays(time.Monday, time.Tuesday),
			Start: 22 * time.Hour, End: 8 * time.Hour, Enabled: true,
		}},
		UpdatedAt: time.Date(2026, time.August, 8, 10, int(revision%60), 0, 0, time.UTC),
	}
}

func selectedDays(days ...time.Weekday) (selected [7]bool) {
	for _, day := range days {
		selected[day] = true
	}
	return selected
}

func assertPolicy(t *testing.T, got, want PolicySnapshot) {
	t.Helper()
	if got.Revision != want.Revision || got.MonitoringPaused != want.MonitoringPaused ||
		got.ManualBlock != want.ManualBlock || got.WarningMinutes != want.WarningMinutes ||
		got.LocalPasswordVerifier != want.LocalPasswordVerifier || !got.UpdatedAt.Equal(want.UpdatedAt) ||
		got.WeeklyQuota != want.WeeklyQuota || len(got.Routines) != len(want.Routines) {
		t.Fatalf("policy mismatch\n got: %+v\nwant: %+v", got, want)
	}
	for i := range got.Routines {
		if got.Routines[i] != want.Routines[i] {
			t.Fatalf("routine %d mismatch\n got: %+v\nwant: %+v", i, got.Routines[i], want.Routines[i])
		}
	}
}
