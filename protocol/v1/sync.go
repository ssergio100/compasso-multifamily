// Package v1 defines the versioned heartbeat contract shared by server and agent.
package v1

import (
	"encoding/json"
	"time"
)

const (
	HeartbeatPath                  = "/api/v1/device/heartbeat"
	VersionHeader                  = "X-Compasso-Protocol-Version"
	CapabilitiesHeader             = "X-Compasso-Capabilities"
	InstallationIDHeader           = "X-Compasso-Agent-Installation-ID"
	InstallationIdentityCapability = "installation-identity"
	NextHeartbeatCapability        = "next-heartbeat-seconds"
	CommandAckReceiptCapability    = "command-ack-receipts"
	CurrentProtocolVersion         = "2"
)

type HeartbeatRequest struct {
	PolicyRevision         int64          `json:"policy_revision"`
	ControlRevision        int64          `json:"control_revision,omitempty"`
	SessionStateRevision   int64          `json:"session_state_revision,omitempty"`
	LocalDate              string         `json:"local_date"`
	SecondsUsed            int64          `json:"seconds_used"`
	GraphicalSessionActive bool           `json:"graphical_session_active"`
	GraphicalSessionID     string         `json:"graphical_session_id,omitempty"`
	GraphicalSessionLocked bool           `json:"graphical_session_locked,omitempty"`
	RequestSessionState    bool           `json:"request_session_state,omitempty"`
	Events                 []PendingEvent `json:"events,omitempty"`
	CommandAcks            []string       `json:"command_acks,omitempty"`
}

type PendingEvent struct {
	UUID      string    `json:"uuid"`
	Kind      string    `json:"kind"`
	Payload   jsonRaw   `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

type jsonRaw = json.RawMessage

type HeartbeatResponse struct {
	ServerTime           time.Time     `json:"server_time"`
	NextHeartbeatSeconds int64         `json:"next_heartbeat_seconds,omitempty"`
	AcknowledgedEvents   []string      `json:"acknowledged_events,omitempty"`
	AcknowledgedCommands []string      `json:"acknowledged_commands,omitempty"`
	Policy               *Policy       `json:"policy,omitempty"`
	SessionState         *SessionState `json:"session_state,omitempty"`
	Commands             []Command     `json:"commands,omitempty"`
	Control              Control       `json:"control"`
}

// Control is online-only authority and must be discarded after heartbeat failure.
type Control struct {
	Revision         int64 `json:"revision"`
	MonitoringPaused bool  `json:"monitoring_paused"`
	ManualBlock      bool  `json:"manual_block"`
}

// SessionState is an authoritative balance anchor. The agent applies it once
// for the identified graphical session and then subtracts locally measured
// elapsed usage until a new revision is explicitly delivered.
type SessionState struct {
	Revision         int64     `json:"revision"`
	SessionID        string    `json:"session_id"`
	LocalDate        string    `json:"local_date"`
	RemainingSeconds int64     `json:"remaining_seconds"`
	UsageSeconds     int64     `json:"usage_seconds"`
	ConfirmedAt      time.Time `json:"confirmed_at"`
}

type Policy struct {
	Revision              int64     `json:"revision"`
	MonitoringPaused      bool      `json:"monitoring_paused"`
	ManualBlock           bool      `json:"manual_block"`
	WarningMinutes        int       `json:"warning_minutes"`
	LocalPasswordVerifier string    `json:"local_password_verifier,omitempty"`
	WeeklyQuotaSeconds    [7]int64  `json:"weekly_quota_seconds"`
	Routines              []Routine `json:"routines,omitempty"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type Routine struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Days        [7]bool `json:"days"`
	StartSecond int64   `json:"start_second"`
	EndSecond   int64   `json:"end_second"`
	Enabled     bool    `json:"enabled"`
}

type Command struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Payload   jsonRaw   `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

type BonusPayload struct {
	UUID      string    `json:"uuid,omitempty"`
	LocalDate string    `json:"local_date"`
	Seconds   int64     `json:"seconds"`
	Origin    string    `json:"origin"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// CreditIncrementPayload adds credit to the daily balance active when the
// command reaches the device. It deliberately has no calendar date.
type CreditIncrementPayload struct {
	UUID      string    `json:"uuid,omitempty"`
	Seconds   int64     `json:"seconds"`
	Origin    string    `json:"origin"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type ErrorResponse struct {
	Error          string `json:"error"`
	Code           string `json:"code,omitempty"`
	CorrelationID  string `json:"correlation_id,omitempty"`
	ClientRevision int64  `json:"client_revision,omitempty"`
	ServerRevision int64  `json:"server_revision,omitempty"`
}
