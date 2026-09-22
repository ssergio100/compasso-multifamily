package companion

import (
	"time"

	"github.com/ssergio100/compasso/agent/platform/windows/ipc"
)

type LifecycleKind string

const (
	LifecycleStarted          LifecycleKind = "started"
	LifecycleConnected        LifecycleKind = "connected"
	LifecycleExited           LifecycleKind = "exited"
	LifecycleRestartScheduled LifecycleKind = "restart_scheduled"
	LifecycleStopped          LifecycleKind = "stopped"
	LifecycleError            LifecycleKind = "error"
)

type LifecycleEvent struct {
	Kind       LifecycleKind `json:"kind"`
	SessionID  uint32        `json:"session_id,omitempty"`
	PID        uint32        `json:"pid,omitempty"`
	ExitCode   *uint32       `json:"exit_code,omitempty"`
	RetryAfter time.Duration `json:"retry_after,omitempty"`
	Error      string        `json:"error,omitempty"`
	ObservedAt time.Time     `json:"observed_at"`
}

type LifecycleSink interface {
	RecordCompanionLifecycle(LifecycleEvent) error
}

type CommandRecord struct {
	Request    ipc.Request   `json:"request"`
	Response   *ipc.Response `json:"response,omitempty"`
	Error      string        `json:"error,omitempty"`
	RecordedAt time.Time     `json:"recorded_at"`
}

type CommandSink interface {
	RecordCompanionCommand(CommandRecord) error
}

func restartDelay(failures int) time.Duration {
	if failures < 1 {
		return 0
	}
	delay := 250 * time.Millisecond
	for attempt := 1; attempt < failures && delay < 5*time.Second; attempt++ {
		delay *= 2
	}
	if delay > 5*time.Second {
		return 5 * time.Second
	}
	return delay
}
