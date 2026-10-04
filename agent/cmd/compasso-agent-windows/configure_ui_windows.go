//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ssergio100/compasso/agent/localmsg"
	"github.com/ssergio100/compasso/agent/windowsipc"
	"github.com/ssergio100/compasso/agent/windowsservice"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	configurationBridgePrefix       = `\\.\pipe\CompassoConfigure-`
	configurationPayloadLimit       = 64 * 1024
	configurationConnectTimeout     = 15 * time.Second
	synchronizationConfirmationWait = 30 * time.Second
)

var requestAgentSynchronization = callAgentPipe

type uiConfigurationRequest struct {
	ServerURL         string `json:"server_url"`
	DeviceID          string `json:"device_id"`
	DeviceToken       string `json:"device_token"`
	ControlledUser    string `json:"controlled_user"`
	ControlledUserSID string `json:"controlled_user_sid"`
	Confirmed         bool   `json:"confirmed"`
}

type uiPublicConfiguration struct {
	Configured        bool   `json:"configured"`
	ServerURL         string `json:"server_url"`
	DeviceID          string `json:"device_id"`
	ControlledUserSID string `json:"controlled_user_sid"`
	HasDeviceToken    bool   `json:"has_device_token"`
}

type uiConfigurationResult struct {
	OK        bool                   `json:"ok"`
	ErrorCode string                 `json:"error_code,omitempty"`
	Message   string                 `json:"message"`
	Settings  *uiPublicConfiguration `json:"settings,omitempty"`
}

// configureFromUI is the elevated half of the Wails configuration flow. The
// nonce only names a one-shot pipe that the unelevated interface created with
// an administrators-only DACL. Credentials therefore stay in memory and never
// appear in arguments, files, stdout or logs.
func configureFromUI(nonce string) error {
	if !validConfigurationNonce(nonce) {
		return errors.New("invalid configuration bridge nonce")
	}
	handle, err := connectConfigurationBridge(configurationBridgePrefix + nonce)
	if err != nil {
		return fmt.Errorf("connect configuration bridge: %w", err)
	}
	defer windows.CloseHandle(handle)

	var request uiConfigurationRequest
	if err := readConfigurationFrame(handle, &request); err != nil {
		return fmt.Errorf("read configuration request: %w", err)
	}
	defer func() { request.DeviceToken = "" }()

	result := applyUIConfiguration(request)
	if err := writeConfigurationFrame(handle, result); err != nil {
		return fmt.Errorf("write configuration result: %w", err)
	}
	return nil
}

func validConfigurationNonce(nonce string) bool {
	if len(nonce) != 32 {
		return false
	}
	decoded, err := hex.DecodeString(nonce)
	if err != nil || len(decoded) != 16 {
		return false
	}
	var zero [16]byte
	return subtle.ConstantTimeCompare(decoded, zero[:]) == 0
}

func applyUIConfiguration(request uiConfigurationRequest) uiConfigurationResult {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return configurationFailure("authorization_required", "A autorização administrativa é necessária para configurar o Compasso.")
	}
	if !request.Confirmed {
		return configurationFailure("confirmation_required", "Confirme explicitamente a conta que poderá ser bloqueada.")
	}
	request.ServerURL = strings.TrimRight(strings.TrimSpace(request.ServerURL), "/")
	request.DeviceID = strings.TrimSpace(request.DeviceID)
	request.DeviceToken = strings.TrimSpace(request.DeviceToken)
	request.ControlledUser = strings.TrimSpace(request.ControlledUser)
	request.ControlledUserSID = strings.TrimSpace(request.ControlledUserSID)
	if err := windowsservice.ValidateControlledAccount(request.ControlledUser, request.ControlledUserSID); err != nil {
		return configurationFailure("invalid_account", "Escolha uma conta local válida que poderá ser controlada.")
	}

	configuration := windowsservice.Configuration{
		ServerURL: request.ServerURL, DeviceID: request.DeviceID,
		DeviceToken: request.DeviceToken, ControlledUserSID: request.ControlledUserSID,
	}
	if err := configuration.Validate(); err != nil {
		return configurationFailure("invalid_configuration", configurationValidationMessage(err))
	}
	path, err := defaultConfigurationPath()
	if err != nil {
		return configurationFailure("internal", "Não foi possível localizar a configuração do Compasso.")
	}
	previous, previousErr := windowsservice.LoadConfiguration(path)
	previousExists := previousErr == nil
	if previousErr != nil && !errors.Is(previousErr, os.ErrNotExist) {
		return configurationFailure("internal", "Não foi possível ler a configuração atual do Compasso.")
	}
	markerPath, err := windowsservice.DefaultSetupMarkerPath(os.Getenv("ProgramData"))
	if err != nil {
		return configurationFailure("internal", "Não foi possível localizar a confirmação do Compasso.")
	}
	previousMarker, markerWasPresent, err := readOptionalFile(markerPath)
	if err != nil {
		return configurationFailure("internal", "Não foi possível ler a confirmação atual do Compasso.")
	}
	if err := os.Remove(markerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return configurationFailure("internal", "Não foi possível preparar a nova configuração do Compasso.")
	}
	if err := windowsservice.SaveConfiguration(path, configuration); err != nil {
		restoreOptionalFile(markerPath, previousMarker, markerWasPresent)
		return configurationFailure("save_failed", "Não foi possível proteger e salvar a configuração do Compasso.")
	}
	if err := restartConfiguredService(); err != nil {
		restoreConfiguration(path, previous, previousExists)
		restoreOptionalFile(markerPath, previousMarker, markerWasPresent)
		return configurationFailure("service_start_failed", "A configuração foi restaurada porque o serviço Compasso não permaneceu ativo.")
	}

	status, detail := waitForConfiguredSynchronization(synchronizationConfirmationWait)
	if status != localmsg.StatusOnline {
		if detail == "" {
			detail = "O agente foi iniciado, mas o servidor ainda não confirmou a comunicação. Verifique a conexão e tente novamente."
		}
		code := "synchronization_failed"
		if strings.Contains(detail, "recusou a identificação") {
			code = "invalid_credentials"
		}
		return configurationFailure(code, detail)
	}
	if err := windowsservice.WriteSetupConfirmation(markerPath); err != nil {
		return configurationFailure("confirmation_failed", "O servidor respondeu, mas não foi possível concluir a configuração local.")
	}
	public := configuration.Public()
	return uiConfigurationResult{
		OK: true, Message: "Configuração concluída. O agente está ativo e o servidor respondeu com sucesso.",
		Settings: &uiPublicConfiguration{
			Configured: true, ServerURL: public.ServerURL, DeviceID: public.DeviceID,
			ControlledUserSID: public.ControlledUserSID, HasDeviceToken: public.HasDeviceToken,
		},
	}
}

func configurationFailure(code, message string) uiConfigurationResult {
	return uiConfigurationResult{OK: false, ErrorCode: code, Message: message}
}

func configurationValidationMessage(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "device_id and device_token"):
		return "Informe o identificador e o token do dispositivo."
	case strings.Contains(message, "must use HTTPS"):
		return "Use HTTPS quando o servidor estiver em outra máquina."
	case strings.Contains(message, "http(s) origin"):
		return "Informe apenas a origem HTTP(S) do servidor, sem caminho, credenciais, consulta ou fragmento."
	default:
		return "Revise os dados da configuração e tente novamente."
	}
}

func restartConfiguredService() error {
	if err := configureServiceAutomaticStart(); err != nil {
		return err
	}
	if err := stopService(); err != nil {
		return err
	}
	return startService()
}

func configureServiceAutomaticStart() error {
	return configureServiceStart(mgr.StartAutomatic, true)
}

func configureServiceManualStart() error {
	return configureServiceStart(mgr.StartManual, false)
}

func configureServiceStart(startType uint32, delayed bool) error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Service Control Manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("open Windows service: %w", err)
	}
	defer service.Close()
	configuration, err := service.Config()
	if err != nil {
		return fmt.Errorf("read Windows service configuration: %w", err)
	}
	configuration.StartType = startType
	configuration.DelayedAutoStart = delayed
	if err := service.UpdateConfig(configuration); err != nil {
		return fmt.Errorf("enable Windows service: %w", err)
	}
	return nil
}

func restoreConfiguration(path string, previous windowsservice.Configuration, previousExists bool) {
	if previousExists {
		_ = windowsservice.SaveConfiguration(path, previous)
		_ = configureServiceAutomaticStart()
		_ = startService()
		return
	}
	_ = os.Remove(path)
	_ = stopService()
	_ = configureServiceManualStart()
}

func readOptionalFile(path string) ([]byte, bool, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return contents, true, nil
}

func restoreOptionalFile(path string, contents []byte, wasPresent bool) {
	if wasPresent {
		_ = os.WriteFile(path, contents, 0o600)
		return
	}
	_ = os.Remove(path)
}

func waitForConfiguredSynchronization(timeout time.Duration) (string, string) {
	deadline := time.Now().Add(timeout)
	lastDetail := ""
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		attempt := 2 * time.Second
		if remaining < attempt {
			attempt = remaining
		}
		ctx, cancel := context.WithTimeout(context.Background(), attempt)
		response, err := requestAgentSynchronization(ctx, windowsipc.Request{Operation: windowsipc.OperationSynchronization})
		cancel()
		if err == nil && response.OK {
			if response.Status == localmsg.StatusOnline {
				return response.Status, ""
			}
			if response.Detail != "" {
				lastDetail = response.Detail
			}
			if response.Status == localmsg.StatusOffline && strings.Contains(response.Detail, "recusou a identificação") {
				return response.Status, response.Detail
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return localmsg.StatusOffline, lastDetail
}

func callAgentPipe(ctx context.Context, request windowsipc.Request) (windowsipc.Response, error) {
	handle, err := connectPipe(ctx, windowsipc.PipeName)
	if err != nil {
		return windowsipc.Response{}, err
	}
	defer windows.CloseHandle(handle)
	if err := writeConfigurationFrame(handle, request); err != nil {
		return windowsipc.Response{}, err
	}
	var response windowsipc.Response
	if err := readConfigurationFrame(handle, &response); err != nil {
		return windowsipc.Response{}, err
	}
	return response, nil
}

func connectConfigurationBridge(name string) (windows.Handle, error) {
	ctx, cancel := context.WithTimeout(context.Background(), configurationConnectTimeout)
	defer cancel()
	return connectPipe(ctx, name)
}

func connectPipe(ctx context.Context, name string) (windows.Handle, error) {
	nameUTF16, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	var lastErr error
	for ctx.Err() == nil {
		handle, err := windows.CreateFile(nameUTF16, windows.GENERIC_READ|windows.GENERIC_WRITE,
			0, nil, windows.OPEN_EXISTING, 0, 0)
		if err == nil {
			return handle, nil
		}
		lastErr = err
		if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return 0, err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, fmt.Errorf("pipe unavailable: %w", lastErr)
}

func writeConfigurationFrame(handle windows.Handle, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > configurationPayloadLimit {
		return errors.New("configuration payload is outside the allowed range")
	}
	var prefix [4]byte
	binary.LittleEndian.PutUint32(prefix[:], uint32(len(payload)))
	if err := writePipeMessage(handle, prefix[:]); err != nil {
		return err
	}
	return writePipeMessage(handle, payload)
}

func readConfigurationFrame(handle windows.Handle, destination any) error {
	var prefix [4]byte
	if err := readPipeMessage(handle, prefix[:]); err != nil {
		return err
	}
	length := binary.LittleEndian.Uint32(prefix[:])
	if length == 0 || length > configurationPayloadLimit {
		return errors.New("configuration payload is outside the allowed range")
	}
	payload := make([]byte, length)
	if err := readPipeMessage(handle, payload); err != nil {
		return err
	}
	defer func() {
		for index := range payload {
			payload[index] = 0
		}
	}()
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

func writePipeMessage(handle windows.Handle, payload []byte) error {
	var written uint32
	if err := windows.WriteFile(handle, payload, &written, nil); err != nil {
		return err
	}
	if written != uint32(len(payload)) {
		return io.ErrShortWrite
	}
	return nil
}

func readPipeMessage(handle windows.Handle, payload []byte) error {
	var read uint32
	if err := windows.ReadFile(handle, payload, &read, nil); err != nil {
		return err
	}
	if read != uint32(len(payload)) {
		return io.ErrUnexpectedEOF
	}
	return nil
}
