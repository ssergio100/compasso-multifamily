//go:build windows

package windowsipc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ssergio100/compasso/agent/localauth"
	"golang.org/x/sys/windows"
)

// stubGranter grants every password, so a reply is always produced.
type stubGranter struct{}

func (stubGranter) Grant(_ context.Context, _ string, seconds int64, _ time.Time) (localauth.GrantResult, error) {
	return localauth.GrantResult{
		UUID:         "11111111-2222-3333-4444-555555555555",
		BonusSeconds: seconds, TotalSeconds: seconds,
	}, nil
}

type stubSync struct{}

func (stubSync) SynchronizationReport() (bool, bool, string) { return true, true, "sincronizado" }

// roundTrip performs one request and response over the real pipe.
func roundTrip(t *testing.T, request Request) Response {
	t.Helper()
	name, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		t.Fatalf("pipe name: %v", err)
	}
	var handle windows.Handle
	deadline := time.Now().Add(5 * time.Second)
	for {
		handle, err = windows.CreateFile(name,
			windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
			windows.OPEN_EXISTING, 0, 0)
		if err == nil {
			break
		}
		// The service builds the next instance after closing the previous one,
		// so a client can briefly find no instance to connect to.
		if time.Now().After(deadline) {
			t.Fatalf("connect to pipe: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer windows.CloseHandle(handle)

	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	var prefix [4]byte
	binary.LittleEndian.PutUint32(prefix[:], uint32(len(payload)))
	if err := windows.WriteFile(handle, prefix[:], nil, nil); err != nil {
		t.Fatalf("write prefix: %v", err)
	}
	if err := windows.WriteFile(handle, payload, nil, nil); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := windows.FlushFileBuffers(handle); err != nil {
		t.Fatalf("flush request: %v", err)
	}

	var read uint32
	if err := windows.ReadFile(handle, prefix[:], &read, nil); err != nil {
		t.Fatalf("read prefix: %v", err)
	}
	body := make([]byte, binary.LittleEndian.Uint32(prefix[:]))
	if err := windows.ReadFile(handle, body, &read, nil); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	var response Response
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}

// usePrivatePipe binds a name unique to the test so a running agent service
// never competes for PipeName.
func usePrivatePipe(t *testing.T) {
	t.Helper()
	previous := pipeName
	pipeName = `\\.\pipe\CompassoAgentTest` + fmt.Sprintf("%d-%s", os.Getpid(), t.Name())
	t.Cleanup(func() { pipeName = previous })
}

// startServer runs Serve until the test finishes.
func startServer(t *testing.T) {
	t.Helper()
	usePrivatePipe(t)
	listener, err := Listen("S-1-5-21-278194532-2139705530-887251162-1002")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	service := NewService(stubGranter{}, stubSync{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve returned %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("serve did not stop")
		}
	})
}

// TestServeHandlesManySequentialRequests guards the pipe lifecycle. An earlier
// implementation served only a few requests before the pipe stopped replying,
// which silently left the interface without a working channel.
func TestServeHandlesManySequentialRequests(t *testing.T) {
	startServer(t)
	for i := 0; i < 12; i++ {
		response := roundTrip(t, Request{Operation: OperationPing})
		if !response.OK {
			t.Fatalf("request %d: got %+v", i, response)
		}
	}
}

// TestServeInterleavesOperations covers payloads larger than the ping request,
// which exercises reading a frame that spans more than one pipe read.
func TestServeInterleavesOperations(t *testing.T) {
	startServer(t)
	for i := 0; i < 6; i++ {
		report := roundTrip(t, Request{Operation: OperationSynchronization})
		if !report.OK || report.Status != "online" {
			t.Fatalf("sync %d: got %+v", i, report)
		}
		grant := roundTrip(t, Request{
			Operation: OperationAddLocalBonus,
			Password:  "senha-com-comprimento-variado-para-o-quadro",
			Seconds:   900,
		})
		if !grant.OK || grant.Grant == nil || grant.Grant.BonusSeconds != 900 {
			t.Fatalf("grant %d: got %+v", i, grant)
		}
		if grant.EventUUID == "" {
			t.Fatalf("grant %d: missing durable event uuid", i)
		}
		ping := roundTrip(t, Request{Operation: OperationPing})
		if !ping.OK {
			t.Fatalf("ping %d after grant: got %+v", i, ping)
		}
	}
}

// TestServeReportsUnknownOperation checks that a malformed request still gets a
// reply instead of closing the connection.
func TestServeReportsUnknownOperation(t *testing.T) {
	startServer(t)
	response := roundTrip(t, Request{Operation: "delete_everything"})
	if response.OK || response.ErrorCode != "invalid_request" {
		t.Fatalf("got %+v", response)
	}
}
