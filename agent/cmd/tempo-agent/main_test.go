//go:build !windows

package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunWithoutEnrollmentDoesNotOpenStoredState(t *testing.T) {
	directory := t.TempDir()
	configurationPath := filepath.Join(directory, "config.toml")
	databasePath := filepath.Join(directory, "state", "tempo-agent.db")
	configuration := "database_path = \"" + databasePath + "\"\n" +
		"controlled_user = \"account-does-not-need-to-exist\"\n" +
		"tick_interval = \"1s\"\n" +
		"checkpoint_interval = \"5s\"\n" +
		"loginctl_path = \"/usr/bin/loginctl\"\n" +
		"server_url = \"\"\n" +
		"device_id = \"\"\n" +
		"device_token = \"\"\n" +
		"heartbeat_interval = \"10s\"\n" +
		"http_timeout = \"8s\"\n"
	if err := os.WriteFile(configurationPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run(configurationPath, log.New(&output, "", 0)); err != nil {
		t.Fatalf("unconfigured agent returned error: %v", err)
	}
	if _, err := os.Stat(databasePath); !os.IsNotExist(err) {
		t.Fatalf("unconfigured agent touched database: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("policy enforcement disabled")) {
		t.Fatalf("unexpected log output: %s", output.String())
	}
}

func TestSetupConfirmedRequiresValidMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-complete")
	confirmed, err := setupConfirmed(path)
	if err != nil || confirmed {
		t.Fatalf("missing marker confirmed=%t err=%v", confirmed, err)
	}
	if err := os.WriteFile(path, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	confirmed, err = setupConfirmed(path)
	if err != nil || confirmed {
		t.Fatalf("invalid marker confirmed=%t err=%v", confirmed, err)
	}
	if err := os.WriteFile(path, []byte("configured\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	confirmed, err = setupConfirmed(path)
	if err != nil || !confirmed {
		t.Fatalf("valid marker confirmed=%t err=%v", confirmed, err)
	}
}

func TestWaitForSetupConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-complete")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = os.WriteFile(path, []byte("configured\n"), 0o600)
	}()
	if !waitForSetupConfirmation(ctx, path, time.Millisecond) {
		t.Fatal("setup confirmation was not observed")
	}
}

func TestWaitForSetupConfirmationStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitForSetupConfirmation(ctx, filepath.Join(t.TempDir(), "missing"), time.Millisecond) {
		t.Fatal("cancelled wait reported setup confirmation")
	}
}
