package localapi

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssergio100/compasso/agent/localauth"
	"github.com/ssergio100/compasso/agent/localmsg"
	"github.com/ssergio100/compasso/agent/storage"
)

type fakeSynchronization struct {
	checked bool
	online  bool
	detail  string
}

func (f *fakeSynchronization) SynchronizationStatus() (bool, bool) {
	return f.checked, f.online
}

func (f *fakeSynchronization) SynchronizationReport() (bool, bool, string) {
	return f.checked, f.online, f.detail
}

func TestBonusAPIMapsPasswordErrorsAndReturnsEventUUID(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	params := localauth.Argon2Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	verifier, err := localauth.HashPassword("secret", params)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.Local)
	if err := store.ReplacePolicy(ctx, storage.PolicySnapshot{Revision: 1, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	service, _ := localauth.NewService(store)
	synchronization := &fakeSynchronization{}
	api, _ := newBonusAPI(service, synchronization)
	api.now = func() time.Time { return now }
	if _, dbusErr := api.AddLocalBonus("anything", 900); dbusErr == nil || !strings.HasSuffix(dbusErr.Name, "PasswordNotConfigured") {
		t.Fatalf("missing password D-Bus error=%v", dbusErr)
	}
	if err := store.ReplacePolicy(ctx, storage.PolicySnapshot{
		Revision: 2, LocalPasswordVerifier: verifier, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, dbusErr := api.AddLocalBonus("wrong", 900); dbusErr == nil || !strings.HasSuffix(dbusErr.Name, "InvalidPassword") {
		t.Fatalf("wrong password D-Bus error=%v", dbusErr)
	}
	api.now = func() time.Time { return now.Add(2 * time.Second) }
	uuid, dbusErr := api.AddLocalBonus("secret", 900)
	if dbusErr != nil || uuid == "" {
		t.Fatalf("correct password uuid=%q error=%v", uuid, dbusErr)
	}
}

func TestSynchronizationStatusReflectsLiveSource(t *testing.T) {
	synchronization := &fakeSynchronization{}
	api := &bonusAPI{synchronization: synchronization}
	if status, _ := api.GetSynchronizationStatus(); status != "checking" {
		t.Fatalf("initial status=%q", status)
	}
	synchronization.checked = true
	if status, _ := api.GetSynchronizationStatus(); status != "offline" {
		t.Fatalf("offline status=%q", status)
	}
	synchronization.online = true
	if status, _ := api.GetSynchronizationStatus(); status != "online" {
		t.Fatalf("online status=%q", status)
	}
}

func TestSynchronizationReportExplainsFailuresWithoutExposingRawDetails(t *testing.T) {
	synchronization := &fakeSynchronization{
		checked: true,
		detail:  "heartbeat returned HTTP 400 secret=must-not-appear",
	}
	api := &bonusAPI{synchronization: synchronization}
	status, detail, dbusErr := api.GetSynchronizationReport()
	if dbusErr != nil || status != "offline" || !strings.Contains(detail, "erro 400") {
		t.Fatalf("offline report status=%q detail=%q error=%v", status, detail, dbusErr)
	}
	if strings.Contains(detail, "must-not-appear") {
		t.Fatalf("raw synchronization detail was exposed: %q", detail)
	}

	synchronization.online = true
	status, detail, dbusErr = api.GetSynchronizationReport()
	if dbusErr != nil || status != "online" || detail != "" {
		t.Fatalf("online report status=%q detail=%q error=%v", status, detail, dbusErr)
	}
}

func TestHumanSynchronizationDetailCoversActionableFailures(t *testing.T) {
	tests := []struct {
		detail string
		want   string
	}{
		{"heartbeat returned HTTP 401", "identificação"},
		{"heartbeat returned HTTP 426", "atualizado"},
		{"heartbeat returned HTTP 502", "temporariamente"},
		{"dial tcp: lookup api.test: no such host", "localizar"},
		{"context deadline exceeded", "demorou"},
		{"unexpected local error", "configurações"},
	}
	for _, test := range tests {
		if got := localmsg.SynchronizationDetail(test.detail); !strings.Contains(got, test.want) {
			t.Errorf("detail %q mapped to %q, want %q", test.detail, got, test.want)
		}
	}
}
