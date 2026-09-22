package session

import (
	"testing"
	"time"
)

func TestDecodeEventAndTrackLockState(t *testing.T) {
	at := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	tracker := NewLockTracker()

	locked, found := DecodeEvent(7, 42, at)
	if !found || locked.Kind != EventLock || locked.SessionID != 42 || !locked.ObservedAt.Equal(at) {
		t.Fatalf("decoded lock event = %+v, found=%v", locked, found)
	}
	if got := tracker.Apply(locked); got != LockLocked {
		t.Fatalf("state after lock = %q", got)
	}

	unlocked, found := DecodeEvent(8, 42, at.Add(time.Second))
	if !found || tracker.Apply(unlocked) != LockUnlocked {
		t.Fatalf("state after unlock = %q, found=%v", tracker.State(42), found)
	}

	logoff, found := DecodeEvent(6, 42, at.Add(2*time.Second))
	if !found || tracker.Apply(logoff) != LockUnknown {
		t.Fatalf("state after logoff = %q, found=%v", tracker.State(42), found)
	}
	if _, found := DecodeEvent(99, 42, at); found {
		t.Fatal("unknown WTS event was accepted")
	}
}

func TestLockStateIsIndependentPerSession(t *testing.T) {
	tracker := NewLockTracker()
	tracker.Apply(Event{Kind: EventLock, SessionID: 1})
	tracker.Apply(Event{Kind: EventUnlock, SessionID: 2})
	if tracker.State(1) != LockLocked || tracker.State(2) != LockUnlocked {
		t.Fatalf("unexpected states: session 1=%q, session 2=%q", tracker.State(1), tracker.State(2))
	}
}

func TestLockTrackerSeedsOnlyUnknownSession(t *testing.T) {
	tracker := NewLockTracker()
	if got := tracker.Seed(1, LockUnknown); got != LockUnknown || tracker.State(1) != LockUnknown {
		t.Fatalf("unknown seed changed state to %q", tracker.State(1))
	}
	if got := tracker.Seed(1, LockUnlocked); got != LockUnlocked || tracker.State(1) != LockUnlocked {
		t.Fatalf("unlocked seed produced %q", tracker.State(1))
	}
	tracker.Apply(Event{Kind: EventLock, SessionID: 1})
	if got := tracker.Seed(1, LockUnlocked); got != LockLocked || tracker.State(1) != LockLocked {
		t.Fatalf("seed overwrote event-confirmed state with %q", tracker.State(1))
	}
}
