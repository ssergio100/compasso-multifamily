//go:build windows

// Package windowsipc exposes the privileged agent operations to the Compasso
// interface over a Windows named pipe. It is the transport counterpart of the
// Linux system D-Bus API in agent/localapi: the bonus engine, the password
// verification and the wording all stay in the shared packages, and only the
// transport and its access control live here.
package windowsipc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
	"unsafe"

	"github.com/ssergio100/compasso/agent/localauth"
	"github.com/ssergio100/compasso/agent/localmsg"
	"golang.org/x/sys/windows"
)

// maxPayloadBytes bounds one framed message. The pipe only carries small
// control messages, so an oversized prefix is a protocol violation rather than
// an allocation request.
const maxPayloadBytes = 64 * 1024

// fileFlagFirstPipeInstance fails creation when the pipe name is already taken.
const fileFlagFirstPipeInstance = 0x00080000

// pipeName is a variable so tests can bind a private name instead of competing
// with a running agent service for PipeName.
var pipeName = PipeName

// bonusRequestTimeout mirrors the D-Bus call timeout used by the Linux agent.
const bonusRequestTimeout = 10 * time.Second

// BonusGranter is the bonus engine contract. localauth.Service satisfies it, and
// tests substitute their own implementation.
type BonusGranter interface {
	Grant(ctx context.Context, password string, seconds int64, now time.Time) (localauth.GrantResult, error)
}

// SynchronizationSource exposes the live heartbeat state.
type SynchronizationSource interface {
	SynchronizationReport() (checked, online bool, detail string)
}

type PublicSettingsSource interface {
	PublicConfiguration() (PublicSettings, bool)
	UpdatePublicConfiguration(request UpdatePublicConfigurationRequest) error
}

// UpdatePublicConfigurationRequest is the safe subset of fields that can be
// written by the interface.
type UpdatePublicConfigurationRequest struct {
	ServerURL           string
	DeviceID            string
	ControlledUserSID   string
	DeviceToken         string
	SetTokenWhenPresent bool
}

// Service serves the privileged agent operations over the named pipe. It makes
// no policy decisions: it adapts transport concerns to the shared bonus engine
// and to the live synchronization state.
type Service struct {
	bonus           BonusGranter
	synchronization SynchronizationSource
	settings        PublicSettingsSource
	now             func() time.Time
}

// NewService builds the named pipe service.
func NewService(bonus BonusGranter, synchronization SynchronizationSource, settings PublicSettingsSource) *Service {
	return &Service{bonus: bonus, synchronization: synchronization, settings: settings, now: time.Now}
}

// Listener owns the named pipe instance. Each accepted client gets a fresh
// instance, so the handle used by one client is never reused by the next.
type Listener struct {
	controlledUserSID string
	handle            windows.Handle
}

// Listen creates a named pipe listener bound to the controlled user.
func Listen(controlledUserSID string) (*Listener, error) {
	if !isValidSID(controlledUserSID) {
		return nil, fmt.Errorf("invalid controlled user SID: %q", controlledUserSID)
	}
	dacl, err := buildDACL(controlledUserSID)
	if err != nil {
		return nil, fmt.Errorf("build pipe DACL: %w", err)
	}
	security := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: dacl,
	}
	nameUTF16, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return nil, fmt.Errorf("convert pipe name: %w", err)
	}
	handle, err := windows.CreateNamedPipe(
		nameUTF16,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|fileFlagFirstPipeInstance,
		windows.PIPE_TYPE_MESSAGE|windows.PIPE_READMODE_MESSAGE|windows.PIPE_WAIT,
		windows.PIPE_UNLIMITED_INSTANCES,
		maxPayloadBytes,
		maxPayloadBytes,
		0,
		security,
	)
	if err != nil {
		return nil, fmt.Errorf("create named pipe: %w", err)
	}
	return &Listener{controlledUserSID: controlledUserSID, handle: handle}, nil
}
