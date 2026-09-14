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
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ssergio100/compasso/agent/localauth"
	"github.com/ssergio100/compasso/server/accountmail"
	"github.com/ssergio100/compasso/server/config"
	"github.com/ssergio100/compasso/server/storage"
	"github.com/ssergio100/compasso/server/web"
)

func main() {
	configPath := flag.String("config", "server/config.toml", "server configuration path")
	suspendFamily := flag.String("suspend-family", "", "suspend a family by owner login or family ID, then exit")
	reactivateFamily := flag.String("reactivate-family", "", "reactivate a family by owner login or family ID, then exit")
	deleteSuspendedFamily := flag.String("delete-suspended-family", "", "delete a suspended family by its exact ID, then exit")
	confirmFamilyID := flag.String("confirm-family-id", "", "repeat the family ID required by -delete-suspended-family")
	flag.Parse()
	logger := log.New(os.Stdout, "tempo-server: ", log.LstdFlags|log.LUTC)
	if *deleteSuspendedFamily != "" || *confirmFamilyID != "" {
		if *suspendFamily != "" || *reactivateFamily != "" {
			logger.Printf("fatal: family deletion cannot be combined with a state change")
			os.Exit(1)
		}
		if err := runSuspendedFamilyDeleteCommand(*configPath, *deleteSuspendedFamily, *confirmFamilyID, logger); err != nil {
			logger.Printf("fatal: %v", err)
			os.Exit(1)
		}
		return
	}
	if *suspendFamily != "" || *reactivateFamily != "" {
		if err := runFamilyStateCommand(*configPath, *suspendFamily, *reactivateFamily, logger); err != nil {
			logger.Printf("fatal: %v", err)
			os.Exit(1)
		}
		return
	}
	if err := run(*configPath, logger); err != nil {
		logger.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func runSuspendedFamilyDeleteCommand(configPath, familyID, confirmation string, logger *log.Logger) error {
	familyID, confirmation = strings.TrimSpace(familyID), strings.TrimSpace(confirmation)
	if familyID == "" || confirmation == "" {
		return errors.New("-delete-suspended-family and -confirm-family-id are both required")
	}
	settings, err := config.Load(configPath)
	if err != nil {
		return err
	}
	settings, err = config.ApplyEnvironmentOverrides(settings, os.Getenv)
	if err != nil {
		return err
	}
	store, err := storage.Open(context.Background(), settings.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.DeleteSuspendedFamily(context.Background(), familyID, confirmation); err != nil {
		return err
	}
	logger.Printf("suspended family deleted family_id=%s", familyID)
	return nil
}

func runFamilyStateCommand(configPath, suspendTarget, reactivateTarget string, logger *log.Logger) error {
	if suspendTarget != "" && reactivateTarget != "" {
		return errors.New("configure only one of -suspend-family or -reactivate-family")
	}
	target, state := strings.TrimSpace(suspendTarget), "suspended"
	if target == "" {
		target, state = strings.TrimSpace(reactivateTarget), "active"
	}
	if target == "" {
		return errors.New("family target is required")
	}
	settings, err := config.Load(configPath)
	if err != nil {
		return err
	}
	settings, err = config.ApplyEnvironmentOverrides(settings, os.Getenv)
	if err != nil {
		return err
	}
	store, err := storage.Open(context.Background(), settings.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	family, changed, err := store.SetFamilyState(context.Background(), target, state, time.Now())
	if err != nil {
		return err
	}
	logger.Printf("family state=%s changed=%t family_id=%s owner=%s", family.State, changed, family.ID, family.OwnerLogin)
	return nil
}

func run(configPath string, logger *log.Logger) error {
	settings, err := config.Load(configPath)
	if err != nil {
		return err
	}
	settings, err = config.ApplyEnvironmentOverrides(settings, os.Getenv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settings.DatabasePath), 0o700); err != nil {
		return fmt.Errorf("create server state directory: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	store, err := storage.Open(ctx, settings.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	bootstrapLogin := os.Getenv("TEMPO_ADMIN_LOGIN")
	bootstrapPassword, err := loadBootstrapAdministratorPassword(
		os.Getenv("TEMPO_ADMIN_PASSWORD"), os.Getenv("TEMPO_ADMIN_PASSWORD_FILE"),
	)
	if err != nil {
		return err
	}
	if bootstrapLogin != "" && bootstrapPassword != "" {
		hash, err := localauth.HashPassword(bootstrapPassword, localauth.DefaultArgon2Params)
		if err != nil {
			return fmt.Errorf("hash bootstrap administrator password: %w", err)
		}
		created, err := store.BootstrapAdmin(ctx, bootstrapLogin, hash, time.Now())
		if err != nil {
			return err
		}
		if created {
			logger.Printf("initial administrator created login=%s", bootstrapLogin)
		}
	}
	webOptions, err := accountMailerOptions(os.Getenv)
	if err != nil {
		return err
	}
	application, err := web.New(
		store, settings.SecureCookies, settings.SessionLifetime, settings.OnlineTimeout,
		settings.HeartbeatInterval, settings.AdminOrigin, webOptions...,
	)
	if err != nil {
		return err
	}
	application.StartOfflineDetector(ctx)
	application.StartAccountMaintenance(ctx)
	server := &http.Server{
		Addr: settings.ListenAddress, Handler: application,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		// WriteTimeout stays disabled so authenticated SSE streams stay open;
		// every other endpoint is short-lived and bounded by the caller.
		WriteTimeout: 0, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 32 << 10,
	}
	shutdownDone := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownDone <- server.Shutdown(shutdownContext)
	}()
	logger.Printf("listening address=%s secure_cookies=%t", settings.ListenAddress, settings.SecureCookies)
	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return <-shutdownDone
}

func accountMailerOptions(environmentValue func(string) string) ([]web.Option, error) {
	options := make([]web.Option, 0, 2)
	address := strings.TrimSpace(environmentValue("TEMPO_SMTP_ADDRESS"))
	username := environmentValue("TEMPO_SMTP_USERNAME")
	password := environmentValue("TEMPO_SMTP_PASSWORD")
	from := strings.TrimSpace(environmentValue("TEMPO_SMTP_FROM"))
	if address != "" || username != "" || password != "" || from != "" {
		if address == "" || from == "" {
			return nil, errors.New("TEMPO_SMTP_ADDRESS and TEMPO_SMTP_FROM are required when account e-mail is configured")
		}
		mailer, err := accountmail.NewSMTP(address, username, password, from)
		if err != nil {
			return nil, fmt.Errorf("configure account e-mail: %w", err)
		}
		options = append(options, web.WithAccountMailer(mailer))
	}
	if raw := strings.TrimSpace(environmentValue("TEMPO_REQUIRE_INSTALLATION_IDENTITY")); raw != "" {
		required, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("parse TEMPO_REQUIRE_INSTALLATION_IDENTITY: %w", err)
		}
		options = append(options, web.WithInstallationIdentityRequired(required))
	}
	return options, nil
}

func loadBootstrapAdministratorPassword(environmentPassword, passwordFilePath string) (string, error) {
	if environmentPassword != "" && passwordFilePath != "" {
		return "", errors.New("configure only one of TEMPO_ADMIN_PASSWORD or TEMPO_ADMIN_PASSWORD_FILE")
	}
	if passwordFilePath == "" {
		return environmentPassword, nil
	}
	fileInformation, err := os.Stat(passwordFilePath)
	if err != nil {
		return "", fmt.Errorf("inspect bootstrap administrator password file: %w", err)
	}
	if fileInformation.IsDir() || fileInformation.Size() > 4096 {
		return "", errors.New("bootstrap administrator password file must be a regular file of at most 4096 bytes")
	}
	contents, err := os.ReadFile(passwordFilePath)
	if err != nil {
		return "", fmt.Errorf("read bootstrap administrator password file: %w", err)
	}
	return strings.TrimRight(string(contents), "\r\n"), nil
}
