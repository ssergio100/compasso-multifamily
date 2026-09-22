package session

import (
	"sync"
	"time"
)

// EventKind is a normalized Windows session-change notification.
type EventKind string

const (
	EventInitial           EventKind = "initial"
	EventConsoleConnect    EventKind = "console_connect"
	EventConsoleDisconnect EventKind = "console_disconnect"
	EventRemoteConnect     EventKind = "remote_connect"
	EventRemoteDisconnect  EventKind = "remote_disconnect"
	EventLogon             EventKind = "logon"
	EventLogoff            EventKind = "logoff"
	EventLock              EventKind = "lock"
	EventUnlock            EventKind = "unlock"
	EventRemoteControl     EventKind = "remote_control"
	EventCreate            EventKind = "create"
	EventTerminate         EventKind = "terminate"
)

// Event describes a session-change notification delivered by the Windows SCM.
type Event struct {
	Kind       EventKind `json:"kind"`
	SessionID  uint32    `json:"session_id"`
	ObservedAt time.Time `json:"observed_at"`
}

// DecodeEvent normalizes the event type passed with
// SERVICE_CONTROL_SESSIONCHANGE. Numeric values are part of the Win32 API and
// intentionally live here so the state transition tests also run on Linux.
func DecodeEvent(eventType, sessionID uint32, observedAt time.Time) (Event, bool) {
	kind, found := map[uint32]EventKind{
		1:  EventConsoleConnect,
		2:  EventConsoleDisconnect,
		3:  EventRemoteConnect,
		4:  EventRemoteDisconnect,
		5:  EventLogon,
		6:  EventLogoff,
		7:  EventLock,
		8:  EventUnlock,
		9:  EventRemoteControl,
		10: EventCreate,
		11: EventTerminate,
	}[eventType]
	return Event{Kind: kind, SessionID: sessionID, ObservedAt: observedAt}, found
}

// LockTracker retains the last lock state confirmed by SCM events for each
// Windows logon session. WTS connection state alone cannot provide this fact.
type LockTracker struct {
	mu     sync.RWMutex
	states map[uint32]LockState
}

func NewLockTracker() *LockTracker {
	return &LockTracker{states: make(map[uint32]LockState)}
}

// Apply records one event and returns the resulting state for its session.
func (t *LockTracker) Apply(event Event) LockState {
	if t == nil {
		return LockUnknown
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch event.Kind {
	case EventLock:
		t.states[event.SessionID] = LockLocked
	case EventUnlock:
		t.states[event.SessionID] = LockUnlocked
	case EventLogon, EventCreate:
		t.states[event.SessionID] = LockUnknown
	case EventLogoff, EventTerminate:
		delete(t.states, event.SessionID)
	}
	state, found := t.states[event.SessionID]
	if !found {
		return LockUnknown
	}
	return state
}

func (t *LockTracker) State(sessionID uint32) LockState {
	if t == nil {
		return LockUnknown
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	state, found := t.states[sessionID]
	if !found {
		return LockUnknown
	}
	return state
}

// Seed records an initial observation only while no lock/unlock event has
// established a state for the session. An unknown observation is never stored,
// and a later observation cannot overwrite a state confirmed by the SCM.
func (t *LockTracker) Seed(sessionID uint32, observed LockState) LockState {
	if t == nil || observed == LockUnknown {
		return LockUnknown
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if state, found := t.states[sessionID]; found && state != LockUnknown {
		return state
	}
	t.states[sessionID] = observed
	return observed
}
