package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.toml")
	contents := `
# Local installation settings.
database_path = "/var/lib/tempo-agent/agent.db"
controlled_user = "child"
tick_interval = "2s"
checkpoint_interval = "7s" # bounds crash loss
loginctl_path = "/bin/loginctl"
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.DatabasePath != "/var/lib/tempo-agent/agent.db" || got.ControlledUser != "child" ||
		got.TickInterval != 2*time.Second || got.CheckpointInterval != 7*time.Second ||
		got.LoginctlPath != "/bin/loginctl" || got.HeartbeatInterval != 3*time.Second {
		t.Fatalf("unexpected configuration: %+v", got)
	}
}

func TestLoadRequiresControlledUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.toml")
	if err := os.WriteFile(path, []byte(`database_path = "/tmp/agent.db"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected missing controlled_user to fail")
	}
}

func TestLoadWithPlatformDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.toml")
	if err := os.WriteFile(path, []byte(`controlled_user = "S-1-5-21-1002"`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWithDefaults(path, Defaults{DatabasePath: `C:\ProgramData\Compasso\Agent\agent.db`})
	if err != nil {
		t.Fatal(err)
	}
	if got.DatabasePath != `C:\ProgramData\Compasso\Agent\agent.db` || got.LoginctlPath != "" {
		t.Fatalf("platform defaults not applied: %+v", got)
	}
}

func TestLoadRejectsUnknownAndDuplicateKeys(t *testing.T) {
	for name, extra := range map[string]string{
		"unknown":   `log_level = "debug"`,
		"duplicate": `controlled_user = "other"`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.toml")
			contents := "database_path = \"/tmp/agent.db\"\ncontrolled_user = \"child\"\n" + extra
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatalf("configuration accepted %s key", name)
			}
		})
	}
}

func TestValidateRequiresHTTPSForRemoteSynchronization(t *testing.T) {
	configuration := Config{
		DatabasePath: "/var/lib/tempo-agent/agent.db", ControlledUser: "child",
		TickInterval: time.Second, CheckpointInterval: 5 * time.Second,
		LoginctlPath: "/usr/bin/loginctl", HeartbeatInterval: 10 * time.Second,
		HTTPTimeout: 8 * time.Second, DeviceID: "device", DeviceToken: "secret",
		ServerURL: "http://api.example.test",
	}
	if err := configuration.Validate(); err == nil {
		t.Fatal("remote plain HTTP was accepted")
	}
	configuration.ServerURL = "https://api.example.test"
	if err := configuration.Validate(); err != nil {
		t.Fatalf("remote HTTPS rejected: %v", err)
	}
	configuration.ServerURL = "http://127.0.0.1:8081"
	if err := configuration.Validate(); err != nil {
		t.Fatalf("loopback development HTTP rejected: %v", err)
	}
}
