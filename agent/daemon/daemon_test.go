package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssergio100/compasso/agent/policy"
	"github.com/ssergio100/compasso/agent/session"
	"github.com/ssergio100/compasso/agent/storage"
)

type fakeSessions struct {
	sessions       []session.Session
	lockRequests   []string
	unlockRequests []string
	locked         map[string]bool
	lockErr        error
	unlockErr      error
}

type fakeSynchronizationSource struct {
	graphicalSessionActive bool
	graphicalSessionID     string
	online                 bool
	paused                 bool
	blocked                bool
	revision               int64
}

func (f *fakeSynchronizationSource) RemoteControl() (bool, bool, bool, int64) {
	return f.online, f.paused, f.blocked, f.revision
}

func TestRemoteControlIsIgnoredOfflineWhileLocalPolicyRemainsActive(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, testPolicy(1, now.Weekday(), time.Hour)); err != nil {
		t.Fatal(err)
	}
	synchronization := &fakeSynchronizationSource{online: true, blocked: true}
	d, _ := New(store, graphicalFake(), "child", time.Second)
	d.SetSynchronizationSource(synchronization)
	if err := store.SaveConfirmedSessionState(ctx, storage.ConfirmedSessionState{
		Revision: 1, SessionID: "3", LocalDate: now.Format("2006-01-02"), ConfirmedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	blocked, err := d.Step(ctx, now)
	if err != nil || blocked.Decision.Reason != policy.ReasonManualBlock {
		t.Fatalf("online remote block=%+v err=%v", blocked, err)
	}
	synchronization.online = false
	offline, err := d.Step(ctx, now.Add(time.Second))
	if err != nil || !offline.Decision.Allowed || offline.Decision.Reason != policy.ReasonAllowed {
		t.Fatalf("stale remote block survived offline=%+v err=%v", offline, err)
	}

	local := testPolicy(2, now.Weekday(), 0)
	if err := store.ReplacePolicy(ctx, local); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfirmedSessionState(ctx, storage.ConfirmedSessionState{
		Revision: 2, SessionID: "3", LocalDate: now.Format("2006-01-02"), ConfirmedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	synchronization.online, synchronization.paused, synchronization.blocked = true, true, false
	paused, err := d.Step(ctx, now.Add(2*time.Second))
	if err != nil || paused.Decision.Reason != policy.ReasonPaused || paused.Decision.ShouldCount {
		t.Fatalf("online remote pause=%+v err=%v", paused, err)
	}
	synchronization.online = false
	policyBlocked, err := d.Step(ctx, now.Add(2*time.Second))
	if err != nil || policyBlocked.Decision.Reason != policy.ReasonQuota {
		t.Fatalf("offline policy was not enforced=%+v err=%v", policyBlocked, err)
	}
}

func TestRemoteBlockAndClearCompleteOnlyAfterGraphicalEffect(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, testPolicy(1, start.Weekday(), time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfirmedSessionState(ctx, storage.ConfirmedSessionState{
		Revision: 1, SessionID: "3", LocalDate: start.Format("2006-01-02"),
		RemainingSeconds: 3600, ConfirmedAt: start,
	}); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	synchronization := &fakeSynchronizationSource{online: true, blocked: true, revision: 2}
	policyDaemon, _ := New(store, sessions, "child", time.Second)
	policyDaemon.SetSynchronizationSource(synchronization)
	if err := store.StageControlEffect(ctx, "block-2", 2, "block_now", start); err != nil {
		t.Fatal(err)
	}
	if _, err := policyDaemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	if len(sessions.lockRequests) != 1 {
		t.Fatalf("block requests=%v", sessions.lockRequests)
	}
	if ids, _ := store.AppliedCommandIDs(ctx, 10); len(ids) != 0 {
		t.Fatalf("block acknowledged before observation: %v", ids)
	}
	if _, err := policyDaemon.Step(ctx, start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if ids, _ := store.AppliedCommandIDs(ctx, 10); len(ids) != 1 || ids[0] != "block-2" {
		t.Fatalf("confirmed block acknowledgements=%v", ids)
	}

	synchronization.blocked, synchronization.revision = false, 3
	if err := store.StageControlEffect(ctx, "clear-3", 3, "clear_manual_block", start.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := policyDaemon.Step(ctx, start.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(sessions.unlockRequests) != 1 || sessions.unlockRequests[0] != "3" {
		t.Fatalf("unlock requests=%v", sessions.unlockRequests)
	}
	if ids, _ := store.AppliedCommandIDs(ctx, 10); len(ids) != 1 {
		t.Fatalf("clear acknowledged before observation: %v", ids)
	}
	if _, err := policyDaemon.Step(ctx, start.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if ids, _ := store.AppliedCommandIDs(ctx, 10); len(ids) != 2 {
		t.Fatalf("confirmed clear acknowledgements=%v", ids)
	}
}

func TestRemoteBlockWaitsForGraphicalSession(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, testPolicy(1, start.Weekday(), time.Hour)); err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{}
	synchronization := &fakeSynchronizationSource{online: true, blocked: true, revision: 2}
	policyDaemon, _ := New(store, sessions, "child", time.Second)
	policyDaemon.SetSynchronizationSource(synchronization)
	if err := store.StageControlEffect(ctx, "block-2", 2, "block_now", start); err != nil {
		t.Fatal(err)
	}
	if _, err := policyDaemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	if ids, _ := store.AppliedCommandIDs(ctx, 10); len(ids) != 0 {
		t.Fatalf("sessionless block was acknowledged: %v", ids)
	}
	if _, available, err := store.PendingControlEffect(ctx); err != nil || !available {
		t.Fatalf("sessionless block pending=%t err=%v", available, err)
	}
}

func (f *fakeSynchronizationSource) SetGraphicalSession(active bool, sessionID string, _ bool) {
	f.graphicalSessionActive = active
	f.graphicalSessionID = sessionID
}

func (f *fakeSessions) Sessions(context.Context, string) ([]session.Session, error) {
	return append([]session.Session(nil), f.sessions...), nil
}

func (f *fakeSessions) Lock(_ context.Context, current session.Session) error {
	f.lockRequests = append(f.lockRequests, current.ID)
	if f.lockErr != nil {
		return f.lockErr
	}
	if f.locked == nil {
		f.locked = make(map[string]bool)
	}
	f.locked[current.ID] = true
	return nil
}

func (f *fakeSessions) Unlock(_ context.Context, current session.Session) error {
	f.unlockRequests = append(f.unlockRequests, current.ID)
	if f.unlockErr != nil {
		return f.unlockErr
	}
	if f.locked == nil {
		f.locked = make(map[string]bool)
	}
	f.locked[current.ID] = false
	return nil
}

func (f *fakeSessions) IsLocked(_ context.Context, current session.Session) (bool, error) {
	return f.locked[current.ID], nil
}

func TestQuotaExpiryLocksGraphicalSessionWithoutLogout(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, testPolicy(1, start.Weekday(), 3*time.Second)); err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{sessions: []session.Session{{
		ID: "3", User: "child", Type: "wayland", Class: "user", State: "active",
	}}}
	daemon, err := New(store, sessions, "child", time.Second)
	if err != nil {
		t.Fatal(err)
	}

	status, err := daemon.Step(ctx, start)
	if err != nil || !status.Decision.Allowed {
		t.Fatalf("initial step = %+v, err=%v", status, err)
	}
	status, err = daemon.Step(ctx, start.Add(2*time.Second))
	if err != nil || status.UsageSeconds != 2 || !status.Decision.Allowed {
		t.Fatalf("second step = %+v, err=%v", status, err)
	}
	status, err = daemon.Step(ctx, start.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if status.Decision.Allowed || status.Decision.Reason != policy.ReasonQuota || status.UsageSeconds != 3 {
		t.Fatalf("expiry status = %+v", status)
	}
	if len(sessions.lockRequests) != 1 || sessions.lockRequests[0] != "3" {
		t.Fatalf("lock requests = %v, want [3]", sessions.lockRequests)
	}
	durable, err := store.LoadDailyUsage(ctx, start.Format("2006-01-02"))
	if err != nil || durable.SecondsUsed != 3 {
		t.Fatalf("durable usage at block = %+v, err=%v", durable, err)
	}

	// A session already confirmed as locked is not requested repeatedly.
	if _, err := daemon.Step(ctx, start.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(sessions.lockRequests) != 1 {
		t.Fatalf("same blocked session locked repeatedly: %v", sessions.lockRequests)
	}
	// A continuing blocked state is retried at a bounded rate, even if the
	// logind hint changes in the meantime.
	sessions.locked["3"] = false
	if _, err := daemon.Step(ctx, start.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(sessions.lockRequests) != 1 {
		t.Fatalf("session retried before interval: %v", sessions.lockRequests)
	}
	if _, err := daemon.Step(ctx, start.Add(3*time.Second+sessionLockRetryInterval)); err != nil {
		t.Fatal(err)
	}
	if len(sessions.lockRequests) != 2 {
		t.Fatalf("blocked session was not retried after interval: %v", sessions.lockRequests)
	}
}

func TestDelayedCycleDoesNotCountPastQuotaExpiry(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, testPolicy(1, start.Weekday(), 3*time.Second)); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	daemon, _ := New(store, sessions, "child", time.Second)
	if _, err := daemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	status, err := daemon.Step(ctx, start.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if status.UsageSeconds != 3 || status.Decision.Reason != policy.ReasonQuota || len(sessions.lockRequests) != 1 {
		t.Fatalf("delayed expiry status=%+v locks=%v", status, sessions.lockRequests)
	}
}

func TestSynchronizedDaemonConsumesConfirmedBalanceWithoutRecalculatingQuota(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	// The copied policy deliberately says eight hours. In synchronized mode the
	// three-second server anchor is the only balance authority.
	if err := store.ReplacePolicy(ctx, testPolicy(4, start.Weekday(), 8*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfirmedSessionState(ctx, storage.ConfirmedSessionState{
		Revision: 4, SessionID: "3", LocalDate: start.Format("2006-01-02"),
		RemainingSeconds: 3, UsageSeconds: 0, ConfirmedAt: start,
	}); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	synchronization := &fakeSynchronizationSource{online: true}
	policyDaemon, err := New(store, sessions, "child", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	policyDaemon.SetSynchronizationSource(synchronization)

	first, err := policyDaemon.Step(ctx, start)
	if err != nil || !first.Decision.Allowed || first.Decision.Remaining != 3*time.Second {
		t.Fatalf("first confirmed decision=%+v err=%v", first, err)
	}
	second, err := policyDaemon.Step(ctx, start.Add(2*time.Second))
	if err != nil || second.Decision.Remaining != time.Second || second.UsageSeconds != 2 {
		t.Fatalf("decremented confirmed decision=%+v err=%v", second, err)
	}
	expired, err := policyDaemon.Step(ctx, start.Add(3*time.Second))
	if err != nil || expired.Decision.Reason != policy.ReasonQuota || len(sessions.lockRequests) != 1 {
		t.Fatalf("confirmed balance expiry=%+v locks=%v err=%v", expired, sessions.lockRequests, err)
	}
	if !synchronization.graphicalSessionActive || synchronization.graphicalSessionID != "3" {
		t.Fatalf("reported graphical session active=%t id=%q", synchronization.graphicalSessionActive, synchronization.graphicalSessionID)
	}
}

func TestQuotaCycleEmitsOneMinuteDesktopAlert(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	snapshot := testPolicy(1, start.Weekday(), 2*time.Minute)
	snapshot.WarningMinutes = 1
	if err := store.ReplacePolicy(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	policyDaemon, err := New(store, graphicalFake(), "child", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policyDaemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	status, err := policyDaemon.Step(ctx, start.Add(time.Minute))
	if err != nil || len(status.DueAlerts) != 1 || status.DueAlerts[0].Kind != "one_minute" {
		t.Fatalf("status due alerts=%+v err=%v", status.DueAlerts, err)
	}
}

func TestLockedSessionConsumesAlertWithoutDeliveringItAfterUnlock(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	snapshot := testPolicy(1, start.Weekday(), 2*time.Minute)
	snapshot.WarningMinutes = 1
	if err := store.ReplacePolicy(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	sessions.locked = map[string]bool{"3": true}
	policyDaemon, err := New(store, sessions, "child", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policyDaemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	locked, err := policyDaemon.Step(ctx, start.Add(time.Minute))
	if err != nil || len(locked.DueAlerts) != 0 || locked.UsageSeconds != 60 {
		t.Fatalf("locked session alerts=%+v err=%v", locked.DueAlerts, err)
	}
	sessions.locked["3"] = false
	unlocked, err := policyDaemon.Step(ctx, start.Add(time.Minute+time.Second))
	if err != nil || len(unlocked.DueAlerts) != 0 {
		t.Fatalf("unlock replayed alerts=%+v err=%v", unlocked.DueAlerts, err)
	}
}

func TestRoutineStartTerminatesSession(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 21, 59, 59, 0, time.Local)
	snapshot := testPolicy(1, start.Weekday(), time.Hour)
	snapshot.Routines = []storage.StoredRoutine{{
		ID: "sleep", Name: "Sleep", Days: daySet(start.Weekday()),
		Start: 22 * time.Hour, End: 8 * time.Hour, Enabled: true,
	}}
	if err := store.ReplacePolicy(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	daemon, _ := New(store, sessions, "child", time.Second)
	if _, err := daemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	status, err := daemon.Step(ctx, start.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if status.Decision.Reason != policy.ReasonRoutine || len(sessions.lockRequests) != 1 {
		t.Fatalf("routine status=%+v locks=%v", status, sessions.lockRequests)
	}
}

func TestRoutineStartRetriesLockWhenLogindHintIsAlreadySet(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 21, 59, 59, 0, time.Local)
	snapshot := testPolicy(1, start.Weekday(), time.Hour)
	snapshot.Routines = []storage.StoredRoutine{{
		ID: "sleep", Name: "Sleep", Days: daySet(start.Weekday()),
		Start: 22 * time.Hour, End: 8 * time.Hour, Enabled: true,
	}}
	if err := store.ReplacePolicy(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	sessions.locked = map[string]bool{"3": true}
	daemon, _ := New(store, sessions, "child", time.Second)
	if _, err := daemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	status, err := daemon.Step(ctx, start.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if status.Decision.Reason != policy.ReasonRoutine || len(sessions.lockRequests) != 1 {
		t.Fatalf("routine status=%+v locks=%v", status, sessions.lockRequests)
	}
}

func TestPausedMonitoringDoesNotCountOrTerminate(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	snapshot := testPolicy(1, start.Weekday(), 0)
	snapshot.MonitoringPaused = true
	if err := store.ReplacePolicy(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	daemon, _ := New(store, sessions, "child", time.Second)
	if _, err := daemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	status, err := daemon.Step(ctx, start.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !status.Decision.Allowed || status.Decision.Reason != policy.ReasonPaused || status.UsageSeconds != 0 {
		t.Fatalf("paused status = %+v", status)
	}
	if len(sessions.lockRequests) != 0 {
		t.Fatalf("paused monitoring locked sessions: %v", sessions.lockRequests)
	}
}

func TestPauseBeforeExpiryStopsCounterAndPreventsLock(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, testPolicy(1, start.Weekday(), 3*time.Second)); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	daemon, _ := New(store, sessions, "child", time.Second)
	if _, err := daemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	status, err := daemon.Step(ctx, start.Add(2*time.Second))
	if err != nil || status.UsageSeconds != 2 {
		t.Fatalf("usage before pause = %+v, err=%v", status, err)
	}
	paused := testPolicy(2, start.Weekday(), 3*time.Second)
	paused.MonitoringPaused = true
	if err := store.ReplacePolicy(ctx, paused); err != nil {
		t.Fatal(err)
	}
	status, err = daemon.Step(ctx, start.Add(2500*time.Millisecond))
	if err != nil || !status.Decision.Allowed || status.Decision.Reason != policy.ReasonPaused {
		t.Fatalf("pause transition = %+v, err=%v", status, err)
	}
	status, err = daemon.Step(ctx, start.Add(10*time.Second))
	if err != nil || status.UsageSeconds != 2 || len(sessions.lockRequests) != 0 {
		t.Fatalf("after pause = %+v, err=%v locks=%v", status, err, sessions.lockRequests)
	}
}

func TestNoGraphicalSessionDoesNotCount(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, testPolicy(1, start.Weekday(), time.Hour)); err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{sessions: []session.Session{{
		ID: "4", User: "child", Type: "tty", Class: "user", State: "active",
	}}}
	daemon, _ := New(store, sessions, "child", time.Second)
	if _, err := daemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	status, err := daemon.Step(ctx, start.Add(30*time.Second))
	if err != nil || status.UsageSeconds != 0 {
		t.Fatalf("non-graphical status=%+v err=%v", status, err)
	}
}

func TestGraphicalLogoutStopsUsageWhileLogindSessionLingersOnline(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, testPolicy(1, start.Weekday(), time.Hour)); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	synchronization := &fakeSynchronizationSource{online: true}
	policyDaemon, _ := New(store, sessions, "child", time.Second)
	policyDaemon.SetSynchronizationSource(synchronization)
	if err := store.SaveConfirmedSessionState(ctx, storage.ConfirmedSessionState{
		Revision: 1, SessionID: "3", LocalDate: start.Format("2006-01-02"),
		RemainingSeconds: 3600, ConfirmedAt: start,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := policyDaemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	active, err := policyDaemon.Step(ctx, start.Add(4*time.Second))
	if err != nil || active.UsageSeconds != 4 || !active.GraphicalSession || !synchronization.graphicalSessionActive {
		t.Fatalf("active session status=%+v synchronized=%+v err=%v", active, synchronization, err)
	}

	// logind may keep a graphical session as "online" briefly after logout. It
	// is no longer established as the active local desktop and must not keep the
	// counter running or be advertised in the next heartbeat.
	sessions.sessions[0].State = "online"
	loggedOut, err := policyDaemon.Step(ctx, start.Add(5*time.Second))
	if err != nil || loggedOut.UsageSeconds != 5 || loggedOut.GraphicalSession || synchronization.graphicalSessionActive || synchronization.graphicalSessionID != "" {
		t.Fatalf("logout transition status=%+v synchronized=%+v err=%v", loggedOut, synchronization, err)
	}
	afterLogout, err := policyDaemon.Step(ctx, start.Add(35*time.Second))
	if err != nil || afterLogout.UsageSeconds != 5 || afterLogout.GraphicalSession {
		t.Fatalf("usage continued after logout: status=%+v err=%v", afterLogout, err)
	}
	durable, err := store.LoadDailyUsage(ctx, start.Format("2006-01-02"))
	if err != nil || durable.SecondsUsed != 5 {
		t.Fatalf("durable usage after logout=%+v err=%v", durable, err)
	}
}

func TestBlockedReloginWaitsUntilLogindReportsActive(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, testPolicy(1, start.Weekday(), 0)); err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{sessions: []session.Session{{
		ID: "5", User: "child", Type: "wayland", Class: "user", State: "opening",
	}}}
	daemon, _ := New(store, sessions, "child", time.Second)
	if _, err := daemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.Step(ctx, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(sessions.lockRequests) != 0 {
		t.Fatalf("opening session locked: %v", sessions.lockRequests)
	}
	sessions.sessions[0].State = "active"
	if _, err := daemon.Step(ctx, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(sessions.lockRequests) != 1 || sessions.lockRequests[0] != "5" {
		t.Fatalf("active session was not locked: %v", sessions.lockRequests)
	}
}

func TestBlockedLoginWaitsForConfirmedSessionState(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, testPolicy(1, start.Weekday(), 0)); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	synchronization := &fakeSynchronizationSource{online: true}
	policyDaemon, _ := New(store, sessions, "child", time.Second)
	policyDaemon.SetSynchronizationSource(synchronization)

	status, err := policyDaemon.Step(ctx, start)
	if err != nil || !status.AwaitingSynchronization || len(sessions.lockRequests) != 0 {
		t.Fatalf("initial blocked login status=%+v err=%v locks=%v", status, err, sessions.lockRequests)
	}
	status, err = policyDaemon.Step(ctx, start.Add(time.Second))
	if err != nil || !status.AwaitingSynchronization || len(sessions.lockRequests) != 0 {
		t.Fatalf("offline blocked login status=%+v err=%v locks=%v", status, err, sessions.lockRequests)
	}

	if err := store.SaveConfirmedSessionState(ctx, storage.ConfirmedSessionState{
		Revision: 1, SessionID: "3", LocalDate: start.Format("2006-01-02"),
		RemainingSeconds: 0, UsageSeconds: 0, ConfirmedAt: start.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	status, err = policyDaemon.Step(ctx, start.Add(2*time.Second))
	if err != nil || status.AwaitingSynchronization || len(sessions.lockRequests) != 1 {
		t.Fatalf("confirmed blocked login status=%+v err=%v locks=%v", status, err, sessions.lockRequests)
	}
}

func TestBlockedLoginRemainsOpenWhenConfirmedStateAddsTime(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, testPolicy(1, start.Weekday(), 0)); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	synchronization := &fakeSynchronizationSource{online: true}
	policyDaemon, _ := New(store, sessions, "child", time.Second)
	policyDaemon.SetSynchronizationSource(synchronization)
	if _, err := policyDaemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}

	if err := store.ReplacePolicy(ctx, testPolicy(2, start.Weekday(), time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfirmedSessionState(ctx, storage.ConfirmedSessionState{
		Revision: 2, SessionID: "3", LocalDate: start.Format("2006-01-02"),
		RemainingSeconds: 60, UsageSeconds: 0, ConfirmedAt: start.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	status, err := policyDaemon.Step(ctx, start.Add(time.Second))
	if err != nil || !status.Decision.Allowed || status.AwaitingSynchronization || len(sessions.lockRequests) != 0 {
		t.Fatalf("updated blocked login status=%+v err=%v locks=%v", status, err, sessions.lockRequests)
	}
}

func TestMidnightSplitsUsageBetweenLocalDates(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	location := time.FixedZone("test", -3*60*60)
	start := time.Date(2026, time.August, 9, 23, 59, 59, 0, location)
	snapshot := testPolicy(1, start.Weekday(), time.Hour)
	snapshot.WeeklyQuota[start.AddDate(0, 0, 1).Weekday()] = time.Hour
	if err := store.ReplacePolicy(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	daemon, _ := New(store, sessions, "child", time.Second)
	if _, err := daemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	status, err := daemon.Step(ctx, start.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	oldDay, err := store.LoadDailyUsage(ctx, "2026-08-09")
	if err != nil {
		t.Fatal(err)
	}
	if oldDay.SecondsUsed != 1 || status.UsageSeconds != 1 {
		t.Fatalf("usage split: old=%d new=%d, want 1 and 1", oldDay.SecondsUsed, status.UsageSeconds)
	}
}

func TestLockFailureDoesNotDoubleCountElapsedTime(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	defer store.Close()
	start := time.Date(2026, time.August, 10, 14, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, testPolicy(1, start.Weekday(), 3*time.Second)); err != nil {
		t.Fatal(err)
	}
	sessions := graphicalFake()
	sessions.lockErr = errors.New("temporary logind failure")
	daemon, _ := New(store, sessions, "child", time.Second)
	if _, err := daemon.Step(ctx, start); err != nil {
		t.Fatal(err)
	}
	status, err := daemon.Step(ctx, start.Add(3*time.Second))
	if err == nil || status.UsageSeconds != 3 {
		t.Fatalf("failed lock status=%+v err=%v", status, err)
	}
	sessions.lockErr = nil
	status, err = daemon.Step(ctx, start.Add(4*time.Second))
	if err != nil || status.UsageSeconds != 3 || len(sessions.lockRequests) != 1 {
		t.Fatalf("early retry status=%+v err=%v locks=%v", status, err, sessions.lockRequests)
	}
	status, err = daemon.Step(ctx, start.Add(3*time.Second+sessionLockRetryInterval))
	if err != nil || status.UsageSeconds != 3 || len(sessions.lockRequests) != 2 {
		t.Fatalf("bounded retry status=%+v err=%v locks=%v", status, err, sessions.lockRequests)
	}
}

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testPolicy(revision int64, weekday time.Weekday, quota time.Duration) storage.PolicySnapshot {
	var quotas [7]time.Duration
	quotas[weekday] = quota
	return storage.PolicySnapshot{
		Revision: revision, WeeklyQuota: quotas,
		UpdatedAt: time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC),
	}
}

func graphicalFake() *fakeSessions {
	return &fakeSessions{sessions: []session.Session{{
		ID: "3", User: "child", Type: "wayland", Class: "user", State: "active",
	}}}
}

func daySet(days ...time.Weekday) (result [7]bool) {
	for _, day := range days {
		result[day] = true
	}
	return result
}
