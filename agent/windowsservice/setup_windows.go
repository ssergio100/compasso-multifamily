//go:build windows

package windowsservice

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const setupConfirmedContents = "configured"

func DefaultSetupMarkerPath(programData string) (string, error) {
	if programData == "" {
		return "", errors.New("ProgramData directory is required")
	}
	return filepath.Join(programData, "Compasso", "setup-complete"), nil
}

func SetupConfirmed(path string) (bool, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read setup confirmation: %w", err)
	}
	return strings.TrimSpace(string(contents)) == setupConfirmedContents, nil
}

func WriteSetupConfirmation(path string) error {
	return replacePrivateFile(path, []byte(setupConfirmedContents+"\n"))
}

func replacePrivateFile(path string, contents []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create private state directory: %w", err)
	}
	if err := restrictDirectory(directory); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".compasso-state-*")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	temporaryPath := temporary.Name()
	keep := true
	defer func() {
		_ = temporary.Close()
		if keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(contents); err != nil {
		return fmt.Errorf("write private state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync private state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close private state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace private state: %w", err)
	}
	keep = false
	return nil
}
