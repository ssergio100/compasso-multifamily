package session

const (
	windowsWTSActive       = uint32(0)
	windowsWTSDisconnected = uint32(4)
	windowsConsoleProtocol = uint16(0)
	windowsSessionLocked   = int32(0)
	windowsSessionUnlocked = int32(1)
)

// classifyWindowsSession keeps only signed-in console sessions. Unknown lock
// flags fail closed so a session is never charged unless it is known unlocked.
func classifyWindowsSession(connectState uint32, protocol uint16, sessionFlag int32) (eligible, locked bool) {
	if protocol != windowsConsoleProtocol {
		return false, false
	}
	switch connectState {
	case windowsWTSActive:
		switch sessionFlag {
		case windowsSessionUnlocked:
			return true, false
		case windowsSessionLocked:
			return true, true
		default:
			return true, true
		}
	case windowsWTSDisconnected:
		return true, true
	default:
		return false, false
	}
}
