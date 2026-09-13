package main

import (
	"bytes"
	"context"
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
