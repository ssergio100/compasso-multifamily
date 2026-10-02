//go:build windows

// Package windowsipc exposes the privileged agent operations to the Compasso
// interface over a Windows named pipe. It is the transport counterpart of the
// Linux system D-Bus API in agent/localapi: the bonus engine, the password
// verification and the wording all stay in the shared packages, and only the
// transport and its access control live here.
package windowsipc

// Operation is the verb requested by the interface.
type Operation string

// Operations mirror the Linux D-Bus methods one to one.
const (
	OperationAddLocalBonus       Operation = "add_local_bonus"
	OperationSynchronization     Operation = "get_synchronization_report"
	OperationPing                Operation = "ping"
	OperationPublicConfiguration       Operation = "get_public_configuration"
	OperationUpdatePublicConfiguration Operation = "update_public_configuration"
)

// PipeName is the machine-local named pipe used by the Compasso interface.
const PipeName = `\\.\pipe\CompassoAgent`

// Request is the framed message sent by the interface. The password travels
// only inside the pipe payload, never in process arguments or logs.
type Request struct {
	Operation Operation `json:"operation"`
	Password  string    `json:"password,omitempty"`
	Seconds   int64     `json:"seconds,omitempty"`
	ServerURL string    `json:"server_url,omitempty"`
	DeviceID  string    `json:"device_id,omitempty"`
	SID       string    `json:"sid,omitempty"`
	// SetTokenWhenPresent instructs the service to persist the device token
	// if a non-empty value is provided. The token must never be returned via
	// the pipe.
	SetTokenWhenPresent bool   `json:"set_token_when_present,omitempty"`
	DeviceToken         string `json:"device_token,omitempty"`
}

// Response is the framed reply. Detail and Message carry the human wording from
// agent/localmsg; raw synchronization errors never cross this boundary.
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

// GrantSummary reports the durable bonus created by a successful request so
// the interface can confirm the persisted result instead of assuming success.
type GrantSummary struct {
	UUID         string `json:"uuid"`
	BonusSeconds int64  `json:"bonus_seconds"`
	TotalSeconds int64  `json:"total_seconds"`
}

// PublicSettings carries configuration that is safe to show in the interface.
// The device token is never included: it stays protected by DPAPI in the
// service and must not be recoverable through the pipe.
type PublicSettings struct {
	ServerURL         string `json:"server_url"`
	DeviceID          string `json:"device_id"`
	ControlledUserSID string `json:"controlled_user_sid"`
	HasDeviceToken    bool   `json:"has_device_token"`
	Configured        bool   `json:"configured"`
}
