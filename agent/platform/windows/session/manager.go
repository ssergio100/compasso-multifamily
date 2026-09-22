package session

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	agentsession "github.com/ssergio100/compasso/agent/session"
)

// ConsoleObserver is the subset of the WTS observer used by the daemon
// adapter. It remains small so session semantics can be tested on Linux.
type ConsoleObserver interface {
	ActiveConsole(context.Context) (*Snapshot, error)
}

// LockRequester sends an enforcement request to the authenticated companion
// in the interactive session. Acceptance is not lock confirmation.
type LockRequester interface {
	RequestLock(context.Context, uint32) error
}

// Manager adapts the physical Windows console to the platform-neutral session
// contract used by agent/daemon. Only the configured SID can become active.
type Manager struct {
	observer      ConsoleObserver
	locks         *LockTracker
	locker        LockRequester
	controlledSID string
	namespace     string
}

func NewManager(observer ConsoleObserver, locks *LockTracker, locker LockRequester, controlledSID, namespace string) (*Manager, error) {
	if observer == nil || locks == nil || locker == nil {
		return nil, errors.New("Windows observer, lock tracker and requester are required")
	}
	if controlledSID == "" {
		return nil, errors.New("controlled Windows SID is required")
	}
	if !validAuthorizationPart(namespace) {
		return nil, errors.New("Windows session namespace is invalid")
	}
	return &Manager{
		observer: observer, locks: locks, locker: locker,
		controlledSID: controlledSID, namespace: namespace,
	}, nil
}

func (m *Manager) Sessions(ctx context.Context, controlledUser string) ([]agentsession.Session, error) {
	if !strings.EqualFold(controlledUser, m.controlledSID) {
		return nil, fmt.Errorf("controlled Windows SID %q does not match manager SID", controlledUser)
	}
	console, err := m.observer.ActiveConsole(ctx)
	if err != nil {
		return nil, err
	}
	if console == nil || !strings.EqualFold(console.AccountSID, m.controlledSID) || !console.Console {
		return nil, nil
	}
	m.locks.Seed(console.SessionID, console.Lock)
	state := "online"
	if console.Connection == ConnectionActive {
		state = "active"
	} else if console.Connection == ConnectionDisconnected {
		state = "closing"
	}
	id := strconv.FormatUint(uint64(console.SessionID), 10)
	return []agentsession.Session{{
		ID: id, AuthorizationID: m.namespace + "_wts_" + id,
		User: console.AccountSID, Type: "windows-console", Class: "user",
		State: state, Remote: false, PauseAccountingWhileLocked: true,
	}}, nil
}

func (m *Manager) Lock(ctx context.Context, current agentsession.Session) error {
	sessionID, err := m.validateCurrent(ctx, current)
	if err != nil {
		return err
	}
	return m.locker.RequestLock(ctx, sessionID)
}

// Unlock is intentionally a validated no-op. Windows has no supported API to
// unlock the secure desktop; after policy permits access, the user authenticates
// with password, PIN or Windows Hello and the SCM confirms WTS_SESSION_UNLOCK.
func (m *Manager) Unlock(ctx context.Context, current agentsession.Session) error {
	_, err := m.validateCurrent(ctx, current)
	return err
}

func (m *Manager) IsLocked(ctx context.Context, current agentsession.Session) (bool, error) {
	sessionID, err := m.validateCurrent(ctx, current)
	if err != nil {
		return false, err
	}
	state := m.locks.State(sessionID)
	if state == LockUnknown {
		return false, errors.New("Windows console lock state is unknown")
	}
	return state == LockLocked, nil
}

func (m *Manager) validateCurrent(ctx context.Context, current agentsession.Session) (uint32, error) {
	parsed, err := strconv.ParseUint(current.ID, 10, 32)
	if err != nil {
		return 0, errors.New("Windows session ID is invalid")
	}
	console, err := m.observer.ActiveConsole(ctx)
	if err != nil {
		return 0, err
	}
	if console == nil || console.SessionID != uint32(parsed) ||
		!console.IsControlledActiveConsole(m.controlledSID) {
		return 0, errors.New("Windows session is no longer the controlled active console")
	}
	m.locks.Seed(console.SessionID, console.Lock)
	return console.SessionID, nil
}

func validAuthorizationPart(value string) bool {
	if value == "" || len(value) > 96 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
