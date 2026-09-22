package ipc

import "errors"

const ProtocolVersion = 1

type CommandKind string

const (
	CommandLock   CommandKind = "lock"
	CommandNotify CommandKind = "notify"
	CommandStop   CommandKind = "stop"
)

// Request is sent by the privileged service to the interactive companion.
type Request struct {
	Version int         `json:"version"`
	ID      string      `json:"id"`
	Command CommandKind `json:"command"`
	Title   string      `json:"title,omitempty"`
	Body    string      `json:"body,omitempty"`
}

func (r Request) Validate() error {
	if r.Version != ProtocolVersion {
		return errors.New("unsupported companion protocol version")
	}
	if r.ID == "" || len(r.ID) > 128 {
		return errors.New("invalid companion request identifier")
	}
	if r.Command != CommandLock && r.Command != CommandNotify && r.Command != CommandStop {
		return errors.New("unsupported companion command")
	}
	if r.Command == CommandNotify {
		if r.Title == "" || r.Body == "" || len(r.Title) > 120 || len(r.Body) > 500 {
			return errors.New("invalid companion notification")
		}
	} else if r.Title != "" || r.Body != "" {
		return errors.New("notification content is not valid for this command")
	}
	return nil
}

// Response acknowledges only whether the companion accepted and invoked the
// command. A successful response is not proof that Windows locked the desktop;
// that proof remains the SCM WTS_SESSION_LOCK event.
type Response struct {
	Version  int    `json:"version"`
	ID       string `json:"id"`
	Accepted bool   `json:"accepted"`
	Error    string `json:"error,omitempty"`
}

func (r Response) ValidateFor(request Request) error {
	if r.Version != ProtocolVersion || r.ID != request.ID {
		return errors.New("companion response does not match request")
	}
	if r.Accepted && r.Error != "" {
		return errors.New("accepted companion response contains an error")
	}
	if !r.Accepted && r.Error == "" {
		return errors.New("rejected companion response has no error")
	}
	return nil
}
