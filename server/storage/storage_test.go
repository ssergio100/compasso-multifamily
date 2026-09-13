package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdministrativePolicyLifecycle(t *testing.T) {
	ctx := context.Background()
	store := openEmptyTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)

	created, err := store.BootstrapAdmin(ctx, "admin", "hash", now)
	if err != nil || !created {
		t.Fatalf("bootstrap created=%t err=%v", created, err)
	}
	created, err = store.BootstrapAdmin(ctx, "other", "other-hash", now)
	if err != nil || created {
		t.Fatalf("second bootstrap created=%t err=%v", created, err)
	}
	admin, err := store.AdminByLogin(ctx, "admin")
	if err != nil || !admin.Active || admin.PasswordHash != "hash" {
		t.Fatalf("admin=%+v err=%v", admin, err)
	}

	device, err := store.CreateDevice(ctx, "PC do quarto", now)
	if err != nil {
		t.Fatal(err)
	}
	if device.AvatarKey != DefaultAvatarKey {
		t.Fatalf("default avatar=%q", device.AvatarKey)
	}
	if err := store.UpdateDeviceIdentity(ctx, device.ID, "PC da sala", "cat_bow", now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	device, _, err = store.LoadDevice(ctx, device.ID)
	if err != nil || device.Name != "PC da sala" || device.AvatarKey != "cat_bow" {
		t.Fatalf("persisted device identity=%+v err=%v", device, err)
	}
	var quotas [7]int64
	quotas[time.Monday] = 2 * 60 * 60
	quotas[time.Tuesday] = 45 * 60
	if err := store.SaveQuotas(ctx, device.ID, quotas, 10, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, policy, err := store.LoadDevice(ctx, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if policy.WeeklyQuota[time.Monday] != 7200 || policy.WeeklyQuota[time.Tuesday] != 2700 {
		t.Fatalf("independent quotas=%v", policy.WeeklyQuota)
	}

	var weekdays [7]bool
	for day := time.Monday; day <= time.Friday; day++ {
		weekdays[day] = true
	}
	routineID, err := store.SaveRoutine(ctx, device.ID, Routine{
		Name: "Dormir", IconKey: "sleep", Days: weekdays, Start: 22 * 60 * 60, End: 8 * 60 * 60, Enabled: true,
	}, now.Add(2*time.Minute))
	if err != nil || routineID == "" {
		t.Fatalf("routine id=%q err=%v", routineID, err)
	}
	_, policy, err = store.LoadDevice(ctx, device.ID)
	if err != nil || len(policy.Routines) != 1 {
		t.Fatalf("policy=%+v err=%v", policy, err)
	}
	routine := policy.Routines[0]
	if routine.IconKey != "sleep" || routine.Start != 79200 || routine.End != 28800 || !routine.Days[time.Monday] || routine.Days[time.Saturday] {
		t.Fatalf("overnight weekday routine=%+v", routine)
	}
	if _, err := store.SaveRoutine(ctx, device.ID, Routine{
		ID: routineID, Name: "Dormir cedo", Days: weekdays, Start: 21 * 60 * 60, End: 8 * 60 * 60, Enabled: true,
	}, now.Add(150*time.Second)); err != nil {
		t.Fatal(err)
	}
	_, policy, err = store.LoadDevice(ctx, device.ID)
	if err != nil || policy.Routines[0].IconKey != "sleep" {
		t.Fatalf("legacy routine update lost icon: policy=%+v err=%v", policy, err)
	}

	verifier := "$argon2id$v=19$m=8192,t=1,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA"
	if err := store.SetLocalPassword(ctx, device.ID, verifier, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListAudit(ctx, device.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	kinds := make(map[string]bool)
	for _, event := range events {
		kinds[event.Kind] = true
		if strings.Contains(event.Details, "argon2") || strings.Contains(event.Details, verifier) {
			t.Fatalf("audit leaked password verifier: %+v", event)
		}
	}
	for _, kind := range []string{"device_created", "quotas_updated", "routine_saved", "local_password_changed"} {
		if !kinds[kind] {
			t.Fatalf("audit missing %s: %+v", kind, events)
		}
	}
}

func TestMultiFamilyPersistenceBoundary(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)

	firstOwner, err := store.AdminByLogin(ctx, "test-owner")
	if err != nil {
		t.Fatal(err)
	}
	secondOwner, err := store.CreateFamilyOwner(ctx, "Família Dois", " OWNER@EXAMPLE.COM ", "hash-2", now)
	if err != nil {
		t.Fatal(err)
	}
	if secondOwner.Login != "owner@example.com" || secondOwner.FamilyID == firstOwner.FamilyID {
		t.Fatalf("second owner=%+v first owner=%+v", secondOwner, firstOwner)
	}
	reloaded, err := store.AdminByID(ctx, secondOwner.ID)
	if err != nil || reloaded.FamilyID != secondOwner.FamilyID || reloaded.AuthGeneration != 1 {
		t.Fatalf("reloaded owner=%+v err=%v", reloaded, err)
	}

	firstDevice, err := store.CreateDeviceForFamily(ctx, firstOwner.FamilyID, "Primeiro", "cat", now)
	if err != nil {
		t.Fatal(err)
	}
	secondDevice, err := store.CreateDeviceForFamily(ctx, secondOwner.FamilyID, "Segundo", "dog", now)
	if err != nil {
		t.Fatal(err)
	}
	firstDevices, err := store.ListDevicesForFamily(ctx, firstOwner.FamilyID)
	if err != nil || len(firstDevices) != 1 || firstDevices[0].ID != firstDevice.ID {
		t.Fatalf("first family devices=%+v err=%v", firstDevices, err)
	}
	secondDevices, err := store.ListDevicesForFamily(ctx, secondOwner.FamilyID)
	if err != nil || len(secondDevices) != 1 || secondDevices[0].ID != secondDevice.ID {
		t.Fatalf("second family devices=%+v err=%v", secondDevices, err)
	}
	if err := store.DeviceBelongsToFamily(ctx, firstOwner.FamilyID, secondDevice.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-family guard error=%v, want ErrNotFound", err)
	}
	if err := store.DeviceBelongsToFamily(ctx, secondOwner.FamilyID, secondDevice.ID); err != nil {
		t.Fatalf("own-family guard error=%v", err)
	}
	suspended, changed, err := store.SetFamilyState(ctx, secondOwner.Login, "suspended", now.Add(time.Minute))
	if err != nil || !changed || suspended.ID != secondOwner.FamilyID || suspended.State != "suspended" {
		t.Fatalf("suspend family=%+v changed=%t err=%v", suspended, changed, err)
	}
	suspendedOwner, err := store.AdminByID(ctx, secondOwner.ID)
	if err != nil || suspendedOwner.FamilyState != "suspended" || suspendedOwner.AuthGeneration != 2 {
		t.Fatalf("suspended owner=%+v err=%v", suspendedOwner, err)
	}
	if _, err := store.CreateDeviceForFamily(ctx, secondOwner.FamilyID, "Bloqueado", "cat", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("suspended family created device: %v", err)
	}
	reactivated, changed, err := store.SetFamilyState(ctx, secondOwner.FamilyID, "active", now.Add(2*time.Minute))
	if err != nil || !changed || reactivated.State != "active" {
		t.Fatalf("reactivate family=%+v changed=%t err=%v", reactivated, changed, err)
	}
	reactivatedOwner, err := store.AdminByID(ctx, secondOwner.ID)
	if err != nil || reactivatedOwner.AuthGeneration != 3 {
		t.Fatalf("reactivated owner=%+v err=%v", reactivatedOwner, err)
	}

	for index := 1; index < 5; index++ {
		if _, err := store.CreateDeviceForFamily(ctx, firstOwner.FamilyID, fmt.Sprintf("Dispositivo %d", index), "cat", now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.CreateDeviceForFamily(ctx, firstOwner.FamilyID, "Sexto", "cat", now); !errors.Is(err, ErrDeviceLimit) {
		t.Fatalf("sixth device error=%v, want ErrDeviceLimit", err)
	}
}

func TestRoutinesOverlap(t *testing.T) {
	day := func(values ...time.Weekday) (days [7]bool) {
		for _, value := range values {
			days[value] = true
		}
		return days
	}
	tests := []struct {
		name   string
		first  Routine
		second Routine
		want   bool
	}{
		{"contained", Routine{Days: day(time.Monday), Start: 12 * 3600, End: 13 * 3600}, Routine{Days: day(time.Monday), Start: 12*3600 + 15*60, End: 12*3600 + 45*60}, true},
		{"adjacent", Routine{Days: day(time.Monday), Start: 12 * 3600, End: 13 * 3600}, Routine{Days: day(time.Monday), Start: 13 * 3600, End: 14 * 3600}, false},
		{"different days", Routine{Days: day(time.Monday), Start: 12 * 3600, End: 13 * 3600}, Routine{Days: day(time.Tuesday), Start: 12 * 3600, End: 13 * 3600}, false},
		{"overnight next day", Routine{Days: day(time.Monday), Start: 22 * 3600, End: 7 * 3600}, Routine{Days: day(time.Tuesday), Start: 6 * 3600, End: 8 * 3600}, true},
		{"week boundary", Routine{Days: day(time.Sunday), Start: 22 * 3600, End: 7 * 3600}, Routine{Days: day(time.Monday), Start: 6 * 3600, End: 8 * 3600}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := routinesOverlap(test.first, test.second); got != test.want {
				t.Fatalf("routinesOverlap()=%t want=%t", got, test.want)
			}
		})
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store := openEmptyTestStore(t)
	bootstrapTestOwner(t, store, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	return store
}

func openEmptyTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func bootstrapTestOwner(t *testing.T, store *Store, now time.Time) {
	t.Helper()
	created, err := store.BootstrapAdmin(context.Background(), "test-owner", "test-hash", now)
	if err != nil || !created {
		_ = store.Close()
		t.Fatalf("bootstrap test owner created=%t err=%v", created, err)
	}
}
