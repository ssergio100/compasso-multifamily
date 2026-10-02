//go:build !windows

// Package syncstatus exchanges a small, root-only synchronization result
// between the daemon and the privileged graphical configuration helper.
package syncstatus

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	StateOnline  = "online"
	StateOffline = "offline"
)

// Report describes the latest synchronization state observed by this agent
// process. Detail must never contain a device token.
type Report struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// Write replaces path atomically with a private status report.
func Write(path string, report Report) error {
	if err := validate(report); err != nil {
		return err
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode synchronization status: %w", err)
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".synchronization-status-*")
	if err != nil {
		return fmt.Errorf("create synchronization status: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		_ = temporary.Close()
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set synchronization status permissions: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		return fmt.Errorf("write synchronization status: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync synchronization status: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close synchronization status: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace synchronization status: %w", err)
	}
	keepTemporary = false
	return nil
}

// Read decodes one bounded status report.
func Read(path string) (Report, error) {
	file, err := os.Open(path)
	if err != nil {
		return Report{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4096))
	decoder.DisallowUnknownFields()
	var report Report
	if err := decoder.Decode(&report); err != nil {
		return Report{}, fmt.Errorf("decode synchronization status: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Report{}, errors.New("decode synchronization status: trailing data")
	}
	if err := validate(report); err != nil {
		return Report{}, err
	}
	return report, nil
}

func validate(report Report) error {
	switch report.State {
	case StateOnline:
		if report.Detail != "" {
			return errors.New("online synchronization status cannot contain an error")
		}
	case StateOffline:
		if report.Detail == "" {
			return errors.New("offline synchronization status requires an error")
		}
	default:
		return errors.New("invalid synchronization state")
	}
	return nil
}
