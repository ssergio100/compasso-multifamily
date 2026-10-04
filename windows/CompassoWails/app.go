package main

import (
	"context"
	"os"
	"os/user"
	"strings"

	"Compasso/ipcclient"
)

// App struct
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// InitialView opens the setup screen after a fresh install while preserving
// the normal Add time entry point for Start menu and desktop shortcuts.
func (a *App) InitialView() string {
	for _, argument := range os.Args[1:] {
		if strings.EqualFold(argument, "--settings") {
			return "settings"
		}
	}
	return "add"
}

// WindowsUser returns the account shown by the settings screen.
func (a *App) WindowsUser() string {
	current, err := user.Current()
	if err != nil {
		return "Conta atual"
	}
	parts := strings.Split(current.Username, `\`)
	return parts[len(parts)-1]
}

// AddTimeResult is what the Add time screen needs to render a reply. The
// interface shows the wording produced by the agent instead of inventing its
// own, so the same request behaves identically on Linux and Windows.
type AddTimeResult struct {
	OK           bool   `json:"ok"`
	Message      string `json:"message"`
	ErrorCode    string `json:"errorCode"`
	BonusSeconds int64  `json:"bonusSeconds"`
	TotalSeconds int64  `json:"totalSeconds"`
	EventUUID    string `json:"eventUuid"`
}

// AddTime asks the agent to grant the requested bonus. A wrong password, a rate
// limit or a missing configuration is a normal reply, not an exception, so the
// screen can explain what happened.
func (a *App) AddTime(password string, minutes int) AddTimeResult {
	if strings.TrimSpace(password) == "" {
		return AddTimeResult{OK: false, ErrorCode: "invalid_request", Message: "Informe a senha do responsável."}
	}
	if minutes != 15 && minutes != 30 && minutes != 60 && minutes != 120 {
		return AddTimeResult{OK: false, ErrorCode: "invalid_request", Message: "Escolha um período válido."}
	}
	response, err := ipcclient.Call(a.ctx, ipcclient.Request{
		Operation: ipcclient.OperationAddLocalBonus,
		Password:  password,
		Seconds:   int64(minutes) * 60,
	})
	if err != nil {
		return AddTimeResult{
			OK: false, ErrorCode: "unavailable",
			Message: "O serviço Compasso está indisponível. Abra as configurações para revisar a configuração.",
		}
	}
	if !response.OK {
		return AddTimeResult{OK: false, ErrorCode: response.ErrorCode, Message: response.Message}
	}
	if response.EventUUID == "" || response.Grant == nil || response.Grant.UUID != response.EventUUID {
		return AddTimeResult{
			OK: false, ErrorCode: "failed",
			Message: "O agente retornou uma resposta inválida.",
		}
	}
	return AddTimeResult{
		OK: true, EventUUID: response.EventUUID, Message: "Tempo adicionado.",
		BonusSeconds: response.Grant.BonusSeconds, TotalSeconds: response.Grant.TotalSeconds,
	}
}

// SyncState describes the agent connection for the header indicator.
type SyncState struct {
	Available bool   `json:"available"`
	Online    bool   `json:"online"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
}

// Synchronization reports whether the agent is reachable and in sync.
func (a *App) Synchronization() SyncState {
	response, err := ipcclient.Call(a.ctx, ipcclient.Request{
		Operation: ipcclient.OperationSynchronization,
	})
	if err != nil {
		return SyncState{Status: "unavailable", Detail: "Serviço do Compasso não está em execução."}
	}
	if !response.OK {
		return SyncState{Status: "unavailable", Detail: "Serviço do Compasso indisponível."}
	}
	online := response.Status == "online"
	state := SyncState{Available: true, Online: online, Status: response.Status, Detail: response.Detail}
	switch response.Status {
	case "online":
		state.Detail = ""
	case "checking":
		if state.Detail == "" {
			state.Detail = "Aguardando a primeira resposta do servidor."
		}
	default:
		state.Status = "offline"
		if state.Detail == "" {
			state.Detail = "A comunicação com o servidor falhou. Abra as configurações para revisar a configuração."
		}
	}
	return state
}

// AgentSettings is the configuration shown by the settings screen. The device
// token is never exposed through this boundary.
type AgentSettings struct {
	Configured        bool   `json:"configured"`
	ServerURL         string `json:"serverUrl"`
	DeviceID          string `json:"deviceId"`
	ControlledUserSID string `json:"controlledUserSid"`
	HasDeviceToken    bool   `json:"hasDeviceToken"`
}

// Settings reads the agent configuration that is safe to display.
func (a *App) Settings() AgentSettings {
	response, err := ipcclient.Call(a.ctx, ipcclient.Request{
		Operation: ipcclient.OperationPublicConfiguration,
	})
	if err != nil || response.Settings == nil {
		return AgentSettings{}
	}
	return AgentSettings{
		Configured:        response.Settings.Configured,
		ServerURL:         response.Settings.ServerURL,
		DeviceID:          response.Settings.DeviceID,
		ControlledUserSID: response.Settings.ControlledUserSID,
		HasDeviceToken:    response.Settings.HasDeviceToken,
	}
}

// Ping checks that the agent service answers, used by the screen on open.
func (a *App) Ping() bool {
	response, err := ipcclient.Call(a.ctx, ipcclient.Request{Operation: ipcclient.OperationPing})
	return err == nil && response.OK
}
