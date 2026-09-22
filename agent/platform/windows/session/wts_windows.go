//go:build windows
// +build windows

package session

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wtsUserName           = 5
	wtsDomainName         = 7
	wtsConnectState       = 8
	wtsClientProtocolType = 16
	wtsSessionInfoEx      = 25
	noConsoleSession      = ^uint32(0)
)

var (
	wtsAPI                         = windows.NewLazySystemDLL("wtsapi32.dll")
	wtsQuerySessionInformationProc = wtsAPI.NewProc("WTSQuerySessionInformationW")
)

// Observer inspects the Windows physical console without making enforcement
// decisions. WTSInfoEx supplies an initial lock observation; the service keeps
// the state current from subsequent WTS session-change events.
type Observer struct{}

// wtsInfoExPrefix is the fixed prefix of WTSINFOEXW followed by
// WTSINFOEX_LEVEL1_W on 64-bit Windows. Data begins at an eight-byte boundary
// because the level-one structure contains LARGE_INTEGER members.
type wtsInfoExPrefix struct {
	Level        uint32
	_            uint32
	SessionID    uint32
	SessionState uint32
	SessionFlags int32
}

func NewObserver() *Observer {
	return &Observer{}
}

// SessionLock queries the extended WTS state for one session. Callers must
// preserve unknown when the API fails or returns an undocumented value.
func (o *Observer) SessionLock(ctx context.Context, sessionID uint32) (LockObservation, error) {
	if o == nil {
		return LockObservation{State: LockUnknown}, errors.New("WTS observer is required")
	}
	if err := ctx.Err(); err != nil {
		return LockObservation{State: LockUnknown}, err
	}
	buffer, bytesReturned, err := queryWTSInformation(sessionID, wtsSessionInfoEx)
	if err != nil {
		return LockObservation{State: LockUnknown}, fmt.Errorf("query WTS session %d extended state: %w", sessionID, err)
	}
	if buffer == 0 || bytesReturned < uint32(unsafe.Sizeof(wtsInfoExPrefix{})) {
		if buffer != 0 {
			windows.WTSFreeMemory(buffer)
		}
		return LockObservation{State: LockUnknown}, errors.New("WTS extended session response is empty")
	}
	defer windows.WTSFreeMemory(buffer)
	information := (*wtsInfoExPrefix)(unsafe.Pointer(buffer))
	if information.Level != 1 {
		return LockObservation{State: LockUnknown}, fmt.Errorf("unsupported WTS extended session level %d", information.Level)
	}
	if information.SessionID != sessionID {
		return LockObservation{State: LockUnknown}, fmt.Errorf(
			"WTS extended session ID %d does not match requested session %d",
			information.SessionID, sessionID,
		)
	}
	raw := uint32(information.SessionFlags)
	return LockObservation{State: normalizeLockState(raw), SessionFlags: raw}, nil
}

// ActiveConsole returns the physical console session, or nil when no session is
// currently attached. RDP and disconnected background sessions are outside the
// initial Windows product scope and are not enumerated here.
func (o *Observer) ActiveConsole(ctx context.Context) (*Snapshot, error) {
	if o == nil {
		return nil, errors.New("WTS observer is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sessionID := windows.WTSGetActiveConsoleSessionId()
	if sessionID == noConsoleSession {
		return nil, nil
	}
	username, err := queryWTSString(sessionID, wtsUserName)
	if err != nil {
		return nil, fmt.Errorf("query WTS console session %d username: %w", sessionID, err)
	}
	if username == "" {
		return nil, nil
	}
	domain, err := queryWTSString(sessionID, wtsDomainName)
	if err != nil {
		return nil, fmt.Errorf("query WTS console session %d domain: %w", sessionID, err)
	}
	connection, err := queryWTSUint32(sessionID, wtsConnectState)
	if err != nil {
		return nil, fmt.Errorf("query WTS console session %d state: %w", sessionID, err)
	}
	protocol, err := queryWTSUint16(sessionID, wtsClientProtocolType)
	if err != nil {
		return nil, fmt.Errorf("query WTS console session %d protocol: %w", sessionID, err)
	}
	account := username
	if domain != "" {
		account = domain + `\` + username
	}
	sid, _, _, err := windows.LookupSID("", account)
	if err != nil {
		return nil, fmt.Errorf("resolve WTS console session %d account %q: %w", sessionID, account, err)
	}
	lock := LockUnknown
	if observation, lockErr := o.SessionLock(ctx, sessionID); lockErr == nil {
		lock = observation.State
	}
	return &Snapshot{
		SessionID: sessionID, AccountSID: sid.String(), AccountName: username,
		Domain: domain, Console: true, Protocol: protocolName(protocol),
		Connection: normalizeConnectionState(connection), Lock: lock,
	}, nil
}

func queryWTSString(sessionID uint32, informationClass uintptr) (string, error) {
	buffer, _, err := queryWTSInformation(sessionID, informationClass)
	if err != nil {
		return "", err
	}
	if buffer == 0 {
		return "", nil
	}
	defer windows.WTSFreeMemory(buffer)
	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(buffer))), nil
}

func queryWTSUint16(sessionID uint32, informationClass uintptr) (uint16, error) {
	buffer, bytesReturned, err := queryWTSInformation(sessionID, informationClass)
	if err != nil {
		return 0, err
	}
	if buffer == 0 || bytesReturned < uint32(unsafe.Sizeof(uint16(0))) {
		if buffer != 0 {
			windows.WTSFreeMemory(buffer)
		}
		return 0, errors.New("WTS uint16 response is empty")
	}
	defer windows.WTSFreeMemory(buffer)
	return *(*uint16)(unsafe.Pointer(buffer)), nil
}

func queryWTSUint32(sessionID uint32, informationClass uintptr) (uint32, error) {
	buffer, bytesReturned, err := queryWTSInformation(sessionID, informationClass)
	if err != nil {
		return 0, err
	}
	if buffer == 0 || bytesReturned < uint32(unsafe.Sizeof(uint32(0))) {
		if buffer != 0 {
			windows.WTSFreeMemory(buffer)
		}
		return 0, errors.New("WTS uint32 response is empty")
	}
	defer windows.WTSFreeMemory(buffer)
	return *(*uint32)(unsafe.Pointer(buffer)), nil
}

func queryWTSInformation(sessionID uint32, informationClass uintptr) (uintptr, uint32, error) {
	var buffer uintptr
	var bytesReturned uint32
	result, _, callErr := wtsQuerySessionInformationProc.Call(
		0, uintptr(sessionID), informationClass,
		uintptr(unsafe.Pointer(&buffer)), uintptr(unsafe.Pointer(&bytesReturned)),
	)
	runtime.KeepAlive(&buffer)
	runtime.KeepAlive(&bytesReturned)
	if result == 0 {
		if callErr == nil || errors.Is(callErr, syscall.Errno(0)) {
			callErr = syscall.EINVAL
		}
		return 0, 0, callErr
	}
	return buffer, bytesReturned, nil
}
