package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionNamespacePersistsInsideRuntimeDirectory(t *testing.T) {
	runtimeDirectory := t.TempDir()
	firstIdentifier, err := loadOrCreateSessionNamespace(runtimeDirectory)
	if err != nil {
		t.Fatal(err)
	}
	secondIdentifier, err := loadOrCreateSessionNamespace(runtimeDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if firstIdentifier != secondIdentifier || len(firstIdentifier) != 32 {
		t.Fatalf("session namespace was not preserved: first=%q second=%q", firstIdentifier, secondIdentifier)
	}
	information, err := os.Stat(filepath.Join(runtimeDirectory, "session-namespace-id"))
	if err != nil {
		t.Fatal(err)
	}
	if permissions := information.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("session namespace permissions=%#o, want 0600", permissions)
	}
}

func TestSessionNamespaceRejectsUnsafeStoredFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-namespace-id")
	if err := os.WriteFile(path, []byte("0123456789abcdef0123456789abcdef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSessionNamespace(path); err == nil {
		t.Fatal("unsafe session namespace permissions were accepted")
	}
}

func TestParsePropertiesAndGraphicalFilter(t *testing.T) {
	session, err := parseProperties("3", []byte("Name=child\nRemote=no\nType=wayland\nClass=user\nState=active\n"))
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != "3" || session.User != "child" || !session.IsLocalGraphical() {
		t.Fatalf("unexpected session: %+v", session)
	}

	nonGraphical := []Session{
		{Type: "tty", Class: "user", State: "active"},
		{Type: "x11", Class: "greeter", State: "active"},
		{Type: "wayland", Class: "user", State: "active", Remote: true},
		{Type: "x11", Class: "user", State: "closing"},
	}
	for _, candidate := range nonGraphical {
		if candidate.IsLocalGraphical() {
			t.Fatalf("session should not count as local graphical: %+v", candidate)
		}
	}
}

func TestParsePropertiesRejectsIncompleteOutput(t *testing.T) {
	if _, err := parseProperties("3", []byte("Name=child\nRemote=no\nType=wayland\n")); err == nil {
		t.Fatal("expected incomplete properties to fail")
	}
}

func TestBalanceAuthorizationIDFallsBackOnlyForAlternateManagers(t *testing.T) {
	current := Session{ID: "7", AuthorizationID: "runtime-namespace_7"}
	if current.BalanceAuthorizationID() != "runtime-namespace_7" {
		t.Fatalf("authorization id=%q", current.BalanceAuthorizationID())
	}
	current.AuthorizationID = ""
	if current.BalanceAuthorizationID() != "7" {
		t.Fatalf("fallback authorization id=%q", current.BalanceAuthorizationID())
	}
}

func TestLockUsesLogindWithoutEndingSession(t *testing.T) {
	manager, err := newLogind("/usr/bin/loginctl", "testnamespace")
	if err != nil {
		t.Fatal(err)
	}
	var commandPath string
	var commandArguments []string
	manager.executeCommand = func(_ context.Context, path string, arguments ...string) ([]byte, error) {
		commandPath = path
		commandArguments = append([]string(nil), arguments...)
		return nil, nil
	}
	current := Session{ID: "3", User: "child", Type: "wayland", Class: "user", State: "active"}
	if err := manager.Lock(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if commandPath != "/usr/bin/loginctl" || len(commandArguments) != 2 ||
		commandArguments[0] != "lock-session" || commandArguments[1] != "3" {
		t.Fatalf("command=%q arguments=%q", commandPath, commandArguments)
	}
}

func TestUnlockUsesLogindWithoutEndingSession(t *testing.T) {
	manager, err := newLogind("/usr/bin/loginctl", "testnamespace")
	if err != nil {
		t.Fatal(err)
	}
	var commandPath string
	var commandArguments []string
	manager.executeCommand = func(_ context.Context, path string, arguments ...string) ([]byte, error) {
		commandPath = path
		commandArguments = append([]string(nil), arguments...)
		return nil, nil
	}
	current := Session{ID: "3", User: "child", Type: "wayland", Class: "user", State: "active"}
	if err := manager.Unlock(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if commandPath != "/usr/bin/loginctl" || len(commandArguments) != 2 ||
		commandArguments[0] != "unlock-session" || commandArguments[1] != "3" {
		t.Fatalf("command=%q arguments=%q", commandPath, commandArguments)
	}
}

func TestIsLockedReadsLogindLockedHint(t *testing.T) {
	manager, err := newLogind("/usr/bin/loginctl", "testnamespace")
	if err != nil {
		t.Fatal(err)
	}
	manager.executeCommand = func(_ context.Context, _ string, arguments ...string) ([]byte, error) {
		if len(arguments) == 5 && arguments[0] == "show-session" && arguments[1] == "9" {
			return []byte("yes\n"), nil
		}
		return nil, errors.New("unexpected command")
	}
	current := Session{ID: "9", User: "child", Type: "x11", Class: "user", State: "active"}
	locked, err := manager.IsLocked(context.Background(), current)
	if err != nil || !locked {
		t.Fatalf("locked=%t err=%v", locked, err)
	}
}
