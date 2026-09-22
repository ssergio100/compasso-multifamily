//go:build windows
// +build windows

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ssergio100/compasso/agent/config"
	"github.com/ssergio100/compasso/agent/daemon"
	windowscompanion "github.com/ssergio100/compasso/agent/platform/windows/companion"
	"github.com/ssergio100/compasso/agent/platform/windows/ipc"
	windowsservice "github.com/ssergio100/compasso/agent/platform/windows/service"
	windowsession "github.com/ssergio100/compasso/agent/platform/windows/session"
	"github.com/ssergio100/compasso/agent/storage"
	"github.com/ssergio100/compasso/agent/syncclient"
	protocol "github.com/ssergio100/compasso/protocol/v1"
	"golang.org/x/sys/windows/svc"
)

type options struct {
	serviceName   string
	diagnostic    bool
	configPath    string
	statePath     string
	eventLog      string
	agentLog      string
	companionPath string
	companionPipe string
	controlledSID string
}

func main() {
	statePath := defaultStatePath()
	serviceName := flag.String("service-name", "CompassoAgent", "Windows SCM service name")
	diagnostic := flag.Bool("diagnostic", false, "run only the WTS/companion diagnostic service")
	configPath := flag.String("config", filepath.Join(statePath, "config.toml"), "agent configuration")
	eventLog := flag.String("event-log", "", "JSON Lines session and companion diagnostic file")
	agentLog := flag.String("agent-log", "", "agent runtime log file; defaults beside the configured database")
	companionPath := flag.String("companion-path", "", "absolute interactive companion path; derived beside this executable in configured mode")
	companionPipe := flag.String("companion-pipe-name", "CompassoAgent", "logical local companion pipe name")
	controlledSID := flag.String("controlled-sid", "", "controlled SID override used only by diagnostic mode")
	flag.Parse()

	err := run(options{
		serviceName: *serviceName, diagnostic: *diagnostic, configPath: *configPath, statePath: statePath, eventLog: *eventLog,
		agentLog: *agentLog, companionPath: *companionPath,
		companionPipe: *companionPipe, controlledSID: *controlledSID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Windows agent service: %v\n", err)
		os.Exit(1)
	}
}

func run(options options) error {
	if options.diagnostic {
		return runDiagnostic(options)
	}
	return runConfigured(options)
}

func runDiagnostic(options options) error {
	sink, err := windowsservice.NewJSONLinesSink(options.eventLog)
	if err != nil {
		return fmt.Errorf("open Windows session event log: %w", err)
	}
	defer sink.Close()

	observer := windowsession.NewObserver()
	handler := windowsservice.NewHandler(observer, sink)
	if options.companionPath != "" {
		manager, err := newCompanionManager(observer, sink, options.companionPath, options.companionPipe, options.controlledSID)
		if err != nil {
			return err
		}
		handler.WithSessionLifecycle(manager)
	}
	if err := svc.Run(options.serviceName, handler); err != nil {
		return fmt.Errorf("run Windows service %q: %w", options.serviceName, err)
	}
	return nil
}

func runConfigured(options options) error {
	settings, err := config.LoadWithDefaults(options.configPath, config.Defaults{
		DatabasePath: filepath.Join(options.statePath, "agent.db"),
	})
	if err != nil {
		return err
	}
	if !settings.SyncEnabled() {
		return errors.New("server enrollment is required for the configured Windows service")
	}
	if options.controlledSID != "" && !strings.EqualFold(options.controlledSID, settings.ControlledUser) {
		return errors.New("controlled SID flag does not match controlled_user in the configuration")
	}
	options.controlledSID = settings.ControlledUser
	if options.companionPath == "" {
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve Windows service executable: %w", err)
		}
		options.companionPath = filepath.Join(filepath.Dir(executable), "compasso-windows-companion.exe")
	}
	if !filepath.IsAbs(options.companionPath) {
		return errors.New("companion path must be absolute")
	}
	stateDirectory := filepath.Dir(settings.DatabasePath)
	if err := os.MkdirAll(stateDirectory, 0o700); err != nil {
		return fmt.Errorf("create Windows agent state directory: %w", err)
	}
	if options.eventLog == "" {
		options.eventLog = filepath.Join(stateDirectory, "session-events.jsonl")
	}
	if options.agentLog == "" {
		options.agentLog = filepath.Join(stateDirectory, "agent.log")
	}

	logFile, err := os.OpenFile(options.agentLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open Windows agent log: %w", err)
	}
	defer logFile.Close()
	logger := log.New(logFile, "compasso-agent: ", log.LstdFlags|log.LUTC)

	sink, err := windowsservice.NewJSONLinesSink(options.eventLog)
	if err != nil {
		return fmt.Errorf("open Windows session event log: %w", err)
	}
	defer sink.Close()

	ctx := context.Background()
	store, err := storage.Open(ctx, settings.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	enrollment, err := store.BindEnrollment(ctx, settings.ServerURL, settings.DeviceID, settings.DeviceToken, false)
	if err != nil {
		return err
	}
	if enrollment.StateReset {
		logger.Printf("previous unbound or mismatched enrollment state cleared")
	}

	observer := windowsession.NewObserver()
	locks := windowsession.NewLockTracker()
	companion, err := newCompanionManager(observer, sink, options.companionPath, options.companionPipe, settings.ControlledUser)
	if err != nil {
		return err
	}
	sessions, err := windowsession.NewManager(observer, locks, companion, settings.ControlledUser, enrollment.InstallationID)
	if err != nil {
		return err
	}
	policyDaemon, err := daemon.New(store, sessions, settings.ControlledUser, settings.CheckpointInterval)
	if err != nil {
		return err
	}
	synchronizer, err := syncclient.New(store, &http.Client{Timeout: settings.HTTPTimeout}, syncclient.Config{
		ServerURL: settings.ServerURL, DeviceID: settings.DeviceID,
		DeviceToken: settings.DeviceToken, InstallationID: enrollment.InstallationID,
		HeartbeatInterval: syncclient.DefaultHeartbeatInterval,
		AttemptTimeout:    settings.HTTPTimeout,
		Capabilities: []string{
			protocol.SessionLockCapability,
			protocol.UnlockAuthenticationCapability,
			protocol.LockPausesAccountingCapability,
		},
	})
	if err != nil {
		return err
	}
	policyDaemon.SetSynchronizationSource(synchronizer)
	policyDaemon.SetAlertNotifier(companion)
	policyDaemon.SetAccessReleaseNotifier(companion)
	runtime, err := windowsservice.NewAgentRuntime(policyDaemon, synchronizer, settings.TickInterval, logger)
	if err != nil {
		return err
	}
	handler := windowsservice.NewHandler(observer, sink).
		WithLockTracker(locks).
		WithSessionLifecycle(companion).
		WithRuntime(runtime)

	logger.Printf("starting controlled_sid=%s database=%s server=%s device_id=%s installation_id=%s",
		settings.ControlledUser, settings.DatabasePath, settings.ServerURL, settings.DeviceID, enrollment.InstallationID)
	if err := svc.Run(options.serviceName, handler); err != nil {
		return fmt.Errorf("run Windows service %q: %w", options.serviceName, err)
	}
	logger.Printf("service stopped cleanly")
	return nil
}

func defaultStatePath() string {
	programData := strings.TrimSpace(os.Getenv("ProgramData"))
	if programData == "" || !filepath.IsAbs(programData) {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, "Compasso", "Agent")
}

func newCompanionManager(observer *windowsession.Observer, sink *windowsservice.JSONLinesSink, executablePath, pipeName, controlledSID string) (*windowscompanion.Manager, error) {
	if controlledSID == "" {
		return nil, errors.New("controlled SID is required with companion lifecycle")
	}
	pipePath, err := ipc.PipePath(pipeName)
	if err != nil {
		return nil, fmt.Errorf("resolve Windows companion pipe: %w", err)
	}
	manager, err := windowscompanion.NewManager(observer, windowscompanion.ManagerConfig{
		ExecutablePath: executablePath,
		PipeName:       pipePath,
		ControlledSID:  controlledSID,
	}, sink)
	if err != nil {
		return nil, fmt.Errorf("configure Windows companion lifecycle: %w", err)
	}
	return manager, nil
}
