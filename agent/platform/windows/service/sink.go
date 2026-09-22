package service

import (
	"encoding/json"
	"errors"
	"os"
	"sync"

	windowscompanion "github.com/ssergio100/compasso/agent/platform/windows/companion"
	windowsession "github.com/ssergio100/compasso/agent/platform/windows/session"
)

// SessionRecord is the durable diagnostic emitted for an SCM session event.
// Console is the physical console observed immediately after that event.
type SessionRecord struct {
	Type    string                  `json:"type"`
	Event   windowsession.Event     `json:"event"`
	Console *windowsession.Snapshot `json:"console,omitempty"`
}

type SessionSink interface {
	Record(SessionRecord) error
}

// JSONLinesSink appends one complete JSON object per event and flushes it to
// disk. It is intentionally simple so the first service spike can be audited
// without relying on a registered Windows Event Log source.
type JSONLinesSink struct {
	mu   sync.Mutex
	file *os.File
}

func NewJSONLinesSink(path string) (*JSONLinesSink, error) {
	if path == "" {
		return nil, errors.New("session event log path is required")
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &JSONLinesSink{file: file}, nil
}

func (s *JSONLinesSink) Record(record SessionRecord) error {
	if record.Type == "" {
		record.Type = "session"
	}
	return s.record(record)
}

func (s *JSONLinesSink) RecordCompanionCommand(record windowscompanion.CommandRecord) error {
	return s.record(struct {
		Type   string                         `json:"type"`
		Record windowscompanion.CommandRecord `json:"record"`
	}{Type: "companion_command", Record: record})
}

func (s *JSONLinesSink) RecordCompanionLifecycle(event windowscompanion.LifecycleEvent) error {
	return s.record(struct {
		Type  string                          `json:"type"`
		Event windowscompanion.LifecycleEvent `json:"event"`
	}{Type: "companion_lifecycle", Event: event})
}

func (s *JSONLinesSink) record(record any) error {
	if s == nil || s.file == nil {
		return errors.New("session event sink is closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := json.NewEncoder(s.file).Encode(record); err != nil {
		return err
	}
	return s.file.Sync()
}

func (s *JSONLinesSink) Close() error {
	if s == nil || s.file == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.file.Close()
	s.file = nil
	return err
}
