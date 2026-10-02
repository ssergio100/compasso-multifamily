//go:build !windows

package syncstatus

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadPrivateReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synchronization-status.json")
	want := Report{State: StateOffline, Detail: "heartbeat returned HTTP 401"}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("report=%+v, want %+v", got, want)
	}
	information, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if permissions := information.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("permissions=%#o, want 0600", permissions)
	}
}

func TestReadRejectsUnknownOrInconsistentReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synchronization-status.json")
	for _, contents := range []string{
		`{"state":"online","detail":"unexpected"}`,
		`{"state":"offline"}`,
		`{"state":"online","unknown":true}`,
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(path); err == nil {
			t.Fatalf("invalid report accepted: %s", contents)
		}
	}
}
