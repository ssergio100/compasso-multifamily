//go:build windows
// +build windows

package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"syscall"
	"unsafe"

	"github.com/ssergio100/compasso/agent/platform/windows/ipc"
	"golang.org/x/sys/windows"
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	lockWorkStation = user32.NewProc("LockWorkStation")
	messageBox      = user32.NewProc("MessageBoxW")
)

const messageBoxFlags = 0x00000000 | 0x00000040 | 0x00010000 | 0x00040000

// Run stays attached to the privileged service and handles commands in the
// interactive user session until the service closes the pipe.
func Run(ctx context.Context, pipeName string) error {
	pipe, err := ipc.Dial(ctx, pipeName)
	if err != nil {
		return err
	}
	defer pipe.Close()

	decoder := json.NewDecoder(pipe)
	encoder := json.NewEncoder(pipe)
	for {
		var request ipc.Request
		if err := decoder.Decode(&request); errors.Is(err, io.EOF) || errors.Is(err, windows.ERROR_BROKEN_PIPE) {
			return nil
		} else if err != nil {
			return fmt.Errorf("read service command: %w", err)
		}
		response := ipc.Response{Version: ipc.ProtocolVersion, ID: request.ID}
		stopAfterResponse := false
		if err := request.Validate(); err != nil {
			response.Error = err.Error()
		} else if request.Command == ipc.CommandStop {
			response.Accepted = true
			stopAfterResponse = true
		} else if request.Command == ipc.CommandNotify {
			response.Accepted = true
			go showNotification(request.Title, request.Body)
		} else {
			response.Accepted, err = execute(request.Command)
			if err != nil {
				response.Error = err.Error()
			}
		}
		if err := encoder.Encode(response); err != nil {
			return fmt.Errorf("send companion response: %w", err)
		}
		if stopAfterResponse {
			return nil
		}
	}
}

func showNotification(title, body string) {
	titlePointer, titleErr := windows.UTF16PtrFromString(title)
	bodyPointer, bodyErr := windows.UTF16PtrFromString(body)
	if titleErr != nil || bodyErr != nil {
		return
	}
	messageBox.Call(0, uintptr(unsafe.Pointer(bodyPointer)), uintptr(unsafe.Pointer(titlePointer)), messageBoxFlags)
}

func execute(command ipc.CommandKind) (bool, error) {
	if command != ipc.CommandLock {
		return false, errors.New("unsupported companion command")
	}
	result, _, callErr := lockWorkStation.Call()
	if result == 0 {
		if callErr == nil || errors.Is(callErr, syscall.Errno(0)) {
			callErr = errors.New("LockWorkStation rejected the request")
		}
		return false, callErr
	}
	return true, nil
}
