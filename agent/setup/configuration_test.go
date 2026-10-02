//go:build !windows

package setup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssergio100/compasso/agent/config"
)

func baseConfiguration() config.Config {
	return config.Config{
		DatabasePath: "/var/lib/tempo-agent/tempo-agent.db", ControlledUser: "placeholder",
		TickInterval: time.Second, CheckpointInterval: 5 * time.Second,
		LoginctlPath: "/usr/bin/loginctl", HeartbeatInterval: 10 * time.Second,
		HTTPTimeout: 8 * time.Second,
	}
}

func TestRequestApplyProducesCompleteHTTPSConfiguration(t *testing.T) {
	settings, err := (Request{
		ControlledUser: " child ", ServerURL: " https://api.example.test/ ",
		DeviceID: " device-1 ", DeviceToken: "secret-token",
	}).Apply(baseConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if settings.ControlledUser != "child" || settings.ServerURL != "https://api.example.test" ||
		settings.DeviceID != "device-1" || settings.DeviceToken != "secret-token" {
		t.Fatalf("unexpected settings: %+v", settings)
	}
	if !settings.SyncEnabled() {
		t.Fatal("synchronization should be enabled")
	}
}

func TestRequestApplyRejectsRemotePlainHTTP(t *testing.T) {
	_, err := (Request{
		ControlledUser: "child", ServerURL: "http://192.0.2.10:8181",
		DeviceID: "device-1", DeviceToken: "secret-token",
	}).Apply(baseConfiguration())
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("remote HTTP error = %v", err)
	}
}

func TestDecodeRequestRejectsUnknownFields(t *testing.T) {
	_, err := DecodeRequest(bytes.NewBufferString(`{"controlled_user":"child","device_secret":"leak"}`))
	if err == nil {
		t.Fatal("unknown request field was accepted")
	}
}

func TestCheckReadyRejectsMissingAndRootAccounts(t *testing.T) {
	settings, err := (Request{
		ControlledUser: "child", ServerURL: "https://example.test",
		DeviceID: "device-1", DeviceToken: "secret-token",
	}).Apply(baseConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckReady(settings, func(string) (string, error) { return "", errors.New("missing") }); err == nil {
		t.Fatal("missing account was accepted")
	}
	if err := CheckReady(settings, func(string) (string, error) { return "0", nil }); err == nil {
		t.Fatal("root account was accepted")
	}
	if err := CheckReady(settings, func(string) (string, error) { return "1000", nil }); err != nil {
		t.Fatalf("regular account rejected: %v", err)
	}
}

func TestAtomicWriteUsesPrivateModeAndRoundTrips(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	settings, err := (Request{
		ControlledUser: "child", ServerURL: "https://example.test",
		DeviceID: "device-1", DeviceToken: "secret-token",
	}).Apply(baseConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomically(path, Render(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("configuration mode = %o", info.Mode().Perm())
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DeviceToken != settings.DeviceToken || loaded.ServerURL != settings.ServerURL {
		t.Fatalf("round trip mismatch: %+v", loaded)
	}
}

func TestAtomicWriteRefusesSymlinkDestination(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "config.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomically(link, []byte("replacement"), 0o600); err == nil {
		t.Fatal("symlink destination was accepted")
	}
}
