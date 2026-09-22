//go:build windows
// +build windows

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	windowsession "github.com/ssergio100/compasso/agent/platform/windows/session"
)

func main() {
	controlledSID := flag.String("controlled-sid", "", "SID selected as the controlled Windows account")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observer := windowsession.NewObserver()
	observedConsole, err := observer.ActiveConsole(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "inspect Windows console session: %v\n", err)
		os.Exit(1)
	}
	var observedLock *windowsession.LockObservation
	var lockError string
	if observedConsole != nil {
		lock, lockErr := observer.SessionLock(ctx, observedConsole.SessionID)
		if lockErr != nil {
			lockError = lockErr.Error()
		} else {
			observedLock = &lock
		}
	}
	var controlledActiveConsole *windowsession.Snapshot
	if observedConsole != nil && observedConsole.IsControlledActiveConsole(*controlledSID) {
		controlledActiveConsole = observedConsole
	}
	result := struct {
		ControlledSID           string                         `json:"controlled_sid,omitempty"`
		ControlledActiveConsole *windowsession.Snapshot        `json:"controlled_active_console,omitempty"`
		ObservedConsole         *windowsession.Snapshot        `json:"observed_console,omitempty"`
		ObservedLock            *windowsession.LockObservation `json:"observed_lock,omitempty"`
		LockError               string                         `json:"lock_error,omitempty"`
	}{
		ControlledSID: *controlledSID, ControlledActiveConsole: controlledActiveConsole,
		ObservedConsole: observedConsole, ObservedLock: observedLock, LockError: lockError,
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "encode Windows sessions: %v\n", err)
		os.Exit(1)
	}
}
