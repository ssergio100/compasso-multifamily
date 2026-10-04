//go:build windows

package windowsservice

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	userAccountDisabled = 0x0002
	userNormalAccount   = 0x0200
)

type localUserInfo1 struct {
	Name        *uint16
	Password    *uint16
	PasswordAge uint32
	Privilege   uint32
	HomeDir     *uint16
	Comment     *uint16
	Flags       uint32
	ScriptPath  *uint16
}

// ValidateControlledAccount makes the elevated helper authoritative for the
// name-to-SID mapping. The UI-provided SID is never trusted by itself.
func ValidateControlledAccount(name, sidValue string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("controlled Windows account is required")
	}
	sid, err := windows.StringToSid(strings.TrimSpace(sidValue))
	if err != nil || !sid.IsValid() {
		return fmt.Errorf("controlled user SID is invalid")
	}
	account, domain, accountType, err := sid.LookupAccount("")
	if err != nil {
		return fmt.Errorf("resolve controlled Windows account: %w", err)
	}
	if accountType != windows.SidTypeUser || !strings.EqualFold(account, name) {
		return fmt.Errorf("controlled Windows account does not match its SID")
	}
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read Windows computer name: %w", err)
	}
	if !strings.EqualFold(domain, hostname) {
		return fmt.Errorf("controlled Windows account must be local")
	}
	encodedName, err := windows.UTF16PtrFromString(account)
	if err != nil {
		return fmt.Errorf("encode controlled Windows account: %w", err)
	}
	var buffer *byte
	if err := windows.NetUserGetInfo(nil, encodedName, 1, &buffer); err != nil {
		return fmt.Errorf("read controlled Windows account: %w", err)
	}
	defer windows.NetApiBufferFree(buffer)
	information := (*localUserInfo1)(unsafe.Pointer(buffer))
	if information.Flags&userAccountDisabled != 0 {
		return fmt.Errorf("controlled Windows account is disabled")
	}
	if information.Flags&userNormalAccount == 0 {
		return fmt.Errorf("controlled Windows account is not a normal local account")
	}
	return nil
}
