//go:build !windows

package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	sessionLockTimeout    = 5 * time.Second
	sessionInspectTimeout = time.Second
)

type commandExecutor func(context.Context, string, ...string) ([]byte, error)

// Logind uses loginctl, systemd's command-line client for logind's D-Bus API.
// Commands are executed directly without a shell.
type Logind struct {
	path               string
	sessionNamespaceID string
	executeCommand     commandExecutor
}

// NewLogind creates a logind-backed session manager.
func NewLogind(loginctlPath string) (*Logind, error) {
	runtimeDirectory := os.Getenv("RUNTIME_DIRECTORY")
	if runtimeDirectory == "" {
		runtimeDirectory = filepath.Join(os.TempDir(), fmt.Sprintf("tempo-agent-%d", os.Getuid()))
	}
	sessionNamespaceID, err := loadOrCreateSessionNamespace(runtimeDirectory)
	if err != nil {
		return nil, err
	}
	return newLogind(loginctlPath, sessionNamespaceID)
}

func newLogind(loginctlPath, sessionNamespaceID string) (*Logind, error) {
	if loginctlPath == "" {
		return nil, errors.New("loginctl path cannot be empty")
	}
	if sessionNamespaceID == "" || strings.ContainsAny(sessionNamespaceID, " \t\r\n/@") {
		return nil, errors.New("session namespace identifier is invalid")
	}
	return &Logind{
		path:               loginctlPath,
		sessionNamespaceID: sessionNamespaceID,
		executeCommand:     executeCommand,
	}, nil
}

// Sessions returns all logind sessions owned by username. Detailed properties
// are requested only for matching accounts to keep each polling cycle small.
func (l *Logind) Sessions(ctx context.Context, username string) ([]Session, error) {
	output, err := exec.CommandContext(ctx, l.path, "list-sessions", "--no-legend", "--no-pager").CombinedOutput()
	if err != nil {
		return nil, commandError("list logind sessions", output, err)
	}

	var sessions []Session
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[2] != username {
			continue
		}
		id := fields[0]
		if err := validateSessionID(id); err != nil {
			return nil, err
		}
		details, err := exec.CommandContext(ctx, l.path, "show-session", id, "--no-pager",
			"--property=Name", "--property=Remote", "--property=Type",
			"--property=Class", "--property=State").CombinedOutput()
		if err != nil {
			return nil, commandError("inspect logind session "+id, details, err)
		}
		session, err := parseProperties(id, details)
		if err != nil {
			return nil, err
		}
		if session.User == username {
			session.AuthorizationID = l.sessionNamespaceID + "_" + session.ID
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func loadOrCreateSessionNamespace(runtimeDirectory string) (string, error) {
	if runtimeDirectory == "" {
		return "", errors.New("runtime directory is required")
	}
	if err := os.MkdirAll(runtimeDirectory, 0o700); err != nil {
		return "", fmt.Errorf("create agent runtime directory: %w", err)
	}
	path := filepath.Join(runtimeDirectory, "session-namespace-id")
	if stored, err := readSessionNamespace(path); err == nil {
		return stored, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate session namespace: %w", err)
	}
	identifier := hex.EncodeToString(randomBytes)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return readSessionNamespace(path)
	}
	if err != nil {
		return "", fmt.Errorf("create session namespace: %w", err)
	}
	writeSucceeded := false
	defer func() {
		_ = file.Close()
		if !writeSucceeded {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.WriteString(identifier + "\n"); err != nil {
		return "", fmt.Errorf("write session namespace: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync session namespace: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close session namespace: %w", err)
	}
	writeSucceeded = true
	return identifier, nil
}

func readSessionNamespace(path string) (string, error) {
	information, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !information.Mode().IsRegular() || information.Mode().Perm()&0o077 != 0 {
		return "", errors.New("stored session namespace has unsafe permissions")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	identifier := strings.TrimSpace(string(contents))
	decoded, err := hex.DecodeString(identifier)
	if err != nil || len(decoded) != 16 {
		return "", errors.New("stored session namespace is invalid")
	}
	return identifier, nil
}

// Lock asks logind to lock the graphical session without closing applications.
func (l *Logind) Lock(ctx context.Context, current Session) error {
	if err := validateSessionID(current.ID); err != nil {
		return err
	}
	lockContext, cancel := context.WithTimeout(ctx, sessionLockTimeout)
	defer cancel()
	output, err := l.executeCommand(lockContext, l.path, "lock-session", current.ID)
	if err == nil {
		return nil
	}
	return commandError("lock session "+current.ID, output, err)
}

// Unlock asks logind to release the graphical lock without ending the session.
func (l *Logind) Unlock(ctx context.Context, current Session) error {
	if err := validateSessionID(current.ID); err != nil {
		return err
	}
	unlockContext, cancel := context.WithTimeout(ctx, sessionLockTimeout)
	defer cancel()
	output, err := l.executeCommand(unlockContext, l.path, "unlock-session", current.ID)
	if err == nil {
		return nil
	}
	return commandError("unlock session "+current.ID, output, err)
}

// IsLocked reports logind's confirmed lock state for the session.
func (l *Logind) IsLocked(ctx context.Context, current Session) (bool, error) {
	if err := validateSessionID(current.ID); err != nil {
		return false, err
	}
	inspectContext, cancel := context.WithTimeout(ctx, sessionInspectTimeout)
	defer cancel()
	output, err := l.executeCommand(inspectContext, l.path, "show-session", current.ID,
		"--no-pager", "--property=LockedHint", "--value")
	if err != nil {
		return false, commandError("inspect session lock "+current.ID, output, err)
	}
	return parseLogindBool(strings.TrimSpace(string(output)))
}

func executeCommand(ctx context.Context, commandPath string, arguments ...string) ([]byte, error) {
	return exec.CommandContext(ctx, commandPath, arguments...).CombinedOutput()
}

func parseProperties(id string, output []byte) (Session, error) {
	values := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			values[key] = value
		}
	}
	remote, err := parseLogindBool(values["Remote"])
	if err != nil {
		return Session{}, fmt.Errorf("session %s has invalid Remote property %q", id, values["Remote"])
	}
	if values["Name"] == "" || values["Type"] == "" || values["Class"] == "" || values["State"] == "" {
		return Session{}, fmt.Errorf("session %s has incomplete logind properties", id)
	}
	return Session{
		ID: id, User: values["Name"], Type: values["Type"], Class: values["Class"],
		State: values["State"], Remote: remote,
	}, nil
}

func parseLogindBool(value string) (bool, error) {
	switch value {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	default:
		return strconv.ParseBool(value)
	}
}

func validateSessionID(id string) error {
	if id == "" || strings.HasPrefix(id, "-") {
		return fmt.Errorf("invalid logind session ID %q", id)
	}
	for _, character := range id {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_' && character != '-' && character != '.' {
			return fmt.Errorf("invalid logind session ID %q", id)
		}
	}
	return nil
}

func commandError(action string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, message)
}
