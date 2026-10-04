//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/user"
	"sort"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	configurationServiceName    = "CompassoAgent"
	configurationBridgePrefix   = `\\.\pipe\CompassoConfigure-`
	configurationPayloadLimit   = 64 * 1024
	configurationHelperTimeout  = 90 * time.Second
	fileFlagFirstPipeInstance   = 0x00080000
	netUserFilterNormalAccounts = 2
	netUserAccountDisabled      = 0x0002
	netUserNormalAccount        = 0x0200
	netMaxPreferredLength       = 0xffffffff
	netStatusMoreData           = 234
)

var (
	netapi32             = windows.NewLazySystemDLL("netapi32.dll")
	procNetUserEnum      = netapi32.NewProc("NetUserEnum")
	procNetApiBufferFree = netapi32.NewProc("NetApiBufferFree")
)

// WindowsAccount is an eligible local account that the agent can control.
type WindowsAccount struct {
	Name    string `json:"name"`
	SID     string `json:"sid"`
	Current bool   `json:"current"`
}

// ConfigureResult is the complete result of the elevated configuration flow.
// It deliberately cannot carry the device token back to JavaScript.
type ConfigureResult struct {
	OK        bool           `json:"ok"`
	ErrorCode string         `json:"errorCode"`
	Message   string         `json:"message"`
	Settings  *AgentSettings `json:"settings"`
}

type configurationRequest struct {
	ServerURL         string `json:"server_url"`
	DeviceID          string `json:"device_id"`
	DeviceToken       string `json:"device_token"`
	ControlledUser    string `json:"controlled_user"`
	ControlledUserSID string `json:"controlled_user_sid"`
	Confirmed         bool   `json:"confirmed"`
}

type configurationBridgeResult struct {
	OK        bool                   `json:"ok"`
	ErrorCode string                 `json:"error_code"`
	Message   string                 `json:"message"`
	Settings  *configurationSettings `json:"settings"`
}

type configurationSettings struct {
	Configured        bool   `json:"configured"`
	ServerURL         string `json:"server_url"`
	DeviceID          string `json:"device_id"`
	ControlledUserSID string `json:"controlled_user_sid"`
	HasDeviceToken    bool   `json:"has_device_token"`
}

type netUserInfo1 struct {
	Name        *uint16
	Password    *uint16
	PasswordAge uint32
	Privilege   uint32
	HomeDir     *uint16
	Comment     *uint16
	Flags       uint32
	ScriptPath  *uint16
}

// WindowsAccounts lists local, enabled, ordinary user accounts. The current
// account is placed first, matching the Linux setup assistant.
func (a *App) WindowsAccounts() []WindowsAccount {
	accounts, err := eligibleWindowsAccounts()
	if err != nil {
		return []WindowsAccount{}
	}
	return accounts
}

// Configure asks an elevated copy of the installed agent to save the
// configuration, restart the service and wait for its first heartbeat. The
// credential crosses only a one-shot administrators-only named pipe.
func (a *App) Configure(serverURL, deviceID, deviceToken, controlledUser, controlledUserSID string, confirmed bool) ConfigureResult {
	request := configurationRequest{
		ServerURL: strings.TrimRight(strings.TrimSpace(serverURL), "/"),
		DeviceID:  strings.TrimSpace(deviceID), DeviceToken: strings.TrimSpace(deviceToken),
		ControlledUser: strings.TrimSpace(controlledUser), ControlledUserSID: strings.TrimSpace(controlledUserSID),
		Confirmed: confirmed,
	}
	if message := validateConfigurationForm(request); message != "" {
		return ConfigureResult{ErrorCode: "invalid_configuration", Message: message}
	}
	accounts, err := eligibleWindowsAccounts()
	if err != nil || !containsAccount(accounts, request.ControlledUser, request.ControlledUserSID) {
		return ConfigureResult{ErrorCode: "invalid_account", Message: "Escolha uma conta local válida que poderá ser controlada."}
	}

	serviceExecutable, err := installedServiceExecutable()
	if err != nil {
		return ConfigureResult{ErrorCode: "service_not_installed", Message: "O serviço Compasso não está instalado neste computador."}
	}
	bridge, nonce, err := newConfigurationBridge()
	if err != nil {
		return ConfigureResult{ErrorCode: "internal", Message: "Não foi possível preparar a autorização administrativa."}
	}
	defer bridge.Close()
	if err := launchElevatedConfigurationHelper(serviceExecutable, nonce); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return ConfigureResult{ErrorCode: "authorization_cancelled", Message: "A autorização administrativa foi cancelada."}
		}
		return ConfigureResult{ErrorCode: "authorization_failed", Message: "Não foi possível abrir a autorização administrativa."}
	}

	ctx, cancel := context.WithTimeout(a.ctx, configurationHelperTimeout)
	defer cancel()
	connection, err := bridge.Accept(ctx)
	if err != nil {
		return ConfigureResult{ErrorCode: "authorization_failed", Message: "O componente administrativo não respondeu."}
	}
	defer connection.Close()
	if err := connection.WriteFrame(request); err != nil {
		return ConfigureResult{ErrorCode: "internal", Message: "Não foi possível enviar a configuração ao serviço."}
	}
	var response configurationBridgeResult
	if err := connection.ReadFrame(&response); err != nil {
		return ConfigureResult{ErrorCode: "internal", Message: "O serviço não concluiu a configuração."}
	}
	result := ConfigureResult{OK: response.OK, ErrorCode: response.ErrorCode, Message: response.Message}
	if response.Settings != nil {
		result.Settings = &AgentSettings{
			Configured: response.Settings.Configured, ServerURL: response.Settings.ServerURL,
			DeviceID: response.Settings.DeviceID, ControlledUserSID: response.Settings.ControlledUserSID,
			HasDeviceToken: response.Settings.HasDeviceToken,
		}
	}
	return result
}

func validateConfigurationForm(request configurationRequest) string {
	if !request.Confirmed {
		return "Confirme explicitamente a conta que poderá ser bloqueada."
	}
	if request.ControlledUser == "" || request.ControlledUserSID == "" {
		return "Escolha a conta Windows que será controlada."
	}
	parsed, err := url.Parse(request.ServerURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "Informe um endereço válido para o servidor."
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return "Informe apenas a origem do servidor, sem caminho, credenciais, consulta ou fragmento."
	}
	if parsed.Scheme == "http" && !isLoopbackServer(parsed.Hostname()) {
		return "Use HTTPS quando o servidor estiver em outra máquina."
	}
	if request.DeviceID == "" {
		return "Informe o identificador do dispositivo."
	}
	if request.DeviceToken == "" {
		return "Informe o token do dispositivo."
	}
	return ""
}

func isLoopbackServer(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func containsAccount(accounts []WindowsAccount, name, sid string) bool {
	for _, account := range accounts {
		if strings.EqualFold(account.Name, name) && strings.EqualFold(account.SID, sid) {
			return true
		}
	}
	return false
}

func eligibleWindowsAccounts() ([]WindowsAccount, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	currentSID := ""
	if current, err := user.Current(); err == nil {
		currentSID = current.Uid
	}
	var accounts []WindowsAccount
	var resume uint32
	for {
		var buffer *byte
		var entriesRead, totalEntries uint32
		status, _, _ := procNetUserEnum.Call(
			0, 1, netUserFilterNormalAccounts, uintptr(unsafe.Pointer(&buffer)),
			netMaxPreferredLength, uintptr(unsafe.Pointer(&entriesRead)),
			uintptr(unsafe.Pointer(&totalEntries)), uintptr(unsafe.Pointer(&resume)),
		)
		if buffer != nil {
			entries := unsafe.Slice((*netUserInfo1)(unsafe.Pointer(buffer)), entriesRead)
			for _, entry := range entries {
				if entry.Name == nil || entry.Flags&netUserAccountDisabled != 0 || entry.Flags&netUserNormalAccount == 0 {
					continue
				}
				name := windows.UTF16PtrToString(entry.Name)
				sid, _, accountType, lookupErr := windows.LookupSID(hostname, hostname+`\`+name)
				if lookupErr != nil || accountType != windows.SidTypeUser {
					continue
				}
				value := sid.String()
				accounts = append(accounts, WindowsAccount{Name: name, SID: value, Current: strings.EqualFold(value, currentSID)})
			}
			_, _, _ = procNetApiBufferFree.Call(uintptr(unsafe.Pointer(buffer)))
		}
		if status == 0 {
			break
		}
		if status != netStatusMoreData {
			return nil, fmt.Errorf("NetUserEnum failed with status %d", status)
		}
	}
	sort.Slice(accounts, func(left, right int) bool {
		if accounts[left].Current != accounts[right].Current {
			return accounts[left].Current
		}
		return strings.ToLower(accounts[left].Name) < strings.ToLower(accounts[right].Name)
	})
	return accounts, nil
}

func installedServiceExecutable() (string, error) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return "", err
	}
	defer windows.CloseServiceHandle(manager)
	serviceName, err := windows.UTF16PtrFromString(configurationServiceName)
	if err != nil {
		return "", err
	}
	handle, err := windows.OpenService(manager, serviceName, windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		return "", err
	}
	service := &mgr.Service{Name: configurationServiceName, Handle: handle}
	defer service.Close()
	configuration, err := service.Config()
	if err != nil {
		return "", err
	}
	executable, err := executableFromServiceCommand(configuration.BinaryPathName)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(executable); err != nil {
		return "", err
	}
	return executable, nil
}

func executableFromServiceCommand(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("empty service command")
	}
	if command[0] == '"' {
		end := strings.Index(command[1:], `"`)
		if end < 0 {
			return "", errors.New("unterminated executable quote")
		}
		return command[1 : end+1], nil
	}
	if separator := strings.IndexAny(command, " \t"); separator >= 0 {
		return command[:separator], nil
	}
	return command, nil
}

func launchElevatedConfigurationHelper(executable, nonce string) error {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(executable)
	arguments, _ := windows.UTF16PtrFromString("configure-ui " + nonce)
	return windows.ShellExecute(0, verb, file, arguments, nil, windows.SW_HIDE)
}

type configurationBridge struct {
	handle windows.Handle
}

func newConfigurationBridge() (*configurationBridge, string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return nil, "", err
	}
	nonce := hex.EncodeToString(random)
	security, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;SY)(A;;GA;;;BA)")
	if err != nil {
		return nil, "", err
	}
	attributes := &windows.SecurityAttributes{
		Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: security,
	}
	name, err := windows.UTF16PtrFromString(configurationBridgePrefix + nonce)
	if err != nil {
		return nil, "", err
	}
	handle, err := windows.CreateNamedPipe(name,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|fileFlagFirstPipeInstance,
		windows.PIPE_TYPE_MESSAGE|windows.PIPE_READMODE_MESSAGE|windows.PIPE_WAIT,
		1, configurationPayloadLimit, configurationPayloadLimit, 0, attributes)
	if err != nil {
		return nil, "", err
	}
	return &configurationBridge{handle: handle}, nonce, nil
}

func (bridge *configurationBridge) Accept(ctx context.Context) (*configurationConnection, error) {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(event)
	overlapped := &windows.Overlapped{HEvent: event}
	err = windows.ConnectNamedPipe(bridge.handle, overlapped)
	switch {
	case err == nil, errors.Is(err, windows.ERROR_PIPE_CONNECTED):
	case errors.Is(err, windows.ERROR_IO_PENDING):
		if _, err := waitConfigurationIO(ctx, bridge.handle, event, overlapped); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	handle := bridge.handle
	bridge.handle = 0
	return &configurationConnection{handle: handle, ctx: ctx}, nil
}

func (bridge *configurationBridge) Close() error {
	if bridge == nil || bridge.handle == 0 {
		return nil
	}
	_ = windows.CancelIoEx(bridge.handle, nil)
	err := windows.CloseHandle(bridge.handle)
	bridge.handle = 0
	return err
}

type configurationConnection struct {
	handle windows.Handle
	ctx    context.Context
}

func (connection *configurationConnection) WriteFrame(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > configurationPayloadLimit {
		return errors.New("configuration payload is outside the allowed range")
	}
	var prefix [4]byte
	binary.LittleEndian.PutUint32(prefix[:], uint32(len(payload)))
	if err := connection.transfer(prefix[:], true); err != nil {
		return err
	}
	return connection.transfer(payload, true)
}

func (connection *configurationConnection) ReadFrame(destination any) error {
	var prefix [4]byte
	if err := connection.transfer(prefix[:], false); err != nil {
		return err
	}
	length := binary.LittleEndian.Uint32(prefix[:])
	if length == 0 || length > configurationPayloadLimit {
		return errors.New("configuration payload is outside the allowed range")
	}
	payload := make([]byte, length)
	if err := connection.transfer(payload, false); err != nil {
		return err
	}
	return json.Unmarshal(payload, destination)
}

func (connection *configurationConnection) transfer(payload []byte, write bool) error {
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(event)
	overlapped := &windows.Overlapped{HEvent: event}
	var transferred uint32
	if write {
		err = windows.WriteFile(connection.handle, payload, &transferred, overlapped)
	} else {
		err = windows.ReadFile(connection.handle, payload, &transferred, overlapped)
	}
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		transferred, err = waitConfigurationIO(connection.ctx, connection.handle, event, overlapped)
	}
	if err != nil {
		return err
	}
	if transferred != uint32(len(payload)) {
		if write {
			return io.ErrShortWrite
		}
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (connection *configurationConnection) Close() error {
	if connection == nil || connection.handle == 0 {
		return nil
	}
	_ = windows.CancelIoEx(connection.handle, nil)
	_ = windows.FlushFileBuffers(connection.handle)
	_ = windows.DisconnectNamedPipe(connection.handle)
	err := windows.CloseHandle(connection.handle)
	connection.handle = 0
	return err
}

func waitConfigurationIO(ctx context.Context, handle, event windows.Handle, overlapped *windows.Overlapped) (uint32, error) {
	for {
		result, err := windows.WaitForSingleObject(event, 100)
		if err != nil {
			return 0, err
		}
		if result == uint32(windows.WAIT_OBJECT_0) {
			var transferred uint32
			err := windows.GetOverlappedResult(handle, overlapped, &transferred, false)
			return transferred, err
		}
		if result != uint32(windows.WAIT_TIMEOUT) {
			return 0, fmt.Errorf("unexpected wait result %d", result)
		}
		if ctx.Err() != nil {
			_ = windows.CancelIoEx(handle, overlapped)
			var transferred uint32
			_ = windows.GetOverlappedResult(handle, overlapped, &transferred, true)
			return transferred, ctx.Err()
		}
	}
}
