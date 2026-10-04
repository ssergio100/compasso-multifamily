//go:build windows

package main

import (
	"context"
	"encoding/json"
	"os/user"
	"strings"
	"testing"
	"time"

	"github.com/ssergio100/compasso/agent/windowsipc"
	"github.com/ssergio100/compasso/agent/windowsservice"
)

func TestConfigurationBridgeNonce(t *testing.T) {
	if !validConfigurationNonce("0123456789abcdef0123456789abcdef") {
		t.Fatal("valid random nonce rejected")
	}
	for _, nonce := range []string{"", "abcd", "00000000000000000000000000000000", "xyzxyzxyzxyzxyzxyzxyzxyzxyzxyzxy"} {
		if validConfigurationNonce(nonce) {
			t.Fatalf("invalid nonce %q accepted", nonce)
		}
	}
}

func TestCurrentUserSIDIsAcceptedForConfiguration(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(current.Username, `\`)
	name := parts[len(parts)-1]
	if err := windowsservice.ValidateControlledAccount(name, current.Uid); err != nil {
		t.Fatalf("current local user SID rejected: %v", err)
	}
}

func TestConfigurationResultCannotExposeToken(t *testing.T) {
	result := uiConfigurationResult{
		OK: true, Message: "configured",
		Settings: &uiPublicConfiguration{
			Configured: true, ServerURL: "https://example.test", DeviceID: "device",
			ControlledUserSID: "S-1-5-21-1-2-3-1001", HasDeviceToken: true,
		},
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"device_token":`) {
		t.Fatalf("result exposed a token field: %s", encoded)
	}
}

func TestConfigurationValidationMessagesAreSanitized(t *testing.T) {
	for _, raw := range []string{
		"device_id and device_token are required",
		"server_url must use HTTPS unless it points to the local machine",
		"server_url must be an http(s) origin without path, credentials, query or fragment",
		"unexpected internal detail secret-token",
	} {
		message := configurationValidationMessage(assertionError(raw))
		if message == "" || strings.Contains(message, "secret-token") {
			t.Fatalf("unsafe message for %q: %q", raw, message)
		}
	}
}

func TestWaitForConfiguredSynchronizationAcceptsFirstHeartbeat(t *testing.T) {
	original := requestAgentSynchronization
	requestAgentSynchronization = func(context.Context, windowsipc.Request) (windowsipc.Response, error) {
		return windowsipc.Response{OK: true, Status: "online"}, nil
	}
	defer func() { requestAgentSynchronization = original }()

	status, detail := waitForConfiguredSynchronization(time.Second)
	if status != "online" || detail != "" {
		t.Fatalf("status=%q detail=%q", status, detail)
	}
}

func TestWaitForConfiguredSynchronizationRejectsInvalidCredentials(t *testing.T) {
	original := requestAgentSynchronization
	requestAgentSynchronization = func(context.Context, windowsipc.Request) (windowsipc.Response, error) {
		return windowsipc.Response{
			OK: true, Status: "offline",
			Detail: "O servidor recusou a identificação deste computador. Revise o dispositivo e o token nas configurações.",
		}, nil
	}
	defer func() { requestAgentSynchronization = original }()

	status, detail := waitForConfiguredSynchronization(time.Second)
	if status != "offline" || !strings.Contains(detail, "recusou a identificação") {
		t.Fatalf("status=%q detail=%q", status, detail)
	}
}

type assertionError string

func (err assertionError) Error() string { return string(err) }
