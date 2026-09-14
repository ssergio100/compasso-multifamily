package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	protocol "github.com/ssergio100/compasso/protocol/v1"
	serverstorage "github.com/ssergio100/compasso/server/storage"
)

// TestPilotLoadOneLogicalHour500Agents is intentionally opt-in: it performs
// 120,000 durable SQLite transactions. It advances the server clock through a
// complete hour while keeping the real 5/30 second heartbeat rates and a
// deterministic phase distribution below the pilot's global rate limit.
func TestPilotLoadOneLogicalHour500Agents(t *testing.T) {
	if os.Getenv("COMPASSO_RUN_PILOT_LOAD_TEST") != "1" {
		t.Skip("set COMPASSO_RUN_PILOT_LOAD_TEST=1 to run the pilot admission load test")
	}
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "pilot-load.db")
	store, err := serverstorage.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	start := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	agents := provisionPilotAgents(t, store, start)
	application, err := New(
		store, true, time.Hour, time.Minute, 3*time.Second, "https://admin.example",
		WithInstallationIdentityRequired(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	serverNow := start
	application.now = func() time.Time { return serverNow }
	bytesBefore := sqliteFilesSize(t, databasePath)

	activePayload, err := json.Marshal(protocol.HeartbeatRequest{
		PolicyRevision: 1, ControlRevision: 1, SessionStateRevision: 1,
		LocalDate: "2026-08-10", GraphicalSessionActive: true,
		GraphicalSessionID: "pilot-load-session",
	})
	if err != nil {
		t.Fatal(err)
	}
	idlePayload, err := json.Marshal(protocol.HeartbeatRequest{
		PolicyRevision: 1, ControlRevision: 1, LocalDate: "2026-08-10",
	})
	if err != nil {
		t.Fatal(err)
	}

	latencies := make([]time.Duration, 0, 120_000)
	statusCounts := make(map[int]int)
	for second := 0; second < 3600; second++ {
		serverNow = start.Add(time.Duration(second) * time.Second)
		for index, agent := range agents {
			active := index < 100
			interval := 30
			phase := (index - 100) % interval
			payload := idlePayload
			if active {
				interval = 5
				phase = index % interval
				payload = activePayload
			}
			if second%interval != phase {
				continue
			}
			request := httptest.NewRequest(http.MethodPost, protocol.HeartbeatPath, bytes.NewReader(payload))
			request.Header.Set(deviceIDHeader, agent.deviceID)
			request.Header.Set("Authorization", "Bearer "+agent.token)
			request.Header.Set(protocol.VersionHeader, protocol.CurrentProtocolVersion)
			request.Header.Set(protocol.CapabilitiesHeader,
				protocol.InstallationIdentityCapability+", "+protocol.NextHeartbeatCapability)
			request.Header.Set(protocol.InstallationIDHeader, agent.installationID)
			response := httptest.NewRecorder()
			began := time.Now()
			application.ServeHTTP(response, request)
			latencies = append(latencies, time.Since(began))
			statusCounts[response.Code]++
			if response.Code != http.StatusOK {
				t.Fatalf("heartbeat second=%d agent=%d status=%d body=%s", second, index, response.Code, response.Body.String())
			}
		}
	}

	const expectedHeartbeats = 120_000
	if len(latencies) != expectedHeartbeats || statusCounts[http.StatusOK] != expectedHeartbeats {
		t.Fatalf("heartbeats=%d status=%v", len(latencies), statusCounts)
	}
	sort.Slice(latencies, func(left, right int) bool { return latencies[left] < latencies[right] })
	p95 := latencies[(95*len(latencies)+99)/100-1]
	if p95 >= time.Second {
		t.Fatalf("heartbeat p95=%s, expected below 1s", p95)
	}
	logs, err := store.ListCommunicationLogs(ctx, agents[0].deviceID, 0, 10)
	if err != nil || len(logs) != 0 {
		t.Fatalf("unchanged healthy heartbeats created communication history: logs=%d err=%v", len(logs), err)
	}

	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsResponse := httptest.NewRecorder()
	application.ServeHTTP(metricsResponse, metricsRequest)
	if metricsResponse.Code != http.StatusOK ||
		!strings.Contains(metricsResponse.Body.String(), `compasso_heartbeats_total{result="accepted"} 120000`) ||
		!strings.Contains(metricsResponse.Body.String(), `compasso_heartbeats_total{result="server_error"} 0`) {
		t.Fatalf("unexpected metrics status=%d body=%s", metricsResponse.Code, metricsResponse.Body.String())
	}
	bytesAfter := sqliteFilesSize(t, databasePath)
	t.Logf("pilot-load agents=500 active=100 idle=400 logical_duration=1h heartbeats=%d p95=%s statuses=%v database_before=%d database_after=%d database_growth=%d",
		len(latencies), p95, statusCounts, bytesBefore, bytesAfter, bytesAfter-bytesBefore)
}

type pilotAgent struct {
	deviceID, token, installationID string
}

func provisionPilotAgents(t *testing.T, store *serverstorage.Store, now time.Time) []pilotAgent {
	t.Helper()
	ctx := context.Background()
	agents := make([]pilotAgent, 0, 500)
	for familyIndex := 0; familyIndex < 100; familyIndex++ {
		owner, err := store.CreateFamilyOwner(ctx,
			fmt.Sprintf("Família de carga %03d", familyIndex+1),
			fmt.Sprintf("carga-%03d@example.com", familyIndex+1), "load-test-password-hash", now)
		if err != nil {
			t.Fatalf("create family %d: %v", familyIndex, err)
		}
		for deviceIndex := 0; deviceIndex < 5; deviceIndex++ {
			device, err := store.CreateDeviceForFamily(ctx, owner.FamilyID,
				fmt.Sprintf("Carga %03d-%d", familyIndex+1, deviceIndex+1), "cat", now)
			if err != nil {
				t.Fatalf("create device family=%d device=%d: %v", familyIndex, deviceIndex, err)
			}
			token, err := store.IssueDeviceToken(ctx, device.ID, now)
			if err != nil {
				t.Fatalf("issue device token family=%d device=%d: %v", familyIndex, deviceIndex, err)
			}
			ordinal := len(agents) + 1
			agents = append(agents, pilotAgent{
				deviceID: device.ID, token: token,
				installationID: fmt.Sprintf("%08x-0000-4000-8000-%012x", ordinal, ordinal),
			})
		}
	}
	return agents
}

func sqliteFilesSize(t *testing.T, databasePath string) int64 {
	t.Helper()
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(databasePath + suffix)
		if err == nil {
			total += info.Size()
			continue
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	return total
}
