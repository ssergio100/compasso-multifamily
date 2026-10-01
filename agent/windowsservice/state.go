// Package windowsservice contains the platform-neutral state used by the
// Compasso Windows service entry point.
package windowsservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	StateRunning = "running"
	StateStopped = "stopped"
)

// State is a small diagnostic record. It deliberately contains no credentials
// or policy data.
type State struct {
	SchemaVersion int       `json:"schema_version"`
	State         string    `json:"state"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// StateStore atomically records service lifecycle transitions.
type StateStore struct {
	Path string
	Now  func() time.Time
}

// DefaultStatePath returns the lifecycle state location below ProgramData.
func DefaultStatePath(programData string) (string, error) {
	if programData == "" {
		return "", errors.New("ProgramData directory is required")
	}
	return filepath.Join(programData, "Compasso", "service-state.json"), nil
}

// Write persists one validated lifecycle state without exposing a partial
// document if the service is interrupted while writing.
func (s StateStore) Write(state string) error {
	if state != StateRunning && state != StateStopped {
		return fmt.Errorf("invalid service state %q", state)
	}
	if s.Path == "" {
		return errors.New("service state path is required")
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	record := State{SchemaVersion: 1, State: state, UpdatedAt: now().UTC()}
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode service state: %w", err)
	}
	encoded = append(encoded, '\n')

	directory := filepath.Dir(s.Path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create service state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".service-state-*")
	if err != nil {
		return fmt.Errorf("create temporary service state: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		_ = temporary.Close()
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(encoded); err != nil {
		return fmt.Errorf("write service state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync service state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close service state: %w", err)
	}
	if err := os.Rename(temporaryPath, s.Path); err != nil {
		return fmt.Errorf("replace service state: %w", err)
	}
	keepTemporary = false
	return nil
}
