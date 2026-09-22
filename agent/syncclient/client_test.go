package syncclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ssergio100/compasso/agent/localauth"
	"github.com/ssergio100/compasso/agent/policy"
	agentstorage "github.com/ssergio100/compasso/agent/storage"
	protocol "github.com/ssergio100/compasso/protocol/v1"
	serverstorage "github.com/ssergio100/compasso/server/storage"
	"github.com/ssergio100/compasso/server/web"
)

const testInstallationID = "79e78a4f-713b-4e19-a756-b60a0505248a"

func TestSynchronizationStatusTracksHeartbeatResult(t *testing.T) {
	client := &Client{}
	if checked, online := client.SynchronizationStatus(); checked || online {
		t.Fatalf("initial status checked=%t online=%t", checked, online)
	}
	client.setRemoteControl(true, false, false, 1)
	if checked, online := client.SynchronizationStatus(); !checked || !online {
		t.Fatalf("successful status checked=%t online=%t", checked, online)
	}
	client.setRemoteControl(false, false, false, 0)
	if checked, online := client.SynchronizationStatus(); !checked || online {
		t.Fatalf("failed status checked=%t online=%t", checked, online)
	}
}

func TestSynchronizationReportKeepsTheLastSanitizedFailure(t *testing.T) {
	client := &Client{}
	client.setRemoteControl(false, false, false, 0)
	client.reportStatus(errors.New("heartbeat returned HTTP 400"))
	checked, online, detail := client.SynchronizationReport()
	if !checked || online || detail != "heartbeat returned HTTP 400" {
		t.Fatalf("offline report checked=%t online=%t detail=%q", checked, online, detail)
	}
	client.setRemoteControl(true, false, false, 1)
	client.reportStatus(nil)
	checked, online, detail = client.SynchronizationReport()
	if !checked || !online || detail != "" {
		t.Fatalf("online report checked=%t online=%t detail=%q", checked, online, detail)
	}
}

func TestRunReportsSuccessfulSynchronization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agentStore, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()
	httpClient := &http.Client{Transport: handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(protocol.VersionHeader) != protocol.CurrentProtocolVersion {
			t.Errorf("protocol version header=%q", r.Header.Get(protocol.VersionHeader))
		}
		if r.Header.Get(protocol.CapabilitiesHeader) != protocol.NextHeartbeatCapability+", "+protocol.CommandAckReceiptCapability+", "+protocol.InstallationIdentityCapability {
			t.Errorf("protocol capabilities header=%q", r.Header.Get(protocol.CapabilitiesHeader))
		}
		var heartbeat protocol.HeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
			t.Fatal(err)
		}
		if strings.Join(heartbeat.Capabilities, ", ") != r.Header.Get(protocol.CapabilitiesHeader) {
			t.Errorf("heartbeat capabilities=%v header=%q", heartbeat.Capabilities, r.Header.Get(protocol.CapabilitiesHeader))
		}
		if r.Header.Get(protocol.InstallationIDHeader) != testInstallationID {
			t.Errorf("installation ID header=%q", r.Header.Get(protocol.InstallationIDHeader))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})}}
	client, err := New(agentStore, httpClient, Config{
		ServerURL: "http://tempo.test", DeviceID: "device", DeviceToken: "token", InstallationID: testInstallationID,
		HeartbeatInterval: time.Second, AttemptTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	reported := make(chan error, 1)
	client.SetStatusReporter(func(synchronizationError error) {
		reported <- synchronizationError
		cancel()
	})
	finished := make(chan error, 1)
	go func() {
		finished <- client.Run(ctx, log.New(io.Discard, "", 0))
	}()
	select {
	case synchronizationError := <-reported:
		if synchronizationError != nil {
			t.Fatalf("reported synchronization error=%v", synchronizationError)
		}
	case <-time.After(time.Second):
		t.Fatal("successful synchronization was not reported")
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestRunUsesServerIntervalAndDoesNotPersistItAcrossRestart(t *testing.T) {
	ctx := context.Background()
	agentStore, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()

	serverIntervals := []int64{4, 9}
	var requests uint32
	httpClient := &http.Client{Transport: handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestIndex := int(atomic.AddUint32(&requests, 1)) - 1
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(protocol.HeartbeatResponse{
			NextHeartbeatSeconds: serverIntervals[requestIndex],
		})
	})}}
	client, err := New(agentStore, httpClient, Config{
		ServerURL: "http://tempo.test", DeviceID: "device", DeviceToken: "token", InstallationID: testInstallationID,
		HeartbeatInterval: DefaultHeartbeatInterval, AttemptTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	client.jitter = func(delay time.Duration) time.Duration { return delay }
	var waits []time.Duration
	client.wait = func(_ context.Context, delay time.Duration) bool {
		waits = append(waits, delay)
		return len(waits) < 2
	}
	if err := client.Run(ctx, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 2 || waits[0] != 4*time.Second || waits[1] != 9*time.Second {
		t.Fatalf("server-controlled waits=%v", waits)
	}

	// A new process starts from its embedded fallback. The previously received
	// interval was process-local and never reached durable storage.
	restartedClient, err := New(agentStore, &http.Client{Transport: handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})}}, Config{
		ServerURL: "http://tempo.test", DeviceID: "device", DeviceToken: "token", InstallationID: testInstallationID,
		HeartbeatInterval: DefaultHeartbeatInterval, AttemptTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	restartedClient.jitter = func(delay time.Duration) time.Duration { return delay }
	restartedWait := time.Duration(0)
	restartedClient.wait = func(_ context.Context, delay time.Duration) bool {
		restartedWait = delay
		return false
	}
	if err := restartedClient.Run(ctx, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}
	if restartedWait != DefaultHeartbeatInterval {
		t.Fatalf("restart wait=%s, want embedded fallback", restartedWait)
	}
}

func TestNextHeartbeatIntervalRejectsMissingAndUnsafeValues(t *testing.T) {
	fallback := DefaultHeartbeatInterval
	for _, test := range []struct {
		name    string
		seconds int64
		want    time.Duration
	}{
		{name: "missing", seconds: 0, want: fallback},
		{name: "negative", seconds: -1, want: fallback},
		{name: "below minimum", seconds: int64(MinimumHeartbeatInterval/time.Second) - 1, want: fallback},
		{name: "minimum", seconds: int64(MinimumHeartbeatInterval / time.Second), want: MinimumHeartbeatInterval},
		{name: "maximum", seconds: int64(MaximumHeartbeatInterval / time.Second), want: MaximumHeartbeatInterval},
		{name: "above maximum", seconds: int64(MaximumHeartbeatInterval/time.Second) + 1, want: fallback},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nextHeartbeatInterval(test.seconds, fallback); got != test.want {
				t.Fatalf("next interval=%s, want %s", got, test.want)
			}
		})
	}
}

func TestNewRejectsUnsafeHeartbeatFallback(t *testing.T) {
	ctx := context.Background()
	agentStore, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()
	for _, interval := range []time.Duration{time.Millisecond, MaximumHeartbeatInterval + time.Second} {
		if _, err := New(agentStore, http.DefaultClient, Config{
			ServerURL: "http://tempo.test", DeviceID: "device", DeviceToken: "token", InstallationID: testInstallationID,
			HeartbeatInterval: interval, AttemptTimeout: time.Second,
		}); err == nil {
			t.Fatalf("unsafe heartbeat fallback %s accepted", interval)
		}
	}
}

func TestControlCommandAcknowledgesOnlyAfterGraphicalEffect(t *testing.T) {
	ctx := context.Background()
	agentStore, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()

	const commandID = "block-command"
	var requests []protocol.HeartbeatRequest
	httpClient := &http.Client{Transport: handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var heartbeat protocol.HeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
			t.Errorf("decode heartbeat: %v", err)
		}
		requests = append(requests, heartbeat)
		w.Header().Set("Content-Type", "application/json")
		if len(requests) <= 2 {
			_, _ = w.Write([]byte(`{
				"server_time":"2026-08-24T12:00:00Z",
				"commands":[{"id":"block-command","kind":"block_now","created_at":"2026-08-24T12:00:00Z"}],
				"control":{"revision":2,"manual_block":true}
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"server_time":"2026-08-24T12:00:01Z",
			"acknowledged_commands":["block-command"],
			"control":{"revision":2,"manual_block":true}
		}`))
	})}}
	client, err := New(agentStore, httpClient, Config{
		ServerURL: "http://tempo.test", DeviceID: "device", DeviceToken: "token", InstallationID: testInstallationID,
		HeartbeatInterval: time.Second, AttemptTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	if _, err := client.Heartbeat(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Heartbeat(ctx, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(requests[1].CommandAcks) != 0 {
		t.Fatalf("command acknowledged before graphical effect: %v", requests[1].CommandAcks)
	}
	if err := agentStore.CompleteControlEffect(ctx, commandID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Heartbeat(ctx, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(requests[2].CommandAcks) != 1 || requests[2].CommandAcks[0] != commandID {
		t.Fatalf("confirmed command acknowledgements=%v", requests[2].CommandAcks)
	}
	if ids, err := agentStore.AppliedCommandIDs(ctx, 10); err != nil || len(ids) != 0 {
		t.Fatalf("server-confirmed command remained locally=%v err=%v", ids, err)
	}
}

func TestRunTimesOutStalledAttemptAndReconnects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agentStore, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()
	transport := &recoveryTransport{}
	httpClient := &http.Client{Transport: transport}
	client, err := New(agentStore, httpClient, Config{
		ServerURL: "http://tempo.test", DeviceID: "device", DeviceToken: "token", InstallationID: testInstallationID,
		HeartbeatInterval: MinimumHeartbeatInterval, AttemptTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Bound this timing test without weakening the production constructor.
	client.config.HeartbeatInterval = 5 * time.Millisecond
	reported := make(chan error, 2)
	client.SetStatusReporter(func(synchronizationError error) {
		reported <- synchronizationError
		if synchronizationError == nil {
			cancel()
		}
	})
	var output bytes.Buffer
	finished := make(chan error, 1)
	go func() {
		finished <- client.Run(ctx, log.New(&output, "", 0))
	}()
	for expectedOffline := true; ; expectedOffline = false {
		select {
		case synchronizationError := <-reported:
			if expectedOffline && synchronizationError == nil {
				t.Fatal("stalled attempt was not reported offline")
			}
			if !expectedOffline && synchronizationError != nil {
				t.Fatalf("recovery reported error=%v", synchronizationError)
			}
			if !expectedOffline {
				goto recovered
			}
		case <-time.After(time.Second):
			t.Fatal("synchronization did not recover after the stalled attempt")
		}
	}

recovered:
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if attempts := atomic.LoadUint32(&transport.attempts); attempts != 2 {
		t.Fatalf("transport attempts=%d, want 2", attempts)
	}
	if closes := atomic.LoadUint32(&transport.idleCloses); closes != 1 {
		t.Fatalf("idle connection closes=%d, want 1", closes)
	}
	logs := output.String()
	if !strings.Contains(logs, "offline stage=transport attempt=1") ||
		!strings.Contains(logs, "online attempts=1") {
		t.Fatalf("unexpected synchronization logs: %s", logs)
	}
}

func TestDecodeHeartbeatErrorExplainsRequiredAgentUpgrade(t *testing.T) {
	err := decodeHeartbeatError(http.StatusUpgradeRequired, "", strings.NewReader(
		`{"error":"agent update required","code":"agent_upgrade_required"}`,
	))
	if !strings.Contains(err.Error(), "update the Compasso agent") {
		t.Fatalf("upgrade error=%q", err)
	}
}

func TestDecodeHeartbeatErrorExplainsRevisionConflict(t *testing.T) {
	err := decodeHeartbeatError(http.StatusConflict, "", strings.NewReader(
		`{"error":"client synchronization state is newer than this device","code":"revision_ahead","client_revision":9,"server_revision":1}`,
	))
	if !strings.Contains(err.Error(), "local revision 9") ||
		!strings.Contains(err.Error(), "server revision 1") ||
		!strings.Contains(err.Error(), "another enrollment") {
		t.Fatalf("revision conflict error=%q", err)
	}
}

func TestDecodeHeartbeatErrorDoesNotExposeUntrustedServerMessage(t *testing.T) {
	err := decodeHeartbeatError(http.StatusUnauthorized, "", strings.NewReader(
		`{"error":"secret supplied by an untrusted proxy"}`,
	))
	if err.Error() != "heartbeat returned incompatible HTTP 401" {
		t.Fatalf("generic heartbeat error=%q", err)
	}
}

func TestDecodeHeartbeatErrorClassifiesRetryDelays(t *testing.T) {
	for _, test := range []struct {
		name, retryAfter, payload string
		status                    int
		kind                      heartbeatRetryKind
		delay                     time.Duration
	}{
		{name: "rate minimum", status: http.StatusTooManyRequests, retryAfter: "1", kind: retryRateLimited, delay: 5 * time.Second},
		{name: "rate value", status: http.StatusTooManyRequests, retryAfter: "120", kind: retryRateLimited, delay: 2 * time.Minute},
		{name: "rate maximum", status: http.StatusTooManyRequests, retryAfter: "99999", kind: retryRateLimited, delay: time.Hour},
		{name: "permanent", status: http.StatusConflict, payload: `{"code":"installation_conflict"}`, kind: retryPermanent},
		{name: "incompatible", status: http.StatusBadRequest, payload: `{}`, kind: retryIncompatible},
		{name: "transient", status: http.StatusServiceUnavailable, payload: `{}`, kind: retryTransient},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := decodeHeartbeatError(test.status, test.retryAfter, strings.NewReader(test.payload))
			var remote *remoteHeartbeatError
			if !errors.As(err, &remote) || remote.retryKind != test.kind || remote.retryAfter != test.delay {
				t.Fatalf("classification=%+v err=%v", remote, err)
			}
		})
	}
}

func TestHeartbeatJitterStaysBetweenZeroAndTenPercent(t *testing.T) {
	base := 30 * time.Second
	for attempt := 0; attempt < 100; attempt++ {
		delay := jitteredHeartbeatDelay(base)
		if delay < base || delay > base+base/10 {
			t.Fatalf("jittered delay=%s", delay)
		}
	}
}

func TestRunSchedulesFailuresByServerClassification(t *testing.T) {
	for _, test := range []struct {
		name, code, retryAfter string
		status                 int
		want                   time.Duration
	}{
		{name: "transient", status: http.StatusServiceUnavailable, want: time.Second},
		{name: "rate limited", status: http.StatusTooManyRequests, code: "rate_limited", retryAfter: "120", want: 2 * time.Minute},
		{name: "permanent", status: http.StatusConflict, code: "installation_conflict", want: 6 * time.Hour},
		{name: "incompatible", status: http.StatusBadRequest, want: time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_ = json.NewEncoder(w).Encode(protocol.ErrorResponse{Error: "untrusted", Code: test.code})
			})
			client, err := New(store, &http.Client{Transport: handlerTransport{handler: handler}}, Config{
				ServerURL: "http://tempo.test", DeviceID: "device", DeviceToken: "token", InstallationID: testInstallationID,
				HeartbeatInterval: 30 * time.Second, AttemptTimeout: time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			client.jitter = func(delay time.Duration) time.Duration { return delay }
			var scheduled time.Duration
			client.wait = func(_ context.Context, delay time.Duration) bool { scheduled = delay; return false }
			if err := client.Run(ctx, log.New(io.Discard, "", 0)); err != nil {
				t.Fatal(err)
			}
			if scheduled != test.want {
				t.Fatalf("scheduled retry=%s, want %s", scheduled, test.want)
			}
		})
	}
}

func TestDecodeLegacyConflictStillExplainsRevisionCause(t *testing.T) {
	err := decodeHeartbeatError(http.StatusConflict, "", strings.NewReader(
		`{"error":"heartbeat rejected"}`,
	))
	if !strings.Contains(err.Error(), "local synchronization state is newer") {
		t.Fatalf("legacy revision conflict error=%q", err)
	}
}

func TestRevisionOfflineQueueAndImmediateEnforcement(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.Local)
	serverStore := openServerTestStore(t, ctx, filepath.Join(t.TempDir(), "server.db"), now)
	defer serverStore.Close()
	device, err := serverStore.CreateDevice(ctx, "Zorin", now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := serverStore.IssueDeviceToken(ctx, device.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	var quotas [7]int64
	quotas[time.Monday] = 3600
	// Initial revision is 1; nine writes produce the explicit revision 10 case.
	for revision := 2; revision <= 10; revision++ {
		if err := serverStore.SaveQuotas(ctx, device.ID, quotas, 10, now.Add(time.Duration(revision)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	application, err := web.New(serverStore, false, time.Hour, time.Minute, 3*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	var online uint32
	atomic.StoreUint32(&online, 1)
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadUint32(&online) == 0 {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		application.ServeHTTP(w, r)
	})
	httpClient := &http.Client{Transport: handlerTransport{handler: proxyHandler}}
	agentStore, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()
	client, err := New(agentStore, httpClient, Config{
		ServerURL: "http://tempo.test", DeviceID: device.ID, DeviceToken: token, InstallationID: testInstallationID,
		HeartbeatInterval: 10 * time.Second, AttemptTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Heartbeat(ctx, now); err != nil {
		t.Fatal(err)
	}
	localPolicy, err := agentStore.LoadPolicy(ctx)
	if err != nil || localPolicy.Revision != 10 {
		t.Fatalf("initial revision=%d err=%v", localPolicy.Revision, err)
	}
	if err := agentStore.CheckpointUsage(ctx, agentstorage.DailyUsage{LocalDate: "2026-08-10", SecondsUsed: 120, CheckpointAt: now}); err != nil {
		t.Fatal(err)
	}
	quotas[time.Monday] = 60
	if err := serverStore.SaveQuotas(ctx, device.ID, quotas, 10, now.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Heartbeat(ctx, now.Add(21*time.Second)); err != nil {
		t.Fatal(err)
	}
	localPolicy, err = agentStore.LoadPolicy(ctx)
	if err != nil || localPolicy.Revision != 11 || localPolicy.WeeklyQuota[time.Monday] != time.Minute {
		t.Fatalf("revision 11 not applied immediately: policy=%+v err=%v", localPolicy, err)
	}
	decision, err := policy.Evaluate(policy.Input{
		Now: now.Add(21 * time.Second), Monitoring: policy.MonitoringActive,
		Quota: policy.WeeklyQuota(localPolicy.WeeklyQuota), Consumed: 120 * time.Second,
	})
	if err != nil || decision.Allowed {
		t.Fatalf("reduced remote quota did not block immediately: decision=%+v err=%v", decision, err)
	}

	bonusPayload, _ := json.Marshal(map[string]interface{}{"local_date": "2026-08-10", "seconds": 300, "origin": "local"})
	bonus := agentstorage.Bonus{UUID: "offline-bonus", LocalDate: "2026-08-10", Seconds: 300, Origin: "local", CreatedAt: now.Add(time.Minute)}
	event := agentstorage.PendingEvent{UUID: bonus.UUID, Kind: "bonus_added", PayloadJSON: string(bonusPayload), CreatedAt: bonus.CreatedAt}
	if err := agentStore.AddBonusWithEvent(ctx, bonus, event); err != nil {
		t.Fatal(err)
	}
	atomic.StoreUint32(&online, 0)
	if _, err := client.Heartbeat(ctx, now.Add(2*time.Minute)); err == nil {
		t.Fatal("offline heartbeat unexpectedly succeeded")
	}
	tracker, err := agentstorage.NewUsageTracker(ctx, agentStore, "2026-08-10", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.Add(ctx, 30*time.Minute, now.Add(32*time.Minute)); err != nil {
		t.Fatal(err)
	}
	preserved, err := agentStore.LoadPolicy(ctx)
	usage, usageErr := agentStore.LoadDailyUsage(ctx, "2026-08-10")
	if err != nil || usageErr != nil || preserved.Revision != 11 || usage.SecondsUsed != 1920 {
		t.Fatalf("offline state not preserved: revision=%d usage=%d errors=%v/%v", preserved.Revision, usage.SecondsUsed, err, usageErr)
	}
	pending, err := agentStore.PendingEvents(ctx, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("offline event queue=%+v err=%v", pending, err)
	}
	atomic.StoreUint32(&online, 1)
	if _, err := client.Heartbeat(ctx, now.Add(33*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Heartbeat(ctx, now.Add(34*time.Minute)); err != nil {
		t.Fatal(err)
	}
	pending, err = agentStore.PendingEvents(ctx, 10)
	serverSummary, summaryErr := serverStore.LoadDailySummary(ctx, device.ID, "2026-08-10")
	if err != nil || summaryErr != nil || len(pending) != 0 || serverSummary.BonusSeconds != 300 {
		t.Fatalf("reconnected bonus pending=%d server=%+v errors=%v/%v", len(pending), serverSummary, err, summaryErr)
	}
}

func TestTransportErrorsRedactDeviceToken(t *testing.T) {
	ctx := context.Background()
	agentStore, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()
	deviceToken := "secret-device-token-that-must-not-appear"
	httpClient := &http.Client{Transport: failingTransport{failure: fmt.Errorf("failed Authorization: Bearer %s", deviceToken)}}
	client, err := New(agentStore, httpClient, Config{
		ServerURL: "http://tempo.test", DeviceID: "device", DeviceToken: deviceToken, InstallationID: testInstallationID,
		HeartbeatInterval: 10 * time.Second, AttemptTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, heartbeatError := client.Heartbeat(ctx, time.Date(2026, time.August, 10, 12, 0, 0, 0, time.Local))
	if heartbeatError == nil || strings.Contains(heartbeatError.Error(), deviceToken) || !strings.Contains(heartbeatError.Error(), "[REDACTED]") {
		t.Fatalf("transport error was not sanitized: %v", heartbeatError)
	}
}

func TestSessionBalanceIsAnchoredOnceAndOnlyRefreshedByRealChange(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.Local)
	serverStore := openServerTestStore(t, ctx, filepath.Join(t.TempDir(), "server.db"), now)
	defer serverStore.Close()
	device, err := serverStore.CreateDevice(ctx, "Anchored balance", now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := serverStore.IssueDeviceToken(ctx, device.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	var quotas [7]int64
	quotas[now.Weekday()] = 600
	if err := serverStore.SaveQuotas(ctx, device.ID, quotas, 1, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	application, err := web.New(serverStore, false, time.Hour, time.Minute, 3*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	agentStore, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()
	client, err := New(agentStore, &http.Client{Transport: handlerTransport{handler: application}}, Config{
		ServerURL: "http://tempo.test", DeviceID: device.ID, DeviceToken: token, InstallationID: testInstallationID,
		HeartbeatInterval: 10 * time.Second, AttemptTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	client.SetGraphicalSession(true, "session-7", false)
	first, err := client.Heartbeat(ctx, now.Add(2*time.Second))
	if err != nil || first.SessionState == nil || first.SessionState.RemainingSeconds != 600 {
		t.Fatalf("initial session state=%+v err=%v", first.SessionState, err)
	}
	initialAnchor, available := agentStore.CurrentConfirmedSessionState()
	if !available || initialAnchor.SessionID != "session-7" || initialAnchor.RemainingSeconds != 600 {
		t.Fatalf("stored initial anchor=%+v available=%t", initialAnchor, available)
	}
	if err := agentStore.CheckpointUsage(ctx, agentstorage.DailyUsage{
		LocalDate: now.Format("2006-01-02"), SecondsUsed: 1, CheckpointAt: now.Add(3 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	unchanged, err := client.Heartbeat(ctx, now.Add(4*time.Second))
	if err != nil || unchanged.SessionState != nil {
		t.Fatalf("unchanged heartbeat unexpectedly reset anchor=%+v err=%v", unchanged.SessionState, err)
	}
	afterUnchanged, _ := agentStore.CurrentConfirmedSessionState()
	if afterUnchanged != initialAnchor {
		t.Fatalf("unchanged heartbeat changed anchor: before=%+v after=%+v", initialAnchor, afterUnchanged)
	}

	if _, err := serverStore.QueueRemoteBonus(ctx, device.ID, 300, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	changed, err := client.Heartbeat(ctx, now.Add(6*time.Second))
	if err != nil || changed.SessionState == nil || changed.SessionState.RemainingSeconds != 899 {
		t.Fatalf("bonus session state=%+v err=%v", changed.SessionState, err)
	}
	bonusAnchor, _ := agentStore.CurrentConfirmedSessionState()
	if bonusAnchor.RemainingSeconds != 899 || bonusAnchor.Revision <= initialAnchor.Revision {
		t.Fatalf("bonus did not create a new anchor: initial=%+v bonus=%+v", initialAnchor, bonusAnchor)
	}
	unchangedAgain, err := client.Heartbeat(ctx, now.Add(7*time.Second))
	if err != nil || unchangedAgain.SessionState != nil {
		t.Fatalf("post-bonus heartbeat reset anchor=%+v err=%v", unchangedAgain.SessionState, err)
	}

	client.SetGraphicalSession(false, "", false)
	if _, err := client.Heartbeat(ctx, now.Add(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	storedDevice, _, err := serverStore.LoadDevice(ctx, device.ID)
	if err != nil || storedDevice.GraphicalSessionActive || storedDevice.GraphicalSessionID != "" {
		t.Fatalf("server graphical presence=%+v err=%v", storedDevice, err)
	}
}

func TestThirtyMinuteBonusReplacesOneMinuteAnchorOnlyAfterDurableApplication(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 13, 23, 0, 0, 0, time.Local)
	serverStore := openServerTestStore(t, ctx, filepath.Join(t.TempDir(), "server.db"), now)
	defer serverStore.Close()
	device, err := serverStore.CreateDevice(ctx, "One minute left", now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := serverStore.IssueDeviceToken(ctx, device.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	var quotas [7]int64
	quotas[now.Weekday()] = 600
	if err := serverStore.SaveQuotas(ctx, device.ID, quotas, 10, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	application, err := web.New(serverStore, false, time.Hour, time.Minute, 3*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	agentStore, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()
	client, err := New(agentStore, &http.Client{Transport: handlerTransport{handler: application}}, Config{
		ServerURL: "http://tempo.test", DeviceID: device.ID, DeviceToken: token, InstallationID: testInstallationID, HeartbeatInterval: time.Second, AttemptTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	client.SetGraphicalSession(true, "session-one-minute", false)
	if _, err := client.Heartbeat(ctx, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := agentStore.CheckpointUsage(ctx, agentstorage.DailyUsage{
		LocalDate: now.Format("2006-01-02"), SecondsUsed: 540, CheckpointAt: now.Add(3 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Heartbeat(ctx, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	operationID, err := serverStore.QueueRemoteBonus(ctx, device.ID, 30*60, now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	changed, err := client.Heartbeat(ctx, now.Add(6*time.Second))
	if err != nil || changed.SessionState == nil || changed.SessionState.RemainingSeconds != 31*60 {
		t.Fatalf("bonus response state=%+v err=%v", changed.SessionState, err)
	}
	anchor, available := agentStore.CurrentConfirmedSessionState()
	if !available || anchor.RemainingSeconds != 31*60 || anchor.UsageSeconds != 540 {
		t.Fatalf("bonus anchor=%+v available=%t", anchor, available)
	}
	if _, err := client.Heartbeat(ctx, now.Add(7*time.Second)); err != nil {
		t.Fatal(err)
	}
	acknowledged, err := serverStore.RemoteBonusAcknowledged(ctx, device.ID, operationID)
	if err != nil || !acknowledged {
		t.Fatalf("bonus acknowledgement=%t err=%v", acknowledged, err)
	}
}

func TestInvalidSessionStateCannotAcknowledgeBonusCommand(t *testing.T) {
	ctx := context.Background()
	store, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, time.August, 13, 23, 0, 0, 0, time.Local)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"server_time":"2026-08-14T02:00:00Z",
			"policy":{"revision":1,"warning_minutes":10,"weekly_quota_seconds":[0,0,0,0,0,0,0],"updated_at":"2026-08-14T02:00:00Z"},
			"session_state":{"revision":1,"session_id":"session","local_date":"2026-08-13","remaining_seconds":-1,"usage_seconds":0,"confirmed_at":"2026-08-14T02:00:00Z"},
			"commands":[{"id":"bonus-command","kind":"add_bonus","payload":{"uuid":"bonus-command","seconds":1800,"origin":"web"},"created_at":"2026-08-14T02:00:00Z"}],
			"control":{}
		}`))
	})
	client, err := New(store, &http.Client{Transport: handlerTransport{handler: handler}}, Config{
		ServerURL: "http://tempo.test", DeviceID: "device", DeviceToken: "token", InstallationID: testInstallationID, HeartbeatInterval: time.Second, AttemptTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	client.SetGraphicalSession(true, "session", false)
	if _, err := client.Heartbeat(ctx, now); err == nil {
		t.Fatal("invalid session state unexpectedly succeeded")
	}
	applied, err := store.AppliedCommandIDs(ctx, 10)
	if err != nil || len(applied) != 0 {
		t.Fatalf("command acknowledged before balance anchor: ids=%v err=%v", applied, err)
	}
}

func TestLegacyDatedBonusUsesHeartbeatLocalDay(t *testing.T) {
	ctx := context.Background()
	store, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"server_time":"2026-08-14T02:00:00Z",
			"commands":[{"id":"legacy-bonus","kind":"add_bonus","payload":{"uuid":"legacy-bonus","local_date":"2026-08-12","seconds":1800,"origin":"web"},"created_at":"2026-08-14T02:00:00Z"}],
			"control":{}
		}`))
	})
	client, err := New(store, &http.Client{Transport: handlerTransport{handler: handler}}, Config{
		ServerURL: "http://tempo.test", DeviceID: "device", DeviceToken: "token", InstallationID: testInstallationID, HeartbeatInterval: time.Second, AttemptTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Heartbeat(ctx, time.Date(2026, 8, 13, 23, 59, 59, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	currentDay, err := store.TotalBonusSeconds(ctx, "2026-08-13")
	if err != nil {
		t.Fatal(err)
	}
	legacyDay, err := store.TotalBonusSeconds(ctx, "2026-08-12")
	if err != nil || currentDay != 1800 || legacyDay != 0 {
		t.Fatalf("bonus days current=%d legacy=%d err=%v", currentDay, legacyDay, err)
	}
}

func TestLocalPasswordChangesOnlyAfterSuccessfulSynchronization(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.Local)
	serverStore := openServerTestStore(t, ctx, filepath.Join(t.TempDir(), "server.db"), now)
	defer serverStore.Close()
	device, err := serverStore.CreateDevice(ctx, "Password sync", now)
	if err != nil {
		t.Fatal(err)
	}
	deviceToken, err := serverStore.IssueDeviceToken(ctx, device.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	passwordParameters := localauth.Argon2Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}
	oldVerifier, err := localauth.HashPassword("old-password", passwordParameters)
	if err != nil {
		t.Fatal(err)
	}
	newVerifier, err := localauth.HashPassword("new-password", passwordParameters)
	if err != nil {
		t.Fatal(err)
	}
	if err := serverStore.SetLocalPassword(ctx, device.ID, oldVerifier, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	application, err := web.New(serverStore, false, time.Hour, time.Minute, 3*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	var serverOnline uint32 = 1
	httpClient := &http.Client{Transport: handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadUint32(&serverOnline) == 0 {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		application.ServeHTTP(w, r)
	})}}
	agentStore, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()
	synchronizationClient, err := New(agentStore, httpClient, Config{
		ServerURL: "http://tempo.test", DeviceID: device.ID,
		DeviceToken: deviceToken, InstallationID: testInstallationID,
		HeartbeatInterval: 10 * time.Second, AttemptTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := synchronizationClient.Heartbeat(ctx, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}

	if err := serverStore.SetLocalPassword(ctx, device.ID, newVerifier, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	atomic.StoreUint32(&serverOnline, 0)
	if _, err := synchronizationClient.Heartbeat(ctx, now.Add(4*time.Second)); err == nil {
		t.Fatal("offline password synchronization unexpectedly succeeded")
	}
	offlinePolicy, err := agentStore.LoadPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldPasswordValid, _ := localauth.VerifyPassword("old-password", offlinePolicy.LocalPasswordVerifier)
	newPasswordValidOffline, _ := localauth.VerifyPassword("new-password", offlinePolicy.LocalPasswordVerifier)
	if !oldPasswordValid || newPasswordValidOffline {
		t.Fatal("offline client did not preserve the old password verifier")
	}

	atomic.StoreUint32(&serverOnline, 1)
	if _, err := synchronizationClient.Heartbeat(ctx, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	onlinePolicy, err := agentStore.LoadPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldPasswordValid, _ = localauth.VerifyPassword("old-password", onlinePolicy.LocalPasswordVerifier)
	newPasswordValid, _ := localauth.VerifyPassword("new-password", onlinePolicy.LocalPasswordVerifier)
	if oldPasswordValid || !newPasswordValid {
		t.Fatal("successful synchronization did not replace the password verifier")
	}
}

type handlerTransport struct{ handler http.Handler }

func openServerTestStore(t *testing.T, ctx context.Context, path string, now time.Time) *serverstorage.Store {
	t.Helper()
	store, err := serverstorage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.BootstrapAdmin(ctx, "sync-test-owner", "test-hash", now)
	if err != nil || !created {
		_ = store.Close()
		t.Fatalf("bootstrap server fixture created=%t err=%v", created, err)
	}
	return store
}

func (transport handlerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	transport.handler.ServeHTTP(recorder, request)
	return recorder.Result(), nil
}

type recoveryTransport struct {
	attempts   uint32
	idleCloses uint32
}

func (transport *recoveryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if atomic.AddUint32(&transport.attempts, 1) == 1 {
		<-request.Context().Done()
		return nil, request.Context().Err()
	}
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Content-Type", "application/json")
	_, _ = recorder.Write([]byte(`{}`))
	return recorder.Result(), nil
}

func (transport *recoveryTransport) CloseIdleConnections() {
	atomic.AddUint32(&transport.idleCloses, 1)
}

type failingTransport struct{ failure error }

func TestHeartbeatFailureDiscardsRemoteControl(t *testing.T) {
	ctx := context.Background()
	store, err := agentstorage.Open(ctx, filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var online uint32 = 1
	transport := handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.LoadUint32(&online) == 0 {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"server_time":"2026-08-10T12:00:00Z","control":{"revision":2,"monitoring_paused":false,"manual_block":true}}`))
	})}
	client, err := New(store, &http.Client{Transport: transport}, Config{
		ServerURL: "http://tempo.test", DeviceID: "device", DeviceToken: "token", InstallationID: testInstallationID,
		HeartbeatInterval: time.Second, AttemptTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.Local)
	if _, err := client.Heartbeat(ctx, now); err != nil {
		t.Fatal(err)
	}
	if active, paused, blocked, revision := client.RemoteControl(); !active || paused || !blocked || revision != 2 {
		t.Fatalf("online control active=%t paused=%t blocked=%t revision=%d", active, paused, blocked, revision)
	}
	atomic.StoreUint32(&online, 0)
	if _, err := client.Heartbeat(ctx, now.Add(time.Second)); err == nil {
		t.Fatal("offline heartbeat succeeded")
	}
	if active, paused, blocked, revision := client.RemoteControl(); active || paused || blocked || revision != 0 {
		t.Fatalf("stale control active=%t paused=%t blocked=%t revision=%d", active, paused, blocked, revision)
	}
}

func (transport failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	if transport.failure == nil {
		return nil, errors.New("transport failed")
	}
	return nil, transport.failure
}
