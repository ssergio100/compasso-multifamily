package windowsservice

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultStatePath(t *testing.T) {
	got, err := DefaultStatePath(`C:\ProgramData`)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(`C:\ProgramData`, "Compasso", "service-state.json")
	if got != want {
		t.Fatalf("state path = %q, want %q", got, want)
	}
}

func TestStateStoreWritesLifecycleStateAtomically(t *testing.T) {
	now := time.Date(2026, time.September, 30, 18, 30, 0, 0, time.FixedZone("BRT", -3*60*60))
	path := filepath.Join(t.TempDir(), "Compasso", "service-state.json")
	store := StateStore{Path: path, Now: func() time.Time { return now }}
	if err := store.Write(StateRunning); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(contents, &state); err != nil {
		t.Fatal(err)
	}
	if state.SchemaVersion != 1 || state.State != StateRunning || !state.UpdatedAt.Equal(now.UTC()) {
		t.Fatalf("unexpected stored state: %+v", state)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".service-state-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary state files remained: %v", matches)
	}
	if err := store.Write(StateStopped); err != nil {
		t.Fatal(err)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents, &state); err != nil {
		t.Fatal(err)
	}
	if state.State != StateStopped {
		t.Fatalf("replacement state = %q, want %q", state.State, StateStopped)
	}
}

func TestStateStoreRejectsUnknownState(t *testing.T) {
	store := StateStore{Path: filepath.Join(t.TempDir(), "state.json")}
	if err := store.Write("paused"); err == nil {
		t.Fatal("unknown lifecycle state was accepted")
	}
}
