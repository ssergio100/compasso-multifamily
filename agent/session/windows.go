//go:build windows

package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wtsCurrentServerHandle = uintptr(0)
	wtsUserName            = uintptr(5)
	wtsDomainName          = uintptr(7)
	wtsClientProtocolType  = uintptr(16)
	wtsSessionInfoEx       = uintptr(25)
)

var (
	wtsAPI32                       = windows.NewLazySystemDLL("wtsapi32.dll")
	wtsQuerySessionInformationProc = wtsAPI32.NewProc("WTSQuerySessionInformationW")
	wtsDisconnectSessionProc       = wtsAPI32.NewProc("WTSDisconnectSession")
)

// Windows observes local console sessions through the Remote Desktop Services
// API. Despite the API name, protocol type zero denotes the physical console.
type Windows struct {
	namespaceID string
}

// NewWindows creates a session manager with a new runtime namespace. A service
// restart therefore requests a fresh balance anchor from the server.
func NewWindows() (*Windows, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return nil, fmt.Errorf("generate Windows session namespace: %w", err)
	}
	return &Windows{namespaceID: hex.EncodeToString(value)}, nil
}

// Sessions returns the signed-in local console session matching controlledSID.
// RDP and other remote sessions are deliberately ignored.
func (manager *Windows) Sessions(ctx context.Context, controlledSID string) ([]Session, error) {
	if manager == nil || manager.namespaceID == "" {
		return nil, errors.New("Windows session manager is not initialized")
	}
	wantedSID, err := windows.StringToSid(controlledSID)
	if err != nil {
		return nil, fmt.Errorf("parse controlled Windows SID: %w", err)
	}
	var first *windows.WTS_SESSION_INFO
	var count uint32
	if err := windows.WTSEnumerateSessions(0, 0, 1, &first, &count); err != nil {
		return nil, fmt.Errorf("enumerate Windows sessions: %w", err)
	}
	if first == nil || count == 0 {
		return nil, nil
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(first)))
	entries := unsafe.Slice(first, int(count))
	result := make([]Session, 0, 1)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.State != windowsWTSActive && entry.State != windowsWTSDisconnected {
			continue
		}
		protocol, err := queryWTSUint16(entry.SessionID, wtsClientProtocolType)
		if err != nil {
			return nil, err
		}
		if protocol != windowsConsoleProtocol {
			continue
		}
		username, err := queryWTSString(entry.SessionID, wtsUserName)
		if err != nil {
			return nil, err
		}
		if username == "" {
			continue
		}
		domain, err := queryWTSString(entry.SessionID, wtsDomainName)
		if err != nil {
			return nil, err
		}
		account := username
		if domain != "" {
			account = domain + `\` + username
		}
		sid, _, _, err := windows.LookupSID("", account)
		if err != nil {
			return nil, fmt.Errorf("resolve SID for Windows session %d: %w", entry.SessionID, err)
		}
		if !strings.EqualFold(sid.String(), wantedSID.String()) {
			continue
		}
		flag, err := queryWTSSessionFlag(entry.SessionID)
		if err != nil {
			// Failing closed prevents accidental charging. A disconnected session
			// is also always treated as locked by the classifier.
			flag = -1
		}
		eligible, locked := classifyWindowsSession(entry.State, protocol, flag)
		if !eligible {
			continue
		}
		id := fmt.Sprintf("%d", entry.SessionID)
		result = append(result, Session{
			ID: id, AuthorizationID: manager.namespaceID + "_" + id,
			User: account, Type: "windows", Class: "user", State: "active",
			Remote: false, Locked: locked,
		})
	}
	return result, nil
}

// Lock disconnects the console session without logging the user off, so open
// applications remain available after the next manual sign-in.
func (manager *Windows) Lock(ctx context.Context, current Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sessionID, err := parseWindowsSessionID(current.ID)
	if err != nil {
		return err
	}
	result, _, callErr := wtsDisconnectSessionProc.Call(wtsCurrentServerHandle, uintptr(sessionID), 1)
	if result == 0 {
		return fmt.Errorf("disconnect Windows session %d: %w", sessionID, callErr)
	}
	return nil
}

// Unlock is intentionally a no-op. Windows requires the authorized user to
// unlock manually; the daemon observes that transition in a later cycle.
func (manager *Windows) Unlock(context.Context, Session) error { return nil }

func (manager *Windows) IsLocked(_ context.Context, current Session) (bool, error) {
	return current.Locked, nil
}

func queryWTSString(sessionID uint32, informationClass uintptr) (string, error) {
	pointer, size, err := queryWTSInformation(sessionID, informationClass)
	if err != nil {
		return "", err
	}
	defer windows.WTSFreeMemory(pointer)
	if pointer == 0 || size < 2 {
		return "", nil
	}
	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(pointer))), nil
}

func queryWTSUint16(sessionID uint32, informationClass uintptr) (uint16, error) {
	pointer, size, err := queryWTSInformation(sessionID, informationClass)
	if err != nil {
		return 0, err
	}
	defer windows.WTSFreeMemory(pointer)
	if pointer == 0 || size < 2 {
		return 0, errors.New("Windows session information is incomplete")
	}
	return *(*uint16)(unsafe.Pointer(pointer)), nil
}

func queryWTSSessionFlag(sessionID uint32) (int32, error) {
	pointer, size, err := queryWTSInformation(sessionID, wtsSessionInfoEx)
	if err != nil {
		return -1, err
	}
	defer windows.WTSFreeMemory(pointer)
	// WTSINFOEXW begins with Level and four bytes of x64 union alignment;
	// WTSINFOEX_LEVEL1 then begins with SessionId, SessionState and SessionFlags.
	const sessionFlagOffset = uintptr(16)
	if pointer == 0 || uintptr(size) < sessionFlagOffset+4 {
		return -1, errors.New("extended Windows session information is incomplete")
	}
	level := *(*uint32)(unsafe.Pointer(pointer))
	if level != 1 {
		return -1, fmt.Errorf("unsupported Windows session information level %d", level)
	}
	return *(*int32)(unsafe.Pointer(pointer + sessionFlagOffset)), nil
}

func queryWTSInformation(sessionID uint32, informationClass uintptr) (uintptr, uint32, error) {
	var pointer uintptr
	var size uint32
	result, _, callErr := wtsQuerySessionInformationProc.Call(
		wtsCurrentServerHandle, uintptr(sessionID), informationClass,
		uintptr(unsafe.Pointer(&pointer)), uintptr(unsafe.Pointer(&size)),
	)
	if result == 0 {
		return 0, 0, fmt.Errorf("query Windows session %d information %d: %w", sessionID, informationClass, callErr)
	}
	return pointer, size, nil
}

func parseWindowsSessionID(value string) (uint32, error) {
	if value == "" {
		return 0, errors.New("Windows session ID is required")
	}
	var sessionID uint32
	if _, err := fmt.Sscanf(value, "%d", &sessionID); err != nil || fmt.Sprintf("%d", sessionID) != value {
		return 0, fmt.Errorf("invalid Windows session ID %q", value)
	}
	return sessionID, nil
}
