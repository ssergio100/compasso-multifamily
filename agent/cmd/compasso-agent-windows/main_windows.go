//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ssergio100/compasso/agent/session"
	"github.com/ssergio100/compasso/agent/windowsservice"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	serviceName        = "CompassoAgent"
	serviceDisplayName = "Compasso Agent"
	serviceDescription = "Aplica as regras de tempo e sincroniza este computador com o Compasso."
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
	if len(arguments) != 1 {
		return errors.New("use: compasso-agent [install|uninstall|start|stop|console|service|inspect-session <SID>|lock-session <SID>]")
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

type serviceHandler struct{}

func (serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	statuses <- svc.Status{State: svc.StartPending}
	stateStore, err := serviceStateStore()
	if err != nil || stateStore.Write(windowsservice.StateRunning) != nil {
		return true, 1
	}
	defer stateStore.Write(windowsservice.StateStopped)

	accepted := svc.AcceptStop | svc.AcceptShutdown
	statuses <- svc.Status{State: svc.Running, Accepts: accepted}
	for request := range requests {
		switch request.Cmd {
		case svc.Interrogate:
			statuses <- request.CurrentStatus
		case svc.Stop, svc.Shutdown:
			statuses <- svc.Status{State: svc.StopPending}
			return false, 0
		}
	}
	return false, 0
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
	<-ctx.Done()
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
