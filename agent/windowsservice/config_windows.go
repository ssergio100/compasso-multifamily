//go:build windows

package windowsservice

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const configurationSchemaVersion = 1

type storedConfiguration struct {
	SchemaVersion     int    `json:"schema_version"`
	ServerURL         string `json:"server_url"`
	DeviceID          string `json:"device_id"`
	ProtectedToken    string `json:"device_token_dpapi"`
	ControlledUserSID string `json:"controlled_user_sid"`
}

func DefaultConfigurationPath(programData string) (string, error) {
	if programData == "" {
		return "", errors.New("ProgramData directory is required")
	}
	return filepath.Join(programData, "Compasso", "agent-config.json"), nil
}

func DefaultDatabasePath(programData string) (string, error) {
	if programData == "" {
		return "", errors.New("ProgramData directory is required")
	}
	return filepath.Join(programData, "Compasso", "agent.db"), nil
}

func SaveConfiguration(path string, configuration Configuration) error {
	configuration.ServerURL = strings.TrimRight(configuration.ServerURL, "/")
	if err := configuration.Validate(); err != nil {
		return err
	}
	if _, err := windows.StringToSid(configuration.ControlledUserSID); err != nil {
		return fmt.Errorf("parse controlled user SID: %w", err)
	}
	protected, err := protectMachineData([]byte(configuration.DeviceToken))
	if err != nil {
		return fmt.Errorf("protect device token: %w", err)
	}
	record := storedConfiguration{
		SchemaVersion: configurationSchemaVersion, ServerURL: configuration.ServerURL,
		DeviceID: configuration.DeviceID, ProtectedToken: base64.StdEncoding.EncodeToString(protected),
		ControlledUserSID: configuration.ControlledUserSID,
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Windows configuration: %w", err)
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	if err := restrictDirectory(directory); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".agent-config-*")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	keep := true
	defer func() {
		_ = temporary.Close()
		if keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(encoded); err != nil {
		return fmt.Errorf("write configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close configuration: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace configuration: %w", err)
	}
	keep = false
	return nil
}

func LoadConfiguration(path string) (Configuration, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return Configuration{}, err
	}
	var record storedConfiguration
	if err := json.Unmarshal(encoded, &record); err != nil {
		return Configuration{}, fmt.Errorf("decode Windows configuration: %w", err)
	}
	if record.SchemaVersion != configurationSchemaVersion {
		return Configuration{}, fmt.Errorf("unsupported Windows configuration schema %d", record.SchemaVersion)
	}
	protected, err := base64.StdEncoding.DecodeString(record.ProtectedToken)
	if err != nil {
		return Configuration{}, errors.New("device token protection data is invalid")
	}
	token, err := unprotectMachineData(protected)
	if err != nil {
		return Configuration{}, fmt.Errorf("unprotect device token: %w", err)
	}
	configuration := Configuration{
		ServerURL: record.ServerURL, DeviceID: record.DeviceID, DeviceToken: string(token),
		ControlledUserSID: record.ControlledUserSID,
	}
	clear(token)
	if err := configuration.Validate(); err != nil {
		return Configuration{}, err
	}
	if _, err := windows.StringToSid(configuration.ControlledUserSID); err != nil {
		return Configuration{}, fmt.Errorf("parse controlled user SID: %w", err)
	}
	return configuration, nil
}

func protectMachineData(value []byte) ([]byte, error) {
	return cryptData(value, true)
}

func unprotectMachineData(value []byte) ([]byte, error) {
	return cryptData(value, false)
}

func cryptData(value []byte, protect bool) ([]byte, error) {
	if len(value) == 0 {
		return nil, errors.New("data cannot be empty")
	}
	input := windows.DataBlob{Size: uint32(len(value)), Data: &value[0]}
	var output windows.DataBlob
	var err error
	if protect {
		err = windows.CryptProtectData(&input, nil, nil, 0, nil,
			windows.CRYPTPROTECT_UI_FORBIDDEN|windows.CRYPTPROTECT_LOCAL_MACHINE, &output)
	} else {
		err = windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	return append([]byte(nil), unsafe.Slice(output.Data, int(output.Size))...), nil
}

func restrictDirectory(path string) error {
	command := exec.Command("icacls.exe", path, "/inheritance:r", "/grant:r",
		"*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("restrict configuration directory ACL: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
