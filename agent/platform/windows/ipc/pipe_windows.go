//go:build windows
// +build windows

package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const pipeBufferSize = 4096

var (
	impersonateNamedPipeClient  = windows.NewLazySystemDLL("advapi32.dll").NewProc("ImpersonateNamedPipeClient")
	getNamedPipeClientProcessID = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetNamedPipeClientProcessId")
)

// Server owns one local-only, first-instance pipe while it waits for the
// companion process created by the service.
type Server struct {
	mu            sync.Mutex
	file          *os.File
	handle        windows.Handle
	controlledSID string
}

type Connection struct {
	exchangeMu sync.Mutex
	closeOnce  sync.Once
	file       *os.File
	handle     windows.Handle
	encoder    *json.Encoder
	decoder    *json.Decoder
}

func Listen(pipeName, controlledSID string) (*Server, error) {
	if err := validatePipeName(pipeName); err != nil {
		return nil, err
	}
	if _, err := windows.StringToSid(controlledSID); err != nil {
		return nil, fmt.Errorf("parse controlled SID: %w", err)
	}
	securityDescriptor, err := windows.SecurityDescriptorFromString(
		fmt.Sprintf("O:SYG:SYD:P(D;;GA;;;NU)(A;;GA;;;SY)(A;;GRGW;;;%s)", controlledSID),
	)
	if err != nil {
		return nil, fmt.Errorf("build companion pipe ACL: %w", err)
	}
	name, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return nil, fmt.Errorf("encode companion pipe name: %w", err)
	}
	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: securityDescriptor,
	}
	handle, err := windows.CreateNamedPipe(
		name,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1, pipeBufferSize, pipeBufferSize, 0, &attributes,
	)
	runtime.KeepAlive(securityDescriptor)
	if err != nil {
		return nil, fmt.Errorf("create companion pipe: %w", err)
	}
	file := os.NewFile(uintptr(handle), pipeName)
	if file == nil {
		windows.CloseHandle(handle)
		return nil, errors.New("wrap companion pipe handle")
	}
	return &Server{file: file, handle: handle, controlledSID: controlledSID}, nil
}

// Accept verifies both the interactive account SID and, when nonzero, the PID
// of the exact companion process created and retained by the service.
func (s *Server) Accept(expectedPID uint32) (*Connection, error) {
	if s == nil {
		return nil, errors.New("companion pipe server is required")
	}
	s.mu.Lock()
	file, handle := s.file, s.handle
	s.mu.Unlock()
	if file == nil || handle == windows.InvalidHandle {
		return nil, errors.New("companion pipe server is closed")
	}
	if err := windows.ConnectNamedPipe(handle, nil); err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		return nil, fmt.Errorf("accept companion pipe client: %w", err)
	}
	if err := verifyConnectedClient(handle, s.controlledSID, expectedPID); err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.file != file {
		s.mu.Unlock()
		return nil, errors.New("companion pipe server closed during accept")
	}
	s.file = nil
	s.handle = windows.InvalidHandle
	s.mu.Unlock()
	return &Connection{file: file, handle: handle, encoder: json.NewEncoder(file), decoder: json.NewDecoder(file)}, nil
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	file := s.file
	s.file = nil
	s.handle = windows.InvalidHandle
	s.mu.Unlock()
	if file == nil {
		return nil
	}
	return file.Close()
}

func (c *Connection) Exchange(request Request) (Response, error) {
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	if c == nil {
		return Response{}, errors.New("companion pipe connection is closed")
	}
	c.exchangeMu.Lock()
	defer c.exchangeMu.Unlock()
	if c.file == nil {
		return Response{}, errors.New("companion pipe connection is closed")
	}
	if err := c.encoder.Encode(request); err != nil {
		return Response{}, fmt.Errorf("send companion command: %w", err)
	}
	var response Response
	if err := c.decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("read companion response: %w", err)
	}
	if err := response.ValidateFor(request); err != nil {
		return Response{}, err
	}
	return response, nil
}

func (c *Connection) Close() error {
	if c == nil {
		return nil
	}
	c.exchangeMu.Lock()
	defer c.exchangeMu.Unlock()
	var err error
	c.closeOnce.Do(func() {
		if c.file != nil {
			_ = windows.DisconnectNamedPipe(c.handle)
			err = c.file.Close()
			c.file = nil
		}
	})
	return err
}

// ServeOne remains as a development probe. Production lifecycle code uses
// Listen and validates the PID returned by CreateProcessAsUser.
func ServeOne(pipeName, controlledSID string, request Request) (Response, error) {
	server, err := Listen(pipeName, controlledSID)
	if err != nil {
		return Response{}, err
	}
	defer server.Close()
	connection, err := server.Accept(0)
	if err != nil {
		return Response{}, err
	}
	defer connection.Close()
	return connection.Exchange(request)
}

// Dial connects only to a local pipe and retries while the service is creating
// its first instance.
func Dial(ctx context.Context, pipeName string) (*os.File, error) {
	if err := validatePipeName(pipeName); err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return nil, fmt.Errorf("encode companion pipe name: %w", err)
	}
	for {
		handle, openErr := windows.CreateFile(
			name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
			windows.OPEN_EXISTING, windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0,
		)
		if openErr == nil {
			file := os.NewFile(uintptr(handle), pipeName)
			if file == nil {
				windows.CloseHandle(handle)
				return nil, errors.New("wrap companion client pipe handle")
			}
			return file, nil
		}
		if !errors.Is(openErr, windows.ERROR_PIPE_BUSY) && !errors.Is(openErr, windows.ERROR_FILE_NOT_FOUND) {
			return nil, fmt.Errorf("connect companion pipe: %w", openErr)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func verifyConnectedClient(pipe windows.Handle, controlledSID string, expectedPID uint32) error {
	if expectedPID != 0 {
		var connectedPID uint32
		result, _, callErr := getNamedPipeClientProcessID.Call(uintptr(pipe), uintptr(unsafe.Pointer(&connectedPID)))
		if result == 0 {
			return fmt.Errorf("read companion client PID: %w", callErr)
		}
		if connectedPID != expectedPID {
			return fmt.Errorf("companion client PID %d is not expected PID %d", connectedPID, expectedPID)
		}
	}
	return verifyConnectedClientSID(pipe, controlledSID)
}

func verifyConnectedClientSID(pipe windows.Handle, controlledSID string) (returnErr error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	result, _, callErr := impersonateNamedPipeClient.Call(uintptr(pipe))
	if result == 0 {
		return fmt.Errorf("impersonate companion pipe client: %w", callErr)
	}
	defer func() {
		if err := windows.RevertToSelf(); err != nil && returnErr == nil {
			returnErr = fmt.Errorf("revert companion client impersonation: %w", err)
		}
	}()

	thread, err := windows.GetCurrentThread()
	if err != nil {
		return fmt.Errorf("get companion server thread: %w", err)
	}
	var token windows.Token
	if err := windows.OpenThreadToken(thread, windows.TOKEN_QUERY, true, &token); err != nil {
		return fmt.Errorf("open companion client token: %w", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("read companion client SID: %w", err)
	}
	if !strings.EqualFold(user.User.Sid.String(), controlledSID) {
		return fmt.Errorf("companion client SID %q is not controlled SID", user.User.Sid.String())
	}
	return nil
}

func validatePipeName(pipeName string) error {
	if !strings.HasPrefix(strings.ToLower(pipeName), localPipePrefix) || len(pipeName) == len(localPipePrefix) {
		return errors.New("companion pipe must use the local \\\\.\\pipe\\ namespace")
	}
	if strings.ContainsAny(pipeName[len(localPipePrefix):], `/\x00`) {
		return errors.New("companion pipe name is invalid")
	}
	return nil
}
