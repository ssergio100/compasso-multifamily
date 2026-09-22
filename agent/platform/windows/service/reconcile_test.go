package service

import (
	"testing"

	windowsession "github.com/ssergio100/compasso/agent/platform/windows/session"
)

func TestReconcileConsoleLockSeedsInitialObservation(t *testing.T) {
	tracker := windowsession.NewLockTracker()
	console := &windowsession.Snapshot{SessionID: 1, Lock: windowsession.LockUnlocked}
	reconcileConsoleLock(tracker, console)
	if console.Lock != windowsession.LockUnlocked || tracker.State(1) != windowsession.LockUnlocked {
		t.Fatalf("console=%q tracker=%q", console.Lock, tracker.State(1))
	}
}

func TestReconcileConsoleLockPreservesEventConfirmedState(t *testing.T) {
	tracker := windowsession.NewLockTracker()
	tracker.Apply(windowsession.Event{Kind: windowsession.EventLock, SessionID: 1})
	console := &windowsession.Snapshot{SessionID: 1, Lock: windowsession.LockUnlocked}
	reconcileConsoleLock(tracker, console)
	if console.Lock != windowsession.LockLocked || tracker.State(1) != windowsession.LockLocked {
		t.Fatalf("console=%q tracker=%q", console.Lock, tracker.State(1))
	}
}

func TestReconcileConsoleLockPreservesUnknown(t *testing.T) {
	tracker := windowsession.NewLockTracker()
	console := &windowsession.Snapshot{SessionID: 1, Lock: windowsession.LockUnknown}
	reconcileConsoleLock(tracker, console)
	if console.Lock != windowsession.LockUnknown || tracker.State(1) != windowsession.LockUnknown {
		t.Fatalf("console=%q tracker=%q", console.Lock, tracker.State(1))
	}
}
