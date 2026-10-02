//go:build windows

// Package ipcclient talks to the Compasso agent service over the local named
// pipe.
//
// The Wails application is a standalone Go module so it can ship as a portable
// installer without pulling in the agent engine and its database. Only this
// small wire contract is duplicated here; the canonical definition lives in
// agent/windowsipc/protocol_windows.go and the two must stay in sync.
package ipcclient

// Operation is the verb requested by the interface.
type Operation string

// Operations mirror the Linux D-Bus methods one to one.
const (
	OperationAddLocalBonus       Operation = "add_local_bonus"
	OperationSynchronization     Operation = "get_synchronization_report"
	OperationPing                Operation = "ping"
	OperationPublicConfiguration Operation = "get_public_configuration"
)

// PipeName is the machine-local named pipe used by the Compasso agent service.
const PipeName = `\\.\pipe\CompassoAgent`

// MaxPayloadBytes mirrors the server limit so a malformed reply is never used to
// size an allocation.
const MaxPayloadBytes = 64 * 1024

// Request is the framed message sent to the service. The password travels only
// inside the pipe payload, never in process arguments or logs.
type Request struct {
	Operation Operation `json:"operation"`
	Password  string    `json:"password,omitempty"`
	Seconds   int64     `json:"seconds,omitempty"`
}

// Response is the framed reply. Message and Detail carry the human wording built
// by the agent; raw service errors never cross this boundary.
type Response struct {
	OK        bool            `json:"ok"`
	ErrorCode string          `json:"error_code,omitempty"`
	Message   string          `json:"message,omitempty"`
	Status    string          `json:"status,omitempty"`
	Detail    string          `json:"detail,omitempty"`
	EventUUID string          `json:"event_uuid,omitempty"`
	Grant     *GrantSummary   `json:"grant,omitempty"`
	Settings  *PublicSettings `json:"settings,omitempty"`
}

// GrantSummary reports the durable bonus created by a successful request, so
// the interface can confirm what was persisted instead of assuming success.
type GrantSummary struct {
	UUID         string `json:"uuid"`
	BonusSeconds int64  `json:"bonus_seconds"`
	TotalSeconds int64  `json:"total_seconds"`
}

// PublicSettings carries configuration that is safe to show in the interface.
// The device token is never included.
type PublicSettings struct {
	ServerURL         string `json:"server_url"`
	DeviceID          string `json:"device_id"`
	ControlledUserSID string `json:"controlled_user_sid"`
	HasDeviceToken    bool   `json:"has_device_token"`
	Configured        bool   `json:"configured"`
}
