package service

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	windowsession "github.com/ssergio100/compasso/agent/platform/windows/session"
)

func TestJSONLinesSinkPersistsCompleteRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	sink, err := NewJSONLinesSink(path)
	if err != nil {
		t.Fatal(err)
	}
	record := SessionRecord{
		Event: windowsession.Event{Kind: windowsession.EventLock, SessionID: 7, ObservedAt: time.Now().UTC()},
		Console: &windowsession.Snapshot{
			SessionID: 7, AccountSID: "S-1-5-21-1002", Console: true,
			Connection: windowsession.ConnectionActive, Lock: windowsession.LockLocked,
		},
	}
	if err := sink.Record(record); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var stored SessionRecord
	if err := json.NewDecoder(bufio.NewReader(file)).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.Event.Kind != windowsession.EventLock || stored.Console == nil || stored.Console.Lock != windowsession.LockLocked {
		t.Fatalf("stored record = %+v", stored)
	}
}
