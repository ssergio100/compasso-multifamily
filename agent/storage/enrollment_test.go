package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestBindEnrollmentClearsUnconfirmedLegacyState(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "agent.db"))
	defer store.Close()
	if err := store.ReplacePolicy(ctx, samplePolicy(6)); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckpointUsage(ctx, DailyUsage{
		LocalDate: "2026-08-12", SecondsUsed: 300, CheckpointAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	binding, err := store.BindEnrollment(ctx, "https://api.example.test", "new-device", "token-1", false)
	if err != nil || !binding.StateReset || binding.InstallationID == "" {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
	if _, err := store.CurrentPolicy(); !errors.Is(err, ErrNoPolicy) {
		t.Fatalf("old policy remains available: %v", err)
	}
	usage, err := store.LoadDailyUsage(ctx, "2026-08-12")
	if err != nil || usage.SecondsUsed != 0 {
		t.Fatalf("old usage=%+v err=%v", usage, err)
	}
	if err := store.ReplacePolicy(ctx, samplePolicy(4)); err != nil {
		t.Fatalf("new server revision was rejected after reset: %v", err)
	}
}

func TestBindEnrollmentPreservesConfirmedUpgradeAndSameDevice(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "agent.db"))
	defer store.Close()
	if err := store.ReplacePolicy(ctx, samplePolicy(6)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO enrollment(singleton_id, server_url, device_id) VALUES (1, ?, ?)`,
		"https://api.example.test", "device-1"); err != nil {
		t.Fatal(err)
	}
	binding, err := store.BindEnrollment(ctx, "https://api.example.test", "device-1", "token-1", false)
	if err != nil || binding.StateReset || binding.InstallationID == "" {
		t.Fatalf("confirmed legacy binding=%+v err=%v", binding, err)
	}
	firstInstallationID := binding.InstallationID
	binding, err = store.BindEnrollment(ctx, "https://api.example.test", "device-1", "token-1", false)
	if err != nil || binding.StateReset || binding.IdentityChanged || binding.InstallationID != firstInstallationID {
		t.Fatalf("same enrollment binding=%+v err=%v", binding, err)
	}
	policy, err := store.CurrentPolicy()
	if err != nil || policy.Revision != 6 {
		t.Fatalf("preserved policy=%+v err=%v", policy, err)
	}
}

func TestBindEnrollmentClearsStateWhenDeviceChanges(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "agent.db"))
	defer store.Close()
	first, err := store.BindEnrollment(ctx, "https://api.example.test", "device-1", "token-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplacePolicy(ctx, samplePolicy(6)); err != nil {
		t.Fatal(err)
	}
	binding, err := store.BindEnrollment(ctx, "https://api.example.test", "device-2", "token-1", true)
	if err != nil || !binding.StateReset || binding.InstallationID == first.InstallationID {
		t.Fatalf("changed enrollment binding=%+v err=%v", binding, err)
	}
	if _, err := store.CurrentPolicy(); !errors.Is(err, ErrNoPolicy) {
		t.Fatalf("old device policy remains available: %v", err)
	}
}

func TestBindEnrollmentChangesIdentityForTokenOnlyWithoutClearingState(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, filepath.Join(t.TempDir(), "agent.db"))
	defer store.Close()
	first, err := store.BindEnrollment(ctx, "https://api.example.test", "device-1", "token-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplacePolicy(ctx, samplePolicy(6)); err != nil {
		t.Fatal(err)
	}
	second, err := store.BindEnrollment(ctx, "https://api.example.test", "device-1", "token-2", false)
	if err != nil || second.StateReset || !second.IdentityChanged || second.InstallationID == first.InstallationID {
		t.Fatalf("rotated token binding=%+v first=%+v err=%v", second, first, err)
	}
	policy, err := store.CurrentPolicy()
	if err != nil || policy.Revision != 6 {
		t.Fatalf("token-only change cleared policy=%+v err=%v", policy, err)
	}
	third, err := store.BindEnrollment(ctx, "https://api.example.test", "device-1", "token-2", false)
	if err != nil || third.IdentityChanged || third.InstallationID != second.InstallationID {
		t.Fatalf("restart changed identity=%+v previous=%+v err=%v", third, second, err)
	}
}
