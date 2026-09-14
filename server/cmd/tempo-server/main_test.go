package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ssergio100/compasso/server/storage"
)

func TestLoadBootstrapAdministratorPasswordFromFile(t *testing.T) {
	passwordFilePath := filepath.Join(t.TempDir(), "admin-password")
	if err := os.WriteFile(passwordFilePath, []byte("domestic-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	password, err := loadBootstrapAdministratorPassword("", passwordFilePath)
	if err != nil || password != "domestic-secret" {
		t.Fatalf("password file result length=%d err=%v", len(password), err)
	}
	if _, err := loadBootstrapAdministratorPassword("environment-secret", passwordFilePath); err == nil {
		t.Fatal("simultaneous password sources were accepted")
	}
}

func TestRunFamilyStateCommand(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "server.db")
	configPath := filepath.Join(directory, "server.toml")
	configuration := "listen_address = \"127.0.0.1:8080\"\ndatabase_path = \"" + databasePath + "\"\n"
	if err := os.WriteFile(configPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(context.Background(), databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if created, err := store.BootstrapAdmin(context.Background(), "owner@example.com", "hash", time.Now()); err != nil || !created {
		t.Fatalf("bootstrap created=%t err=%v", created, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	logger := log.New(&output, "", 0)
	if err := runFamilyStateCommand(configPath, "owner@example.com", "", logger); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(context.Background(), databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	admin, err := store.AdminByLogin(context.Background(), "owner@example.com")
	if err != nil || admin.FamilyState != "suspended" || admin.AuthGeneration != 2 {
		t.Fatalf("suspended admin=%+v err=%v output=%q", admin, err, output.String())
	}
	if err := runFamilyStateCommand(configPath, "", admin.FamilyID, logger); err != nil {
		t.Fatal(err)
	}
	admin, err = store.AdminByLogin(context.Background(), "owner@example.com")
	if err != nil || admin.FamilyState != "active" || admin.AuthGeneration != 3 {
		t.Fatalf("reactivated admin=%+v err=%v output=%q", admin, err, output.String())
	}
	if err := runFamilyStateCommand(configPath, "owner@example.com", admin.FamilyID, logger); err == nil {
		t.Fatal("simultaneous family actions were accepted")
	}
}

func TestRunSuspendedFamilyDeleteCommand(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "server.db")
	configPath := filepath.Join(directory, "server.toml")
	configuration := "listen_address = \"127.0.0.1:8080\"\ndatabase_path = \"" + databasePath + "\"\n"
	if err := os.WriteFile(configPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(context.Background(), databasePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := store.CreateFamilyOwner(context.Background(), "Família", "owner@example.com", "hash", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SetFamilyState(context.Background(), owner.FamilyID, "suspended", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	logger := log.New(&bytes.Buffer{}, "", 0)
	if err := runSuspendedFamilyDeleteCommand(configPath, owner.FamilyID, "wrong-id", logger); err == nil {
		t.Fatal("wrong family ID confirmation was accepted")
	}
	if err := runSuspendedFamilyDeleteCommand(configPath, owner.FamilyID, owner.FamilyID, logger); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(context.Background(), databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AdminByID(context.Background(), owner.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("deleted owner remains: %v", err)
	}
}

func TestAccountMailerOptionsRequireCompleteSMTPConfiguration(t *testing.T) {
	environment := map[string]string{"TEMPO_SMTP_ADDRESS": "smtp.example:587"}
	lookup := func(name string) string { return environment[name] }
	if _, err := accountMailerOptions(lookup); err == nil {
		t.Fatal("incomplete SMTP configuration was accepted")
	}
	environment["TEMPO_SMTP_FROM"] = "contas@example.com"
	environment["TEMPO_SMTP_USERNAME"] = "user"
	environment["TEMPO_SMTP_PASSWORD"] = "secret"
	options, err := accountMailerOptions(lookup)
	if err != nil || len(options) != 1 {
		t.Fatalf("SMTP options=%d err=%v", len(options), err)
	}
	for key := range environment {
		delete(environment, key)
	}
	options, err = accountMailerOptions(lookup)
	if err != nil || len(options) != 0 {
		t.Fatalf("disabled SMTP options=%d err=%v", len(options), err)
	}
	environment["TEMPO_REQUIRE_INSTALLATION_IDENTITY"] = "true"
	options, err = accountMailerOptions(lookup)
	if err != nil || len(options) != 1 {
		t.Fatalf("installation identity option=%d err=%v", len(options), err)
	}
	environment["TEMPO_REQUIRE_INSTALLATION_IDENTITY"] = "invalid"
	if _, err := accountMailerOptions(lookup); err == nil {
		t.Fatal("invalid installation identity flag was accepted")
	}
}
