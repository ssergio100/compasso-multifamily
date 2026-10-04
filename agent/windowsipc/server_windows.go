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
	"sync"
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
	security          *windows.SECURITY_DESCRIPTOR
	mu                sync.Mutex
	handle            windows.Handle
	closed            bool
}

// Listen creates a named pipe listener bound to the controlled user.
func Listen(controlledUserSID string) (*Listener, error) {
	sid, err := windows.StringToSid(controlledUserSID)
	if err != nil || !sid.IsValid() {
		return nil, fmt.Errorf("invalid controlled user SID: %q", controlledUserSID)
	}
	security, err := windows.SecurityDescriptorFromString(
		"D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;" + sid.String() + ")",
	)
	if err != nil {
		return nil, fmt.Errorf("build pipe DACL: %w", err)
	}
	listener := &Listener{controlledUserSID: sid.String(), security: security}
	handle, err := listener.createPipe(true)
	if err != nil {
		return nil, err
	}
	listener.handle = handle
	return listener, nil
}

func (l *Listener) createPipe(first bool) (windows.Handle, error) {
	security := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: l.security,
	}
	nameUTF16, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return 0, fmt.Errorf("convert pipe name: %w", err)
	}
	flags := uint32(windows.PIPE_ACCESS_DUPLEX | windows.FILE_FLAG_OVERLAPPED)
	if first {
		flags |= fileFlagFirstPipeInstance
	}
	handle, err := windows.CreateNamedPipe(
		nameUTF16,
		flags,
		windows.PIPE_TYPE_MESSAGE|windows.PIPE_READMODE_MESSAGE|windows.PIPE_WAIT,
		windows.PIPE_UNLIMITED_INSTANCES,
		maxPayloadBytes,
		maxPayloadBytes,
		0,
		security,
	)
	if err != nil {
		return 0, fmt.Errorf("create named pipe: %w", err)
	}
	return handle, nil
}

// Accept waits for one local client and transfers ownership of the connected
// handle to the returned connection. The listener never closes that handle a
// second time.
func (l *Listener) Accept(ctx context.Context) (*Conn, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, io.ErrClosedPipe
	}
	if l.handle == 0 {
		handle, err := l.createPipe(false)
		if err != nil {
			l.mu.Unlock()
			return nil, err
		}
		l.handle = handle
	}
	handle := l.handle
	l.mu.Unlock()

	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("create pipe event: %w", err)
	}
	defer windows.CloseHandle(event)
	overlapped := &windows.Overlapped{HEvent: event}
	err = windows.ConnectNamedPipe(handle, overlapped)
	switch {
	case err == nil, errors.Is(err, windows.ERROR_PIPE_CONNECTED):
	case errors.Is(err, windows.ERROR_IO_PENDING):
		if _, err = waitOverlapped(ctx, handle, event, overlapped); err != nil {
			return nil, fmt.Errorf("accept pipe client: %w", err)
		}
	default:
		return nil, fmt.Errorf("accept pipe client: %w", err)
	}

	l.mu.Lock()
	if l.handle != handle || l.closed {
		l.mu.Unlock()
		_ = windows.CloseHandle(handle)
		return nil, io.ErrClosedPipe
	}
	l.handle = 0
	l.mu.Unlock()
	return &Conn{handle: handle, ctx: ctx}, nil
}

// Close cancels a pending accept and releases only the handle still owned by
// the listener.
func (l *Listener) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	handle := l.handle
	l.handle = 0
	l.mu.Unlock()
	if handle == 0 {
		return nil
	}
	_ = windows.CancelIoEx(handle, nil)
	return windows.CloseHandle(handle)
}

// Conn is one request/response exchange on the local named pipe.
type Conn struct {
	mu     sync.Mutex
	handle windows.Handle
	ctx    context.Context
}

func (c *Conn) Read(buffer []byte) (int, error) {
	return c.transfer(buffer, false)
}

func (c *Conn) Write(buffer []byte) (int, error) {
	return c.transfer(buffer, true)
}

func (c *Conn) transfer(buffer []byte, write bool) (int, error) {
	c.mu.Lock()
	handle := c.handle
	c.mu.Unlock()
	if handle == 0 {
		return 0, io.ErrClosedPipe
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(event)
	overlapped := &windows.Overlapped{HEvent: event}
	var transferred uint32
	if write {
		err = windows.WriteFile(handle, buffer, &transferred, overlapped)
	} else {
		err = windows.ReadFile(handle, buffer, &transferred, overlapped)
	}
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		transferred, err = waitOverlapped(c.ctx, handle, event, overlapped)
	}
	if err != nil {
		if errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_NO_DATA) {
			return int(transferred), io.EOF
		}
		return int(transferred), err
	}
	if transferred == 0 && !write {
		return 0, io.EOF
	}
	return int(transferred), nil
}

func (c *Conn) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	handle := c.handle
	c.handle = 0
	c.mu.Unlock()
	if handle == 0 {
		return nil
	}
	_ = windows.CancelIoEx(handle, nil)
	_ = windows.FlushFileBuffers(handle)
	_ = windows.DisconnectNamedPipe(handle)
	return windows.CloseHandle(handle)
}

func waitOverlapped(ctx context.Context, handle, event windows.Handle, overlapped *windows.Overlapped) (uint32, error) {
	for {
		result, err := windows.WaitForSingleObject(event, 100)
		if err != nil {
			return 0, err
		}
		if result == uint32(windows.WAIT_OBJECT_0) {
			var transferred uint32
			if err := windows.GetOverlappedResult(handle, overlapped, &transferred, false); err != nil {
				return transferred, err
			}
			return transferred, nil
		}
		if result != uint32(windows.WAIT_TIMEOUT) {
			return 0, fmt.Errorf("unexpected wait result %d", result)
		}
		if ctx.Err() != nil {
			_ = windows.CancelIoEx(handle, overlapped)
			var transferred uint32
			_ = windows.GetOverlappedResult(handle, overlapped, &transferred, true)
			return transferred, ctx.Err()
		}
	}
}

// Serve keeps accepting clients after malformed or disconnected requests. A
// broken UI connection must never silently disable the local interface.
func (s *Service) Serve(ctx context.Context, listener *Listener) error {
	defer listener.Close()
	for {
		connection, err := listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, io.ErrClosedPipe) {
				return nil
			}
			return err
		}
		request, readErr := readFrame(connection)
		if readErr == nil {
			_ = writeFrame(connection, s.Handle(request))
		}
		_ = connection.Close()
	}
}

// Handle maps the wire request to the same local service used by Linux.
func (s *Service) Handle(request Request) Response {
	switch request.Operation {
	case OperationPing:
		return Response{OK: true, Message: "pong"}
	case OperationSynchronization:
		checked, online, detail := s.synchronization.SynchronizationReport()
		status, message := localmsg.Report(synchronizationState(checked, online), detail)
		return Response{OK: true, Status: status, Detail: message}
	case OperationAddLocalBonus:
		ctx, cancel := context.WithTimeout(context.Background(), bonusRequestTimeout)
		defer cancel()
		result, err := s.bonus.Grant(ctx, request.Password, request.Seconds, s.now())
		if err != nil {
			code := bonusErrorCode(err)
			return Response{OK: false, ErrorCode: code, Message: localmsg.BonusError(code)}
		}
		return Response{
			OK: true, EventUUID: result.UUID,
			Grant: &GrantSummary{
				UUID: result.UUID, BonusSeconds: result.BonusSeconds, TotalSeconds: result.TotalSeconds,
			},
		}
	case OperationPublicConfiguration:
		settings := PublicSettings{}
		if s.settings != nil {
			if loaded, ok := s.settings.PublicConfiguration(); ok {
				settings = loaded
			}
		}
		return Response{OK: true, Settings: &settings}
	default:
		return Response{OK: false, ErrorCode: localmsg.ErrorInvalidRequest, Message: localmsg.BonusError(localmsg.ErrorInvalidRequest)}
	}
}

func synchronizationState(checked, online bool) string {
	switch {
	case !checked:
		return localmsg.StatusChecking
	case online:
		return localmsg.StatusOnline
	default:
		return localmsg.StatusOffline
	}
}

func bonusErrorCode(err error) string {
	switch {
	case errors.Is(err, localauth.ErrPasswordNotConfigured):
		return localmsg.ErrorPasswordNotConfigured
	case errors.Is(err, localauth.ErrInvalidPassword):
		return localmsg.ErrorInvalidPassword
	case errors.Is(err, localauth.ErrRateLimited):
		return localmsg.ErrorRateLimited
	default:
		return localmsg.ErrorFailed
	}
}

func readFrame(reader io.Reader) (Request, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(reader, prefix[:]); err != nil {
		return Request{}, err
	}
	length := binary.LittleEndian.Uint32(prefix[:])
	if length == 0 || length > maxPayloadBytes {
		return Request{}, fmt.Errorf("request payload length %d is invalid", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return Request{}, err
	}
	var request Request
	if err := json.Unmarshal(payload, &request); err != nil {
		return Request{}, fmt.Errorf("decode request: %w", err)
	}
	return request, nil
}

func writeFrame(writer io.Writer, value interface{}) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > maxPayloadBytes {
		return fmt.Errorf("response payload length %d is invalid", len(payload))
	}
	var prefix [4]byte
	binary.LittleEndian.PutUint32(prefix[:], uint32(len(payload)))
	if _, err := writer.Write(prefix[:]); err != nil {
		return err
	}
	_, err = writer.Write(payload)
	return err
}
