package session

import (
	"context"
	"errors"
	"testing"

	agentsession "github.com/ssergio100/compasso/agent/session"
)

type fakeConsoleObserver struct {
	console *Snapshot
	err     error
}

func (f *fakeConsoleObserver) ActiveConsole(context.Context) (*Snapshot, error) {
	if f.console == nil {
		return nil, f.err
	}
	copy := *f.console
	return &copy, f.err
}

type fakeLockRequester struct {
	sessionID uint32
	err       error
}

func (f *fakeLockRequester) RequestLock(_ context.Context, sessionID uint32) error {
	f.sessionID = sessionID
	return f.err
}

func newTestManager(t *testing.T, lock LockState) (*Manager, *fakeConsoleObserver, *fakeLockRequester) {
	t.Helper()
	observer := &fakeConsoleObserver{console: &Snapshot{
		SessionID: 1, AccountSID: "S-1-5-21-1002", Console: true,
		Protocol: "console", Connection: ConnectionActive, Lock: lock,
	}}
	locker := &fakeLockRequester{}
	manager, err := NewManager(observer, NewLockTracker(), locker, "S-1-5-21-1002", "install_1")
	if err != nil {
		t.Fatal(err)
	}
	return manager, observer, locker
}

func TestManagerMapsControlledConsoleToDaemonSession(t *testing.T) {
	manager, _, _ := newTestManager(t, LockUnlocked)
	sessions, err := manager.Sessions(context.Background(), "s-1-5-21-1002")
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
	current := sessions[0]
	if current.ID != "1" || current.AuthorizationID != "install_1_wts_1" ||
		current.Type != "windows-console" || current.State != "active" || !current.IsLocalGraphical() {
		t.Fatalf("mapped session=%+v", current)
	}
	locked, err := manager.IsLocked(context.Background(), current)
	if err != nil || locked {
		t.Fatalf("locked=%t err=%v", locked, err)
	}
}

func TestManagerLocksOnlyCurrentControlledConsole(t *testing.T) {
	manager, observer, locker := newTestManager(t, LockUnlocked)
	current := agentsession.Session{ID: "1"}
	if err := manager.Lock(context.Background(), current); err != nil || locker.sessionID != 1 {
		t.Fatalf("lock session=%d err=%v", locker.sessionID, err)
	}
	observer.console.SessionID = 2
	if err := manager.Lock(context.Background(), current); err == nil {
		t.Fatal("stale session was accepted")
	}
}

func TestManagerUnlockWaitsForUserAuthentication(t *testing.T) {
	manager, _, locker := newTestManager(t, LockLocked)
	if err := manager.Unlock(context.Background(), agentsession.Session{ID: "1"}); err != nil {
		t.Fatal(err)
	}
	if locker.sessionID != 0 {
		t.Fatal("unlock sent an IPC command")
	}
}

func TestManagerRejectsUnknownLockStateAndWrongSID(t *testing.T) {
	manager, observer, _ := newTestManager(t, LockUnknown)
	if _, err := manager.IsLocked(context.Background(), agentsession.Session{ID: "1"}); err == nil {
		t.Fatal("unknown lock state was accepted")
	}
	observer.console.AccountSID = "S-1-5-21-2000"
	sessions, err := manager.Sessions(context.Background(), "S-1-5-21-1002")
	if err != nil || len(sessions) != 0 {
		t.Fatalf("foreign sessions=%+v err=%v", sessions, err)
	}
}

func TestManagerPropagatesObserverAndLockerErrors(t *testing.T) {
	manager, observer, locker := newTestManager(t, LockUnlocked)
	observer.err = errors.New("observer failed")
	if _, err := manager.Sessions(context.Background(), "S-1-5-21-1002"); err == nil {
		t.Fatal("observer error was hidden")
	}
	observer.err = nil
	locker.err = errors.New("lock failed")
	if err := manager.Lock(context.Background(), agentsession.Session{ID: "1"}); err == nil {
		t.Fatal("locker error was hidden")
	}
}
