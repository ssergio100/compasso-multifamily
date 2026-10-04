//go:build windows

package alert

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
)

func TestWindowsNotifierStartsAlertProcessInActiveSession(t *testing.T) {
	originalExecutable := windowsExecutable
	originalLauncher := launchAlertProcess
	t.Cleanup(func() {
		windowsExecutable = originalExecutable
		launchAlertProcess = originalLauncher
	})
	windowsExecutable = func() (string, error) { return `C:\Program Files\Compasso\CompassoAgent.exe`, nil }

	var gotSessionID uint32
	var gotExecutable, gotPayload string
	launchAlertProcess = func(sessionID uint32, executablePath, payload string) error {
		gotSessionID, gotExecutable, gotPayload = sessionID, executablePath, payload
		return nil
	}
	notifier := &WindowsNotifier{activeSessionID: func() uint32 { return 7 }}

	err := notifier.Notify(context.Background(), Alert{
		Title: "O tempo de hoje termina em 5 minutos",
		Body:  "O texto longo não deve substituir o aviso direto.",
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(gotPayload)
	if err != nil {
		t.Fatal(err)
	}
	if gotSessionID != 7 || gotExecutable != `C:\Program Files\Compasso\CompassoAgent.exe` ||
		string(decoded) != "O tempo de hoje termina em 5 minutos" {
		t.Fatalf("alert launch = session %d executable %q message %q", gotSessionID, gotExecutable, decoded)
	}
}

func TestWindowsNotifierRejectsMissingActiveSession(t *testing.T) {
	originalLauncher := launchAlertProcess
	t.Cleanup(func() { launchAlertProcess = originalLauncher })
	called := false
	launchAlertProcess = func(uint32, string, string) error {
		called = true
		return nil
	}
	notifier := &WindowsNotifier{activeSessionID: func() uint32 { return windowsInvalidSessionID }}
	if err := notifier.Notify(context.Background(), Alert{Title: "Resta 1 minuto"}); err == nil {
		t.Fatal("missing active session was accepted")
	}
	if called {
		t.Fatal("alert process was started without an active session")
	}
}

func TestWindowsNotifierReportsExecutableLookupFailure(t *testing.T) {
	originalExecutable := windowsExecutable
	t.Cleanup(func() { windowsExecutable = originalExecutable })
	windowsExecutable = func() (string, error) { return "", errors.New("lookup failed") }
	notifier := &WindowsNotifier{activeSessionID: func() uint32 { return 3 }}
	if err := notifier.Notify(context.Background(), Alert{Title: "Resta 1 minuto"}); err == nil {
		t.Fatal("executable lookup failure was ignored")
	}
}

func TestShowWindowsAlertPlaysSoundBeforeWaitingForConfirmation(t *testing.T) {
	originalSound := playAlertSound
	originalMessage := showAlertMessage
	t.Cleanup(func() {
		playAlertSound = originalSound
		showAlertMessage = originalMessage
	})
	steps := make([]string, 0, 2)
	playAlertSound = func() error {
		steps = append(steps, "sound")
		return nil
	}
	showAlertMessage = func(message string) error {
		steps = append(steps, "message:"+message)
		return nil
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("Resta 1 minuto"))
	if err := ShowWindowsAlert(payload); err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0] != "sound" || steps[1] != "message:Resta 1 minuto" {
		t.Fatalf("presentation steps = %v", steps)
	}
}

func TestShowWindowsAlertStillDisplaysWhenSoundFails(t *testing.T) {
	originalSound := playAlertSound
	originalMessage := showAlertMessage
	t.Cleanup(func() {
		playAlertSound = originalSound
		showAlertMessage = originalMessage
	})
	playAlertSound = func() error { return errors.New("sound failed") }
	displayed := false
	showAlertMessage = func(string) error {
		displayed = true
		return nil
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("Resta 1 minuto"))
	if err := ShowWindowsAlert(payload); err == nil {
		t.Fatal("sound failure was not reported")
	}
	if !displayed {
		t.Fatal("visual alert was skipped after sound failure")
	}
}
