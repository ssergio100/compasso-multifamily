//go:build !windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/ssergio100/compasso/agent/config"
	agentsetup "github.com/ssergio100/compasso/agent/setup"
	"github.com/ssergio100/compasso/agent/syncstatus"
)

const (
	configurationPath                  = "/etc/tempo-agent/config.toml"
	setupMarkerPath                    = "/etc/tempo-agent/setup-complete"
	synchronizationStatusPath          = "/run/tempo-agent/synchronization-status.json"
	systemctlPath                      = "/usr/bin/systemctl"
	synchronizationConfirmationTimeout = 30 * time.Second
)

func main() {
	if len(os.Args) != 1 {
		fatal(errors.New("unexpected positional arguments"))
	}

	settings, err := config.Load(configurationPath)
	if err != nil {
		fatal(err)
	}
	if os.Geteuid() != 0 {
		fatal(errors.New("administrative authorization is required"))
	}

	request, err := agentsetup.DecodeRequest(os.Stdin)
	if err != nil {
		fatal(err)
	}
	updatedSettings, err := request.Apply(settings)
	if err != nil {
		fatal(err)
	}
	if err := agentsetup.CheckReady(updatedSettings, lookupUID); err != nil {
		fatal(err)
	}
	originalConfiguration, err := os.ReadFile(configurationPath)
	if err != nil {
		fatal(fmt.Errorf("read current configuration: %w", err))
	}
	originalSetupMarker, markerWasPresent, err := readOptionalFile(setupMarkerPath)
	if err != nil {
		fatal(fmt.Errorf("read current setup marker: %w", err))
	}
	if err := os.Remove(setupMarkerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		fatal(fmt.Errorf("clear setup confirmation: %w", err))
	}
	if err := agentsetup.WriteFileAtomically(configurationPath, agentsetup.Render(updatedSettings), 0o600); err != nil {
		restoreSetupMarker(originalSetupMarker, markerWasPresent)
		fatal(err)
	}
	if err := startAgentService(); err != nil {
		_ = agentsetup.WriteFileAtomically(configurationPath, originalConfiguration, 0o600)
		restoreSetupMarker(originalSetupMarker, markerWasPresent)
		_ = exec.Command(systemctlPath, "disable", "--now", "tempo-agent.service").Run()
		fatal(err)
	}
	if err := waitForSuccessfulSynchronization(synchronizationStatusPath, synchronizationConfirmationTimeout); err != nil {
		fatal(err)
	}
	if err := agentsetup.WriteFileAtomically(setupMarkerPath, []byte("configured\n"), 0o644); err != nil {
		fatal(err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "configured"})
}

func readOptionalFile(path string) ([]byte, bool, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return contents, true, nil
}

func restoreSetupMarker(contents []byte, wasPresent bool) {
	if wasPresent {
		_ = agentsetup.WriteFileAtomically(setupMarkerPath, contents, 0o644)
	}
}

func lookupUID(username string) (string, error) {
	account, err := user.Lookup(username)
	if err != nil {
		return "", err
	}
	return account.Uid, nil
}

func startAgentService() error {
	if output, err := exec.Command(systemctlPath, "enable", "tempo-agent.service").CombinedOutput(); err != nil {
		return fmt.Errorf("enable Compasso service: %w: %s", err, string(output))
	}
	if err := os.Remove(synchronizationStatusPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove previous synchronization status: %w", err)
	}
	command := exec.Command(systemctlPath, "restart", "tempo-agent.service")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("restart Compasso service: %w: %s", err, string(output))
	}
	if err := exec.Command(systemctlPath, "is-active", "--quiet", "tempo-agent.service").Run(); err != nil {
		return errors.New("Compasso service did not remain active")
	}
	return nil
}

func waitForSuccessfulSynchronization(statusPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	lastFailure := "nenhuma resposta recebida"
	for time.Now().Before(deadline) {
		report, err := syncstatus.Read(statusPath)
		if err == nil {
			if report.State == syncstatus.StateOnline {
				return nil
			}
			lastFailure = report.Detail
			if strings.Contains(report.Detail, "HTTP 401") || strings.Contains(report.Detail, "HTTP 403") {
				return fmt.Errorf("server rejected device credentials: %s", report.Detail)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			lastFailure = err.Error()
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("server communication was not confirmed: %s", lastFailure)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "tempo-agent-configure: %v\n", err)
	os.Exit(1)
}
