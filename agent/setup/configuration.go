//go:build !windows

// Package setup implements the privileged, installation-time configuration of
// tempo-agent. It deliberately keeps device credentials out of command-line
// arguments and process listings.
package setup

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ssergio100/compasso/agent/config"
)

const maximumRequestBytes = 64 * 1024

// Request contains the values entered in the graphical first-run assistant.
type Request struct {
	ControlledUser string `json:"controlled_user"`
	ServerURL      string `json:"server_url"`
	DeviceID       string `json:"device_id"`
	DeviceToken    string `json:"device_token"`
}

// DecodeRequest reads exactly one JSON request from stdin. The caller must not
// log the returned value because it includes the device token.
func DecodeRequest(reader io.Reader) (Request, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, maximumRequestBytes))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, fmt.Errorf("read configuration request: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Request{}, errors.New("configuration request must contain one JSON object")
	}
	return request, nil
}

// Apply returns a complete validated agent configuration while preserving
// operational settings that are not shown in the first-run assistant.
func (request Request) Apply(current config.Config) (config.Config, error) {
	controlledUser := strings.TrimSpace(request.ControlledUser)
	serverURL := strings.TrimRight(strings.TrimSpace(request.ServerURL), "/")
	deviceID := strings.TrimSpace(request.DeviceID)
	deviceToken := request.DeviceToken

	if controlledUser == "" {
		return config.Config{}, errors.New("controlled user is required")
	}
	if serverURL == "" {
		return config.Config{}, errors.New("server address is required")
	}
	if deviceID == "" {
		return config.Config{}, errors.New("device ID is required")
	}
	if strings.TrimSpace(deviceToken) == "" {
		return config.Config{}, errors.New("device token is required")
	}
	if len(controlledUser) > 256 || len(serverURL) > 2048 || len(deviceID) > 1024 || len(deviceToken) > 8192 {
		return config.Config{}, errors.New("one or more configuration values are too long")
	}

	current.ControlledUser = controlledUser
	current.ServerURL = serverURL
	current.DeviceID = deviceID
	current.DeviceToken = deviceToken
	if err := current.Validate(); err != nil {
		return config.Config{}, err
	}
	return current, nil
}

// AccountLookup returns the numeric UID for a local Linux account.
type AccountLookup func(username string) (string, error)

// CheckReady confirms that a configuration is complete and targets an existing
// non-root account. It never contacts the server.
func CheckReady(settings config.Config, lookup AccountLookup) error {
	if !settings.SyncEnabled() {
		return errors.New("server address and device credentials are not configured")
	}
	if lookup == nil {
		return errors.New("account lookup is required")
	}
	uid, err := lookup(settings.ControlledUser)
	if err != nil {
		return fmt.Errorf("controlled user does not exist: %w", err)
	}
	if uid == "0" {
		return errors.New("root cannot be the controlled user")
	}
	return nil
}

// Render serializes the small flat TOML configuration understood by the agent.
func Render(settings config.Config) []byte {
	var output strings.Builder
	writer := bufio.NewWriter(&output)
	writeValue := func(key, value string) {
		_, _ = fmt.Fprintf(writer, "%s = %s\n", key, strconv.Quote(value))
	}
	writeValue("controlled_user", settings.ControlledUser)
	writeValue("database_path", settings.DatabasePath)
	writeValue("tick_interval", settings.TickInterval.String())
	writeValue("checkpoint_interval", settings.CheckpointInterval.String())
	writeValue("loginctl_path", settings.LoginctlPath)
	writer.WriteByte('\n')
	writeValue("server_url", settings.ServerURL)
	writeValue("device_id", settings.DeviceID)
	writeValue("device_token", settings.DeviceToken)
	writeValue("heartbeat_interval", settings.HeartbeatInterval.String())
	writeValue("http_timeout", settings.HTTPTimeout.String())
	_ = writer.Flush()
	return []byte(output.String())
}

// WriteFileAtomically replaces a root-owned configuration or marker without a
// window in which a partial file could be observed.
func WriteFileAtomically(path string, contents []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse to replace non-regular file %s", path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect destination: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".compasso-setup-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		_ = temporary.Close()
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("set temporary file mode: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace destination: %w", err)
	}
	keepTemporary = false

	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open destination directory: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync destination directory: %w", err)
	}
	return nil
}
