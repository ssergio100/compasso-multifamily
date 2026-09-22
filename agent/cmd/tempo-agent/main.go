package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ssergio100/compasso/agent/alert"
	"github.com/ssergio100/compasso/agent/config"
	"github.com/ssergio100/compasso/agent/daemon"
	"github.com/ssergio100/compasso/agent/localapi"
	"github.com/ssergio100/compasso/agent/localauth"
	"github.com/ssergio100/compasso/agent/session"
	"github.com/ssergio100/compasso/agent/storage"
	"github.com/ssergio100/compasso/agent/syncclient"
	"github.com/ssergio100/compasso/agent/syncstatus"
	protocol "github.com/ssergio100/compasso/protocol/v1"
)

const (
	setupMarkerPath         = "/etc/tempo-agent/setup-complete"
	setupMarkerPollInterval = 200 * time.Millisecond
)

func main() {
	configPath := flag.String("config", "/etc/tempo-agent/config.toml", "path to the agent configuration")
	validateConfigurationOnly := flag.Bool("check-config", false, "validate configuration and exit")
	flag.Parse()

	logger := log.New(os.Stdout, "tempo-agent: ", log.LstdFlags|log.LUTC)
	if *validateConfigurationOnly {
		if _, err := config.Load(*configPath); err != nil {
			logger.Printf("configuration invalid: %v", err)
			os.Exit(1)
		}
		logger.Printf("configuration valid")
		return
	}
	if err := run(*configPath, logger); err != nil {
		logger.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run(configPath string, logger *log.Logger) error {
	// Protect databases, WAL files and any future state created by the daemon,
	// including when it is started manually instead of through systemd.
	syscall.Umask(0o077)
	settings, err := config.Load(configPath)
	if err != nil {
		return err
	}
	// A database can survive package removal or an interrupted enrollment. It
	// must never become policy authority until this installation has complete
	// device credentials. In particular, do not apply an old offline policy.
	if !settings.SyncEnabled() {
		logger.Printf("agent not configured; policy enforcement disabled")
		return nil
	}
	setupConfirmedAtStartup, err := setupConfirmed(setupMarkerPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settings.DatabasePath), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	controlledAccount, err := user.Lookup(settings.ControlledUser)
	if err != nil {
		return fmt.Errorf("find controlled Linux account %q: %w", settings.ControlledUser, err)
	}
	if controlledAccount.Uid == "0" {
		return fmt.Errorf("controlled Linux account %q cannot be root", settings.ControlledUser)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	store, err := storage.Open(ctx, settings.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	enrollment, err := store.BindEnrollment(ctx, settings.ServerURL, settings.DeviceID, settings.DeviceToken, setupConfirmedAtStartup)
	if err != nil {
		return err
	}
	if enrollment.StateReset {
		logger.Printf("previous enrollment state cleared before initial synchronization")
	}
	logind, err := session.NewLogind(settings.LoginctlPath)
	if err != nil {
		return err
	}
	policyDaemon, err := daemon.New(store, logind, settings.ControlledUser, settings.CheckpointInterval)
	if err != nil {
		return err
	}
	desktopNotifier, err := alert.NewDesktopNotifier(settings.ControlledUser)
	if err != nil {
		return err
	}
	policyDaemon.SetAlertNotifier(desktopNotifier)
	logger.Printf("starting controlled_user=%s database=%s", settings.ControlledUser, settings.DatabasePath)
	synchronizer, err := syncclient.New(store, &http.Client{Timeout: settings.HTTPTimeout}, syncclient.Config{
		ServerURL: settings.ServerURL, DeviceID: settings.DeviceID,
		DeviceToken: settings.DeviceToken, InstallationID: enrollment.InstallationID,
		HeartbeatInterval: syncclient.DefaultHeartbeatInterval,
		AttemptTimeout:    settings.HTTPTimeout,
		Capabilities:      []string{protocol.SessionLockCapability},
	})
	if err != nil {
		return err
	}
	localBonus, err := localauth.NewService(store)
	if err != nil {
		return err
	}
	localServer, err := localapi.ExportSystem(localBonus, synchronizer)
	if err != nil {
		logger.Printf("local D-Bus API unavailable: %v", err)
	} else {
		defer localServer.Close()
		logger.Printf("local D-Bus API ready name=%s", localapi.BusName)
	}
	runtimeDirectory := os.Getenv("RUNTIME_DIRECTORY")
	if runtimeDirectory != "" {
		statusPath := filepath.Join(runtimeDirectory, "synchronization-status.json")
		_ = os.Remove(statusPath)
		synchronizer.SetStatusReporter(func(synchronizationError error) {
			report := syncstatus.Report{State: syncstatus.StateOnline}
			if synchronizationError != nil {
				report.State = syncstatus.StateOffline
				report.Detail = synchronizationError.Error()
			}
			if err := syncstatus.Write(statusPath, report); err != nil {
				logger.Printf("write synchronization status: %v", err)
			}
		})
	}
	policyDaemon.SetSynchronizationSource(synchronizer)
	go func() {
		if err := synchronizer.Run(ctx, logger); err != nil {
			logger.Printf("synchronization stopped: %v", err)
		}
	}()
	logger.Printf("synchronization enabled server=%s device_id=%s", settings.ServerURL, settings.DeviceID)
	if !setupConfirmedAtStartup {
		logger.Printf("awaiting setup confirmation; policy enforcement disabled")
		if !waitForSetupConfirmation(ctx, setupMarkerPath, setupMarkerPollInterval) {
			return nil
		}
		logger.Printf("setup confirmed; policy enforcement enabled")
	}
	return policyDaemon.Run(ctx, settings.TickInterval, logger)
}

func setupConfirmed(path string) (bool, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read setup confirmation: %w", err)
	}
	return strings.TrimSpace(string(contents)) == "configured", nil
}

func waitForSetupConfirmation(ctx context.Context, path string, pollInterval time.Duration) bool {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		confirmed, err := setupConfirmed(path)
		if err == nil && confirmed {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}
