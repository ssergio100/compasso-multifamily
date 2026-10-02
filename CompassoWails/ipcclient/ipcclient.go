//go:build windows

package ipcclient

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/sys/windows"
)

// ErrServiceUnavailable means the agent service could not be reached at all.
var ErrServiceUnavailable = errors.New("serviço do Compasso indisponível")

// callTimeout bounds one request. The service answers from local state, so a
// slower reply means something is wrong and the interface should not hang.
const callTimeout = 15 * time.Second

// connectRetryWindow covers the brief interval between the service closing one
// pipe instance and creating the next one.
const connectRetryWindow = 3 * time.Second

// Call performs one request and returns the service reply. A reply that reports
// a business failure is returned as a Response with OK false, not as an error:
// only transport problems are errors.
func Call(ctx context.Context, request Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	handle, err := connect(ctx)
	if err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrServiceUnavailable, err)
	}
	defer windows.CloseHandle(handle)

	if err := writeFrame(ctx, handle, request); err != nil {
		return Response{}, err
	}
	return readFrame(ctx, handle)
}

// connect opens the pipe, retrying while the service rebuilds its instance.
func connect(ctx context.Context) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(PipeName)
	if err != nil {
		return 0, err
	}
	deadline := time.Now().Add(connectRetryWindow)
	var lastErr error
	for {
		handle, err := windows.CreateFile(name,
			windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
			windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
		if err == nil {
			return handle, nil
		}
		lastErr = err
		if time.Now().After(deadline) || ctx.Err() != nil {
			return 0, lastErr
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func writeFrame(ctx context.Context, handle windows.Handle, request Request) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("codificar requisição: %w", err)
	}
	if len(payload) > MaxPayloadBytes {
		return fmt.Errorf("requisição de %d bytes excede o limite", len(payload))
	}
	var prefix [4]byte
	binary.LittleEndian.PutUint32(prefix[:], uint32(len(payload)))
	if err := writeAll(ctx, handle, prefix[:]); err != nil {
		return err
	}
	return writeAll(ctx, handle, payload)
}

func writeAll(ctx context.Context, handle windows.Handle, payload []byte) error {
	event, err := newEvent()
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)

	overlapped := new(windows.Overlapped)
	overlapped.HEvent = event
	var written uint32
	transferred := written
	if err := windows.WriteFile(handle, payload, &written, overlapped); err != nil {
		if !errors.Is(err, windows.ERROR_IO_PENDING) {
			return fmt.Errorf("escrever no serviço: %w", err)
		}
		if err := wait(ctx, handle, event); err != nil {
			return err
		}
		transferred, err = overlappedResult(handle, overlapped)
		if err != nil {
			return fmt.Errorf("escrever no serviço: %w", err)
		}
	} else {
		transferred = written
	}
	if transferred != uint32(len(payload)) {
		return io.ErrShortWrite
	}
	return nil
}

func readFrame(ctx context.Context, handle windows.Handle) (Response, error) {
	var prefix [4]byte
	if err := readAll(ctx, handle, prefix[:]); err != nil {
		return Response{}, err
	}
	length := binary.LittleEndian.Uint32(prefix[:])
	if length == 0 || length > MaxPayloadBytes {
		return Response{}, fmt.Errorf("resposta de %d bytes fora do intervalo permitido", length)
	}
	payload := make([]byte, length)
	if err := readAll(ctx, handle, payload); err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.Unmarshal(payload, &response); err != nil {
		return Response{}, fmt.Errorf("decodificar resposta: %w", err)
	}
	return response, nil
}

func readAll(ctx context.Context, handle windows.Handle, buffer []byte) error {
	event, err := newEvent()
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)

	overlapped := new(windows.Overlapped)
	overlapped.HEvent = event
	var read uint32
	transferred := read
	if err := windows.ReadFile(handle, buffer, &read, overlapped); err != nil {
		if !errors.Is(err, windows.ERROR_IO_PENDING) {
			return transportError(err)
		}
		if err := wait(ctx, handle, event); err != nil {
			return err
		}
		transferred, err = overlappedResult(handle, overlapped)
		if err != nil {
			return transportError(err)
		}
	} else {
		transferred = read
	}
	if transferred == 0 {
		return io.EOF
	}
	return nil
}

func newEvent() (windows.Handle, error) {
	return windows.CreateEvent(nil, 1, 0, nil)
}

// wait blocks until the operation completes, the deadline passes or the caller
// gives up. Cancelling aborts the pending operation so the handle is not left
// with a queued read.
func wait(ctx context.Context, handle windows.Handle, event windows.Handle) error {
	for {
		result, err := windows.WaitForSingleObject(event, 100)
		if err != nil {
			return fmt.Errorf("aguardar o serviço: %w", err)
		}
		switch result {
		case uint32(windows.WAIT_OBJECT_0):
			return nil
		case uint32(windows.WAIT_TIMEOUT):
			if ctx.Err() != nil {
				_ = windows.CancelIo(handle)
				return fmt.Errorf("%w: %v", ErrServiceUnavailable, ctx.Err())
			}
		default:
			return fmt.Errorf("aguardar o serviço: resultado %d", result)
		}
	}
}

// overlappedResult retrieves the status and byte count of a completed call. The
// count is only reported here when the call did not finish inline.
func overlappedResult(handle windows.Handle, overlapped *windows.Overlapped) (uint32, error) {
	var transferred uint32
	err := windows.GetOverlappedResult(handle, overlapped, &transferred, false)
	return transferred, err
}

func transportError(err error) error {
	switch {
	case errors.Is(err, windows.ERROR_BROKEN_PIPE),
		errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED),
		errors.Is(err, windows.ERROR_NO_DATA),
		errors.Is(err, windows.ERROR_PIPE_CLOSING):
		return fmt.Errorf("%w: %v", ErrServiceUnavailable, err)
	case errors.Is(err, windows.ERROR_OPERATION_ABORTED):
		return fmt.Errorf("%w: operação cancelada", ErrServiceUnavailable)
	default:
		return fmt.Errorf("falha de comunicação: %w", err)
	}
}
