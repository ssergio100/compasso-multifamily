//go:build !windows

package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssergio100/compasso/agent/syncstatus"
)

func TestWaitForSuccessfulSynchronizationAcceptsOnlineReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	if err := syncstatus.Write(path, syncstatus.Report{State: syncstatus.StateOnline}); err != nil {
		t.Fatal(err)
	}
	if err := waitForSuccessfulSynchronization(path, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForSuccessfulSynchronizationReportsRejectedCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	if err := syncstatus.Write(path, syncstatus.Report{
		State: syncstatus.StateOffline, Detail: "heartbeat returned HTTP 401",
	}); err != nil {
		t.Fatal(err)
	}
	err := waitForSuccessfulSynchronization(path, time.Second)
	if err == nil || !strings.Contains(err.Error(), "rejected device credentials") {
		t.Fatalf("credential rejection error=%v", err)
	}
}

func TestWaitForSuccessfulSynchronizationTimesOutWithoutReport(t *testing.T) {
	err := waitForSuccessfulSynchronization(filepath.Join(t.TempDir(), "missing"), time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "was not confirmed") {
		t.Fatalf("missing synchronization error=%v", err)
	}
}
