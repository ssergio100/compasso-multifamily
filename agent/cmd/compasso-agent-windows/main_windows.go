//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ssergio100/compasso/agent/alert"
	"github.com/ssergio100/compasso/agent/daemon"
	"github.com/ssergio100/compasso/agent/localauth"
	"github.com/ssergio100/compasso/agent/session"
	"github.com/ssergio100/compasso/agent/storage"
	"github.com/ssergio100/compasso/agent/syncclient"
	"github.com/ssergio100/compasso/agent/windowsipc"
	"github.com/ssergio100/compasso/agent/windowsservice"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	serviceName        = "CompassoAgent"
	serviceDisplayName = "Compasso Agent"
	serviceDescription = "Aplica as regras de tempo e sincroniza este computador com o Compasso."

	tickInterval       = time.Second
	checkpointInterval = 5 * time.Second
	httpTimeout        = 8 * time.Second
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "compasso-agent:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return svc.Run(serviceName, serviceHandler{})
	}
	if len(arguments) == 2 && strings.EqualFold(arguments[0], "inspect-session") {
		return inspectSession(arguments[1])
	}
	if len(arguments) == 2 && strings.EqualFold(arguments[0], "lock-session") {
		return lockSession(arguments[1])
	}
	if len(arguments) == 1 && strings.EqualFold(arguments[0], "inspect-state") {
		return inspectState()
	}
	if len(arguments) == 4 && strings.EqualFold(arguments[0], "configure") {
		return configure(arguments[1], arguments[2], arguments[3])
	}
	if len(arguments) != 1 {
		return errors.New("use: compasso-agent [install|uninstall|start|stop|console|service|configure <server-url> <device-id> <SID>]|inspect-session <SID>|lock-session <SID>")
	}
	switch strings.ToLower(arguments[0]) {
	case "install":
		return installService()
	case "uninstall":
		return uninstallService()
	case "start":
		return startService()
	case "stop":
		return stopService()
	case "console":
		return runConsole()
	case "service":
		return svc.Run(serviceName, serviceHandler{})
	default:
		return fmt.Errorf("unknown command %q", arguments[0])
	}
}

func inspectSession(controlledSID string) error {
	manager, err := session.NewWindows()
	if err != nil {
		return err
	}
	sessions, err := manager.Sessions(context.Background(), controlledSID)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(sessions)
}

func lockSession(controlledSID string) error {
	manager, err := session.NewWindows()
	if err != nil {
		return err
	}
	sessions, err := manager.Sessions(context.Background(), controlledSID)
	if err != nil {
		return err
	}
	if len(sessions) != 1 {
		return fmt.Errorf("expected one local console session for the controlled SID, found %d", len(sessions))
	}
	if sessions[0].Locked {
		return nil
	}
	return manager.Lock(context.Background(), sessions[0])
}

func inspectState() error {
	ctx := context.Background()
	databasePath, err := defaultDatabasePath()
	if err != nil {
		return err
	}
	store, err := storage.Open(ctx, databasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	report := map[string]any{"database": databasePath}
	policy, err := store.CurrentPolicy()
	if err != nil {
		report["policy_error"] = err.Error()
	} else {
		quota := map[string]int64{}
		for index, allowed := range policy.WeeklyQuota {
			quota[time.Weekday(index).String()] = int64(allowed / time.Second)
		}
		report["policy"] = map[string]any{
			"revision": policy.Revision, "monitoring_paused": policy.MonitoringPaused,
			"manual_block": policy.ManualBlock, "warning_minutes": policy.WarningMinutes,
			"weekly_quota_seconds": quota, "routines": len(policy.Routines),
		}
	}
	for _, offset := range []int{0, -1} {
		localDate := time.Now().AddDate(0, 0, offset).Format("2006-01-02")
		usage, err := store.LoadDailyUsage(ctx, localDate)
		if err != nil {
			report["usage_error_"+localDate] = err.Error()
			continue
		}
		bonus, err := store.TotalBonusSeconds(ctx, localDate)
		if err != nil {
			report["bonus_error_"+localDate] = err.Error()
			continue
		}
		report["usage_"+localDate] = map[string]any{
			"seconds_used": usage.SecondsUsed, "bonus_seconds": bonus,
		}
	}
	if sessionState, ok := store.CurrentConfirmedSessionState(); ok {
		report["confirmed_session"] = map[string]any{
			"revision": sessionState.Revision, "session_id": sessionState.SessionID,
			"local_date": sessionState.LocalDate, "usage_seconds": sessionState.UsageSeconds,
			"remaining_seconds": sessionState.RemainingSeconds,
			"confirmed_at":      sessionState.ConfirmedAt,
		}
	} else {
		report["confirmed_session"] = nil
	}
	pending, err := store.PendingEvents(ctx, 10)
	if err != nil {
		report["pending_events_error"] = err.Error()
	} else {
		kinds := map[string]int{}
		for _, event := range pending {
			kinds[event.Kind]++
		}
		report["pending_events"] = kinds
	}
	commands, err := store.AppliedCommandIDs(ctx, 10)
	if err != nil {
		report["applied_commands_error"] = err.Error()
	} else {
		report["applied_commands"] = commands
	}

	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

func configure(serverURL, deviceID, controlledSID string) error {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read device token from standard input: %w", err)
	}
	path, err := defaultConfigurationPath()
	if err != nil {
		return err
	}
	if err := windowsservice.SaveConfiguration(path, windowsservice.Configuration{
		ServerURL: serverURL, DeviceID: deviceID,
		DeviceToken: strings.TrimSpace(string(raw)), ControlledUserSID: controlledSID,
	}); err != nil {
		return err
	}
	public, _ := loadPublicConfiguration()
	encoded, err := json.MarshalIndent(public, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("configuration stored path=%s\n%s\n", path, encoded)
	return nil
}

func defaultConfigurationPath() (string, error) {
	return windowsservice.DefaultConfigurationPath(os.Getenv("ProgramData"))
}

func defaultDatabasePath() (string, error) {
	return windowsservice.DefaultDatabasePath(os.Getenv("ProgramData"))
}

func loadPublicConfiguration() (windowsservice.PublicConfiguration, error) {
	path, err := defaultConfigurationPath()
	if err != nil {
		return windowsservice.PublicConfiguration{}, err
	}
	settings, err := windowsservice.LoadConfiguration(path)
	if err != nil {
		return windowsservice.PublicConfiguration{}, err
	}
	return settings.Public(), nil
}

type silentNotifier struct{}

func (silentNotifier) Notify(context.Context, alert.Alert) error { return nil }

// configurationSource publishes only the settings that are safe to show. The
// device token is never exposed through the pipe.
type configurationSource struct {
	path string
}

func (source configurationSource) PublicConfiguration() (windowsipc.PublicSettings, bool) {
	settings, err := windowsservice.LoadConfiguration(source.path)
	if err != nil {
		return windowsipc.PublicSettings{}, false
	}
	public := settings.Public()
func (source configurationSource) UpdatePublicConfiguration(request windowsipc.UpdatePublicConfigurationRequest) error {
	settings, err := windowsservice.LoadConfiguration(source.path)
	if err != nil {
		return err
	}
	if request.ServerURL != "" {
		settings.ServerURL = request.ServerURL
	}
	if request.DeviceID != "" {
		settings.DeviceID = request.DeviceID
	}
	if request.ControlledUserSID != "" {
		settings.ControlledUserSID = request.ControlledUserSID
	}
	if request.SetTokenWhenPresent && request.DeviceToken != "" {
		settings.DeviceToken = request.DeviceToken
	}
	return windowsservice.SaveConfiguration(source.path, settings)
}
func runAgent(ctx context.Context, logger *log.Logger) error {
	configPath, err := defaultConfigurationPath()
	if err != nil {
		return err
	}
	settings, err := windowsservice.LoadConfiguration(configPath)
	if err != nil {
		return err
	}
	databasePath, err := defaultDatabasePath()
	if err != nil {
		return err
	}
	store, err := storage.Open(ctx, databasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	enrollment, err := store.BindEnrollment(ctx, settings.ServerURL, settings.DeviceID, settings.DeviceToken, true)
	if err != nil {
		return err
	}
	if enrollment.StateReset {
		logger.Printf("previous enrollment state cleared before initial synchronization")
	}

	sessions, err := session.NewWindows()
	if err != nil {
		return err
	}
	policyDaemon, err := daemon.New(store, sessions, settings.ControlledUserSID, checkpointInterval)
	if err != nil {
		return err
	}
	policyDaemon.SetAlertNotifier(silentNotifier{})
	logger.Printf("starting controlled_user_sid=%s database=%s", settings.ControlledUserSID, databasePath)

	synchronizer, err := syncclient.New(store, &http.Client{Timeout: httpTimeout}, syncclient.Config{
		ServerURL: settings.ServerURL, DeviceID: settings.DeviceID,
		DeviceToken: settings.DeviceToken, InstallationID: enrollment.InstallationID,
		HeartbeatInterval: syncclient.DefaultHeartbeatInterval,
		AttemptTimeout:    httpTimeout,
	})
	if err != nil {
		return err
	}
	localBonus, err := localauth.NewService(store)
	if err != nil {
		return err
	}
	ipcService := windowsipc.NewService(localBonus, synchronizer, configurationSource{path: configPath})
	go serveLocalInterface(ctx, ipcService, settings.ControlledUserSID, logger)
	logger.Printf("local interface ready pipe=%s", windowsipc.PipeName)

	policyDaemon.SetSynchronizationSource(synchronizer)
	go func() {
		if err := synchronizer.Run(ctx, logger); err != nil {
			logger.Printf("synchronization stopped: %v", err)
		}
	}()
	logger.Printf("synchronization enabled server=%s device_id=%s", settings.ServerURL, settings.DeviceID)
	return policyDaemon.Run(ctx, tickInterval, logger)
}

// localInterfaceRetryDelay is how long the agent waits before rebuilding the
// named pipe after the interface stopped unexpectedly.
const localInterfaceRetryDelay = 5 * time.Second

// serveLocalInterface keeps the named pipe available for the lifetime of the
// agent. Enforcement must keep running when the interface is down, so a failure
// here is retried instead of ending the service: a running agent with no pipe
// would leave the screen reporting the service as unavailable with no way back
// other than a manual restart.
func serveLocalInterface(ctx context.Context, service *windowsipc.Service, controlledUserSID string, logger *log.Logger) {
	for {
		listener, err := windowsipc.Listen(controlledUserSID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Printf("local interface unavailable: %v", err)
			if !sleepContext(ctx, localInterfaceRetryDelay) {
				return
			}
			continue
		}
		err = service.Serve(ctx, listener)
		if ctx.Err() != nil {
			return
		}
		logger.Printf("local interface stopped: %v", err)
		if !sleepContext(ctx, localInterfaceRetryDelay) {
			return
		}
	}
}

// sleepContext waits for the delay and reports whether the wait completed instead
// of the context being cancelled.
func sleepContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

type serviceHandler struct{}

func (serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	statuses <- svc.Status{State: svc.StartPending}
	stateStore, err := serviceStateStore()
	if err != nil || stateStore.Write(windowsservice.StateRunning) != nil {
		return true, 1
	}
	defer stateStore.Write(windowsservice.StateStopped)

	logger := log.New(os.Stdout, "compasso-agent: ", log.LstdFlags|log.LUTC)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentFailed := make(chan error, 1)
	go func() {
		if err := runAgent(ctx, logger); err != nil && ctx.Err() == nil {
			agentFailed <- err
		}
		close(agentFailed)
	}()

	accepted := svc.AcceptStop | svc.AcceptShutdown
	statuses <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case err := <-agentFailed:
			if err != nil {
				logger.Printf("fatal: %v", err)
				return true, 1
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				statuses <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				statuses <- svc.Status{State: svc.StopPending}
				cancel()
				return false, 0
			}
		}
	}
}

func serviceStateStore() (windowsservice.StateStore, error) {
	path, err := windowsservice.DefaultStatePath(os.Getenv("ProgramData"))
	if err != nil {
		return windowsservice.StateStore{}, err
	}
	return windowsservice.StateStore{Path: path}, nil
}

func runConsole() error {
	stateStore, err := serviceStateStore()
	if err != nil {
		return err
	}
	if err := stateStore.Write(windowsservice.StateRunning); err != nil {
		return err
	}
	defer stateStore.Write(windowsservice.StateStopped)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Println("Compasso Agent em execução. Pressione Ctrl+C para encerrar.")
	if err := runAgent(ctx, log.New(os.Stdout, "compasso-agent: ", log.LstdFlags|log.LUTC)); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func installService() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate service executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("resolve service executable: %w", err)
	}
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Service Control Manager: %w", err)
	}
	defer manager.Disconnect()

	configuration := mgr.Config{
		StartType:        mgr.StartAutomatic,
		ErrorControl:     mgr.ErrorNormal,
		DisplayName:      serviceDisplayName,
		Description:      serviceDescription,
		DelayedAutoStart: true,
	}
	service, err := manager.OpenService(serviceName)
	if err == nil {
		defer service.Close()
		existing, err := service.Config()
		if err != nil {
			return fmt.Errorf("read Windows service configuration: %w", err)
		}
		existing.BinaryPathName = fmt.Sprintf("\"%s\" service", executable)
		existing.StartType = configuration.StartType
		existing.ErrorControl = configuration.ErrorControl
		existing.DisplayName = configuration.DisplayName
		existing.Description = configuration.Description
		existing.DelayedAutoStart = configuration.DelayedAutoStart
		if err := service.UpdateConfig(existing); err != nil {
			return fmt.Errorf("update Windows service: %w", err)
		}
		return configureRecovery(service)
	}
	if !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return fmt.Errorf("inspect Windows service: %w", err)
	}
	service, err = manager.CreateService(serviceName, executable, configuration, "service")
	if err != nil {
		return fmt.Errorf("create Windows service: %w", err)
	}
	defer service.Close()
	return configureRecovery(service)
}

func configureRecovery(service *mgr.Service) error {
	if err := service.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 24*60*60); err != nil {
		return fmt.Errorf("configure service recovery: %w", err)
	}
	return service.SetRecoveryActionsOnNonCrashFailures(true)
}

func uninstallService() error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Service Control Manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(serviceName)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open Windows service: %w", err)
	}
	defer service.Close()
	if err := stopOpenedService(service); err != nil {
		return err
	}
	if err := service.Delete(); err != nil {
		return fmt.Errorf("delete Windows service: %w", err)
	}
	return nil
}

func startService() error {
	service, closeService, err := openService()
	if err != nil {
		return err
	}
	defer closeService()
	status, err := service.Query()
	if err != nil {
		return fmt.Errorf("query Windows service: %w", err)
	}
	if status.State == svc.Running {
		return nil
	}
	if err := service.Start(); err != nil {
		return fmt.Errorf("start Windows service: %w", err)
	}
	return waitForServiceState(service, svc.Running, 15*time.Second)
}

func stopService() error {
	service, closeService, err := openService()
	if err != nil {
		return err
	}
	defer closeService()
	return stopOpenedService(service)
}

func stopOpenedService(service *mgr.Service) error {
	status, err := service.Query()
	if err != nil {
		return fmt.Errorf("query Windows service: %w", err)
	}
	if status.State == svc.Stopped {
		return nil
	}
	if _, err := service.Control(svc.Stop); err != nil {
		return fmt.Errorf("stop Windows service: %w", err)
	}
	return waitForServiceState(service, svc.Stopped, 15*time.Second)
}

func openService() (*mgr.Service, func(), error) {
	manager, err := mgr.Connect()
	if err != nil {
		return nil, func() {}, fmt.Errorf("connect to Service Control Manager: %w", err)
	}
	service, err := manager.OpenService(serviceName)
	if err != nil {
		manager.Disconnect()
		return nil, func() {}, fmt.Errorf("open Windows service: %w", err)
	}
	return service, func() {
		service.Close()
		manager.Disconnect()
	}, nil
}

func waitForServiceState(service *mgr.Service, wanted svc.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return fmt.Errorf("query Windows service: %w", err)
		}
		if status.State == wanted {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("Windows service did not reach state %d within %s", wanted, timeout)
}
