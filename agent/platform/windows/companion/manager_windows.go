//go:build windows
// +build windows

package companion

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ssergio100/compasso/agent/alert"
	"github.com/ssergio100/compasso/agent/platform/windows/ipc"
	windowsession "github.com/ssergio100/compasso/agent/platform/windows/session"
	"golang.org/x/sys/windows"
)

const (
	defaultConnectTimeout = 10 * time.Second
	defaultStopTimeout    = 3 * time.Second
	waitTimeoutResult     = 258
)

type consoleObserver interface {
	ActiveConsole(context.Context) (*windowsession.Snapshot, error)
}

type ManagerConfig struct {
	ExecutablePath string
	PipeName       string
	ControlledSID  string
	ConnectTimeout time.Duration
	StopTimeout    time.Duration
}

// Manager keeps exactly one companion in the controlled physical console.
// It owns the process handle, validates the pipe client PID and restarts an
// unexpectedly terminated process with bounded backoff.
type Manager struct {
	observer consoleObserver
	config   ManagerConfig
	sink     LifecycleSink

	rootCtx    context.Context
	rootCancel context.CancelFunc
	mu         sync.Mutex
	stopped    bool
	sessionID  uint32
	cancel     context.CancelFunc
	done       chan struct{}
	connection *ipc.Connection
}

// RequestLock asks the authenticated companion for LockWorkStation. A
// successful response only means the call was accepted; WTS_SESSION_LOCK is
// still required before policy can consider the screen locked.
func (m *Manager) RequestLock(ctx context.Context, sessionID uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	connection := m.connection
	currentSession := m.sessionID
	stopped := m.stopped
	m.mu.Unlock()
	if stopped {
		return errors.New("companion manager is stopped")
	}
	if currentSession != sessionID || connection == nil {
		return errors.New("companion is not connected to the requested session")
	}
	request := ipc.Request{
		Version: ipc.ProtocolVersion,
		ID:      fmt.Sprintf("lock-%d-%d", sessionID, time.Now().UnixNano()),
		Command: ipc.CommandLock,
	}
	response, err := connection.Exchange(request)
	m.recordCommand(request, response, err)
	if err != nil {
		return err
	}
	if !response.Accepted {
		return fmt.Errorf("companion rejected lock request: %s", response.Error)
	}
	return nil
}

// Notify displays an agent alert through the authenticated companion running
// in the controlled interactive session.
func (m *Manager) Notify(ctx context.Context, scheduled alert.Alert) error {
	if scheduled.Title == "" || scheduled.Body == "" {
		return errors.New("Windows notification contents are required")
	}
	m.mu.Lock()
	connection := m.connection
	sessionID := m.sessionID
	stopped := m.stopped
	m.mu.Unlock()
	if stopped {
		return errors.New("companion manager is stopped")
	}
	if sessionID == 0 || connection == nil {
		return errors.New("companion is not connected to an interactive session")
	}
	request := ipc.Request{
		Version: ipc.ProtocolVersion,
		ID:      fmt.Sprintf("notify-%d-%d", sessionID, time.Now().UnixNano()),
		Command: ipc.CommandNotify,
		Title:   scheduled.Title,
		Body:    scheduled.Body,
	}
	response, err := connection.Exchange(request)
	m.recordCommand(request, response, err)
	if err != nil {
		return err
	}
	if !response.Accepted {
		return fmt.Errorf("companion rejected notification: %s", response.Error)
	}
	return nil
}

func NewManager(observer consoleObserver, config ManagerConfig, sink LifecycleSink) (*Manager, error) {
	if observer == nil || sink == nil {
		return nil, errors.New("companion observer and lifecycle sink are required")
	}
	if !filepath.IsAbs(config.ExecutablePath) || config.PipeName == "" {
		return nil, errors.New("absolute companion path and pipe name are required")
	}
	if _, err := windows.StringToSid(config.ControlledSID); err != nil {
		return nil, fmt.Errorf("parse controlled SID: %w", err)
	}
	if config.ConnectTimeout <= 0 {
		config.ConnectTimeout = defaultConnectTimeout
	}
	if config.StopTimeout <= 0 {
		config.StopTimeout = defaultStopTimeout
	}
	rootCtx, rootCancel := context.WithCancel(context.Background())
	return &Manager{observer: observer, config: config, sink: sink, rootCtx: rootCtx, rootCancel: rootCancel}, nil
}

func (m *Manager) Reconcile(ctx context.Context) error {
	console, err := m.observer.ActiveConsole(ctx)
	if err != nil {
		return err
	}
	wantedSession := uint32(0)
	if console != nil && console.IsControlledActiveConsole(m.config.ControlledSID) {
		wantedSession = console.SessionID
	}
	return m.switchSession(ctx, wantedSession)
}

func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	m.rootCancel()
	cancel, done := m.cancel, m.done
	m.cancel, m.done, m.sessionID, m.connection = nil, nil, 0, nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(m.config.StopTimeout + time.Second):
		}
	}
}

func (m *Manager) switchSession(ctx context.Context, wantedSession uint32) error {
	m.mu.Lock()
	if m.stopped || m.sessionID == wantedSession {
		m.mu.Unlock()
		return nil
	}
	oldCancel, oldDone := m.cancel, m.done
	m.cancel, m.done, m.sessionID, m.connection = nil, nil, 0, nil
	m.mu.Unlock()

	if oldCancel != nil {
		oldCancel()
	}
	if oldDone != nil {
		select {
		case <-oldDone:
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.config.StopTimeout + time.Second):
			return errors.New("previous companion supervisor did not stop")
		}
	}
	if wantedSession == 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return nil
	}
	sessionCtx, cancel := context.WithCancel(m.rootCtx)
	done := make(chan struct{})
	m.sessionID, m.cancel, m.done = wantedSession, cancel, done
	go func() {
		defer close(done)
		m.supervise(sessionCtx, wantedSession)
	}()
	return nil
}

func (m *Manager) supervise(ctx context.Context, sessionID uint32) {
	failures := 0
	for ctx.Err() == nil {
		runStarted := time.Now()
		err := m.runCompanion(ctx, sessionID)
		if ctx.Err() != nil {
			return
		}
		if time.Since(runStarted) >= 30*time.Second {
			failures = 0
		}
		failures++
		delay := restartDelay(failures)
		m.record(LifecycleEvent{Kind: LifecycleRestartScheduled, SessionID: sessionID, RetryAfter: delay, Error: errorText(err)})
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			m.record(LifecycleEvent{Kind: LifecycleStopped, SessionID: sessionID})
			return
		case <-timer.C:
		}
	}
}

func (m *Manager) runCompanion(ctx context.Context, sessionID uint32) error {
	server, err := ipc.Listen(m.config.PipeName, m.config.ControlledSID)
	if err != nil {
		m.record(LifecycleEvent{Kind: LifecycleError, SessionID: sessionID, Error: err.Error()})
		return err
	}
	process, err := launchInteractive(sessionID, m.config)
	if err != nil {
		server.Close()
		m.record(LifecycleEvent{Kind: LifecycleError, SessionID: sessionID, Error: err.Error()})
		return err
	}
	defer process.Close()
	m.record(LifecycleEvent{Kind: LifecycleStarted, SessionID: sessionID, PID: process.pid})

	type acceptResult struct {
		connection *ipc.Connection
		err        error
	}
	accepted := make(chan acceptResult, 1)
	go func() {
		connection, acceptErr := server.Accept(process.pid)
		accepted <- acceptResult{connection: connection, err: acceptErr}
	}()
	var connection *ipc.Connection
	select {
	case result := <-accepted:
		if result.err != nil {
			server.Close()
			process.Stop(m.config.StopTimeout)
			m.record(LifecycleEvent{Kind: LifecycleError, SessionID: sessionID, PID: process.pid, Error: result.err.Error()})
			return result.err
		}
		connection = result.connection
	case <-ctx.Done():
		server.Close()
		process.Stop(m.config.StopTimeout)
		return ctx.Err()
	case <-time.After(m.config.ConnectTimeout):
		server.Close()
		process.Stop(m.config.StopTimeout)
		err := errors.New("companion did not connect before timeout")
		m.record(LifecycleEvent{Kind: LifecycleError, SessionID: sessionID, PID: process.pid, Error: err.Error()})
		return err
	}
	defer connection.Close()
	m.record(LifecycleEvent{Kind: LifecycleConnected, SessionID: sessionID, PID: process.pid})
	m.mu.Lock()
	if !m.stopped && m.sessionID == sessionID {
		m.connection = connection
	}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.connection == connection {
			m.connection = nil
		}
		m.mu.Unlock()
	}()

	exited := make(chan processExit, 1)
	go func() { exited <- process.Wait() }()
	select {
	case exit := <-exited:
		m.record(LifecycleEvent{Kind: LifecycleExited, SessionID: sessionID, PID: process.pid, ExitCode: &exit.code, Error: errorText(exit.err)})
		if exit.err != nil {
			return exit.err
		}
		return fmt.Errorf("companion exited with code %d", exit.code)
	case <-ctx.Done():
		shutdownDone := make(chan error, 1)
		go func() {
			_, shutdownErr := connection.Exchange(ipc.Request{
				Version: ipc.ProtocolVersion,
				ID:      fmt.Sprintf("stop-%d", time.Now().UnixNano()),
				Command: ipc.CommandStop,
			})
			shutdownDone <- shutdownErr
		}()
		select {
		case <-shutdownDone:
		case <-time.After(time.Second):
		}
		connection.Close()
		exit := process.StopAndWait(exited, m.config.StopTimeout)
		m.record(LifecycleEvent{Kind: LifecycleStopped, SessionID: sessionID, PID: process.pid, ExitCode: &exit.code, Error: errorText(exit.err)})
		return ctx.Err()
	}
}

func (m *Manager) record(event LifecycleEvent) {
	event.ObservedAt = time.Now().UTC()
	_ = m.sink.RecordCompanionLifecycle(event)
}

func (m *Manager) recordCommand(request ipc.Request, response ipc.Response, commandErr error) {
	sink, ok := m.sink.(CommandSink)
	if !ok {
		return
	}
	record := CommandRecord{Request: request, RecordedAt: time.Now().UTC()}
	if commandErr != nil {
		record.Error = commandErr.Error()
	} else {
		record.Response = &response
	}
	_ = sink.RecordCompanionCommand(record)
}

type childProcess struct {
	handle windows.Handle
	pid    uint32
}

type processExit struct {
	code uint32
	err  error
}

func launchInteractive(sessionID uint32, config ManagerConfig) (*childProcess, error) {
	var token windows.Token
	if err := windows.WTSQueryUserToken(sessionID, &token); err != nil {
		return nil, fmt.Errorf("query session %d user token: %w", sessionID, err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read session %d token SID: %w", sessionID, err)
	}
	if !strings.EqualFold(user.User.Sid.String(), config.ControlledSID) {
		return nil, fmt.Errorf("session %d token SID %q is not controlled SID", sessionID, user.User.Sid.String())
	}

	var environment *uint16
	if err := windows.CreateEnvironmentBlock(&environment, token, false); err != nil {
		return nil, fmt.Errorf("create companion environment: %w", err)
	}
	defer windows.DestroyEnvironmentBlock(environment)
	application, err := windows.UTF16PtrFromString(config.ExecutablePath)
	if err != nil {
		return nil, fmt.Errorf("encode companion path: %w", err)
	}
	command := windows.EscapeArg(config.ExecutablePath) + " -pipe-name " + windows.EscapeArg(pipeLogicalName(config.PipeName))
	commandLine, err := windows.UTF16PtrFromString(command)
	if err != nil {
		return nil, fmt.Errorf("encode companion command line: %w", err)
	}
	desktop, _ := windows.UTF16PtrFromString(`winsta0\default`)
	currentDirectory, err := windows.UTF16PtrFromString(filepath.Dir(config.ExecutablePath))
	if err != nil {
		return nil, fmt.Errorf("encode companion directory: %w", err)
	}
	startup := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{})), Desktop: desktop}
	var information windows.ProcessInformation
	if err := windows.CreateProcessAsUser(
		token, application, commandLine, nil, nil, false,
		windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NO_WINDOW,
		environment, currentDirectory, &startup, &information,
	); err != nil {
		return nil, fmt.Errorf("create companion in session %d: %w", sessionID, err)
	}
	windows.CloseHandle(information.Thread)
	return &childProcess{handle: information.Process, pid: information.ProcessId}, nil
}

func pipeLogicalName(pipePath string) string {
	const prefix = `\\.\pipe\`
	if strings.HasPrefix(strings.ToLower(pipePath), strings.ToLower(prefix)) {
		return pipePath[len(prefix):]
	}
	return pipePath
}

func (p *childProcess) Wait() processExit {
	if _, err := windows.WaitForSingleObject(p.handle, windows.INFINITE); err != nil {
		return processExit{err: err}
	}
	var code uint32
	if err := windows.GetExitCodeProcess(p.handle, &code); err != nil {
		return processExit{err: err}
	}
	return processExit{code: code}
}

func (p *childProcess) Stop(timeout time.Duration) {
	waitMilliseconds := uint32(timeout / time.Millisecond)
	result, err := windows.WaitForSingleObject(p.handle, waitMilliseconds)
	if err == nil && result == windows.WAIT_OBJECT_0 {
		return
	}
	if err == nil && result == waitTimeoutResult {
		_ = windows.TerminateProcess(p.handle, 1)
		_, _ = windows.WaitForSingleObject(p.handle, uint32(time.Second/time.Millisecond))
	}
}

func (p *childProcess) StopAndWait(exited <-chan processExit, timeout time.Duration) processExit {
	select {
	case exit := <-exited:
		return exit
	case <-time.After(timeout):
		if err := windows.TerminateProcess(p.handle, 1); err != nil {
			return processExit{err: fmt.Errorf("terminate companion: %w", err)}
		}
		select {
		case exit := <-exited:
			return exit
		case <-time.After(time.Second):
			return processExit{err: errors.New("companion did not exit after termination")}
		}
	}
}

func (p *childProcess) Close() {
	if p != nil && p.handle != 0 && p.handle != windows.InvalidHandle {
		windows.CloseHandle(p.handle)
		p.handle = windows.InvalidHandle
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
