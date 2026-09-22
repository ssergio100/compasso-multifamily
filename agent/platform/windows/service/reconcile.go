package service

import windowsession "github.com/ssergio100/compasso/agent/platform/windows/session"

// reconcileConsoleLock combines an initial WTSInfoEx observation with the
// event tracker. Once the SCM confirms a lock or unlock event, that event state
// remains authoritative over later point-in-time observations.
func reconcileConsoleLock(tracker *windowsession.LockTracker, console *windowsession.Snapshot) {
	if tracker == nil || console == nil {
		return
	}
	state := tracker.State(console.SessionID)
	if state == windowsession.LockUnknown {
		state = tracker.Seed(console.SessionID, console.Lock)
	}
	console.Lock = state
}
