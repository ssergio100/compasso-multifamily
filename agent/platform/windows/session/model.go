// Package session models Windows interactive sessions independently from the
// Win32 calls used to discover them.
package session

import "strings"

// ConnectionState is the normalized WTS connection state needed by the agent.
type ConnectionState string

const (
	ConnectionUnknown      ConnectionState = "unknown"
	ConnectionActive       ConnectionState = "active"
	ConnectionConnected    ConnectionState = "connected"
	ConnectionDisconnected ConnectionState = "disconnected"
	ConnectionOther        ConnectionState = "other"
)

// LockState is deliberately independent from ConnectionState. WTS can report
// an interactive console as active while its secure desktop is locked.
type LockState string

const (
	LockUnknown  LockState = "unknown"
	LockLocked   LockState = "locked"
	LockUnlocked LockState = "unlocked"
)

// LockObservation preserves the raw WTSInfoEx flag next to its normalized
// value so diagnostics can distinguish an unknown flag from a query failure.
type LockObservation struct {
	State        LockState `json:"state"`
	SessionFlags uint32    `json:"session_flags"`
}

const (
	wtsSessionStateLock    uint32 = 0
	wtsSessionStateUnlock  uint32 = 1
	wtsSessionStateUnknown uint32 = ^uint32(0)
)

// Snapshot is one Windows logon session observed by the privileged service.
// AccountSID, rather than the localized account name, is its stable identity.
type Snapshot struct {
	SessionID   uint32          `json:"session_id"`
	AccountSID  string          `json:"account_sid"`
	AccountName string          `json:"account_name"`
	Domain      string          `json:"domain"`
	Console     bool            `json:"console"`
	Protocol    string          `json:"protocol"`
	Connection  ConnectionState `json:"connection"`
	Lock        LockState       `json:"lock"`
}

// IsControlledActiveConsole reports whether this snapshot is the physical
// desktop whose elapsed time should be accounted for. Lock does not enter this
// decision: the Linux product also keeps counting while its screen is locked.
func (s Snapshot) IsControlledActiveConsole(controlledSID string) bool {
	return controlledSID != "" && strings.EqualFold(s.AccountSID, controlledSID) &&
		s.Console && s.Connection == ConnectionActive
}

func normalizeConnectionState(raw uint32) ConnectionState {
	switch raw {
	case 0: // WTSActive
		return ConnectionActive
	case 1: // WTSConnected
		return ConnectionConnected
	case 4: // WTSDisconnected
		return ConnectionDisconnected
	case 2, 3, 5, 6, 7, 8, 9:
		return ConnectionOther
	default:
		return ConnectionUnknown
	}
}

func protocolName(raw uint16) string {
	switch raw {
	case 0:
		return "console"
	case 2:
		return "rdp"
	default:
		return "unknown"
	}
}

// normalizeLockState maps the SessionFlags member returned in
// WTSINFOEX_LEVEL1. Windows 7 and Server 2008 R2 returned the lock and unlock
// values reversed, but neither platform is supported by the Windows agent.
func normalizeLockState(raw uint32) LockState {
	switch raw {
	case wtsSessionStateLock:
		return LockLocked
	case wtsSessionStateUnlock:
		return LockUnlocked
	case wtsSessionStateUnknown:
		return LockUnknown
	default:
		return LockUnknown
	}
}
