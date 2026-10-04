//go:build windows

package windowsservice

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupConfirmationLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Compasso", "setup-complete")
	confirmed, err := SetupConfirmed(path)
	if err != nil || confirmed {
		t.Fatalf("missing marker: confirmed=%v error=%v", confirmed, err)
	}
	if err := WriteSetupConfirmation(path); err != nil {
		t.Fatal(err)
	}
	confirmed, err = SetupConfirmed(path)
	if err != nil || !confirmed {
		t.Fatalf("written marker: confirmed=%v error=%v", confirmed, err)
	}
	if err := os.WriteFile(path, []byte("incomplete\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	confirmed, err = SetupConfirmed(path)
	if err != nil || confirmed {
		t.Fatalf("invalid marker: confirmed=%v error=%v", confirmed, err)
	}
}

func TestDefaultSetupMarkerPathRequiresProgramData(t *testing.T) {
	if _, err := DefaultSetupMarkerPath(""); err == nil {
		t.Fatal("expected an empty ProgramData path to be rejected")
	}
}
