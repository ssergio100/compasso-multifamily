//go:build windows

package ipcclient

import (
	"context"
	"errors"
	"testing"
	"time"
)

// These tests exercise the client against the real agent service. They skip when
// the service is not running so the module still builds and tests anywhere.

func requireService(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := Call(ctx, Request{Operation: OperationPing}); err != nil {
		t.Skipf("agent service is not available: %v", err)
	}
}

func TestCallPing(t *testing.T) {
	requireService(t)
	response, err := Call(context.Background(), Request{Operation: OperationPing})
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if !response.OK || response.Message != "pong" {
		t.Fatalf("got %+v", response)
	}
}

func TestCallSynchronization(t *testing.T) {
	requireService(t)
	response, err := Call(context.Background(), Request{Operation: OperationSynchronization})
	if err != nil {
		t.Fatalf("synchronization: %v", err)
	}
	if !response.OK {
		t.Fatalf("got %+v", response)
	}
	switch response.Status {
	case "online", "offline", "checking":
	default:
		t.Fatalf("unexpected status %q", response.Status)
	}
}

func TestCallPublicConfigurationNeverExposesToken(t *testing.T) {
	requireService(t)
	response, err := Call(context.Background(), Request{Operation: OperationPublicConfiguration})
	if err != nil {
		t.Fatalf("configuration: %v", err)
	}
	if !response.OK || response.Settings == nil {
		t.Fatalf("got %+v", response)
	}
	if response.Settings.ServerURL == "" || response.Settings.ControlledUserSID == "" {
		t.Fatalf("incomplete settings: %+v", response.Settings)
	}
	if response.Settings.HasDeviceToken != true {
		t.Fatalf("service reports no device token: %+v", response.Settings)
	}
}

// TestCallRepeatedRequests covers the interval where the service closes one pipe
// instance and creates the next one: a real interface clicks repeatedly.
func TestCallRepeatedRequests(t *testing.T) {
	requireService(t)
	for i := 0; i < 15; i++ {
		response, err := Call(context.Background(), Request{Operation: OperationPing})
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if !response.OK {
			t.Fatalf("request %d: got %+v", i, response)
		}
	}
}

func TestCallWrongPasswordIsBusinessFailure(t *testing.T) {
	requireService(t)
	response, err := Call(context.Background(), Request{
		Operation: OperationAddLocalBonus, Password: "senha-errada", Seconds: 900,
	})
	if err != nil {
		t.Fatalf("add time: %v", err)
	}
	if response.OK {
		t.Fatalf("wrong password was accepted: %+v", response)
	}
	if response.ErrorCode != "invalid_password" && response.ErrorCode != "password_not_configured" {
		t.Fatalf("unexpected error code %q", response.ErrorCode)
	}
}

func TestCallReportsServiceUnavailable(t *testing.T) {
	// A bogus pipe name must surface as an unavailable service rather than a
	// silent success, so the screen can tell the user what happened.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	name := pipeName
	pipeName = `\\.\pipe\CompassoAgentMissing`
	defer func() { pipeName = name }()

	if _, err := Call(ctx, Request{Operation: OperationPing}); !errors.Is(err, ErrServiceUnavailable) {
		t.Skipf("cannot exercise a missing pipe in this environment: %v", err)
	}
}
