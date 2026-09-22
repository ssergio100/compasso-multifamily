package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	protocol "github.com/ssergio100/compasso/protocol/v1"
)

func TestDeviceAuthenticationAndDuplicateHeartbeatAreIdempotent(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	device, err := store.CreateDevice(ctx, "Zorin", now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.IssueDeviceToken(ctx, device.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AuthenticateDevice(ctx, device.ID, token); err != nil {
		t.Fatalf("valid device credential rejected: %v", err)
	}
	admin, err := store.AdminByLogin(ctx, "test-owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SetFamilyState(ctx, admin.FamilyID, "suspended", now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := store.AuthenticateDevice(ctx, device.ID, "wrong"); !errors.Is(err, ErrInvalidDeviceCredentials) {
		t.Fatalf("suspended family exposed before credential verification: %v", err)
	}
	if err := store.AuthenticateDevice(ctx, device.ID, token); !errors.Is(err, ErrFamilySuspended) {
		t.Fatalf("suspended family credential error=%v", err)
	}
	if _, _, err := store.SetFamilyState(ctx, admin.FamilyID, "active", now.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := store.AuthenticateDevice(ctx, device.ID, "wrong"); !errors.Is(err, ErrInvalidDeviceCredentials) {
		t.Fatalf("wrong credential error=%v", err)
	}
	if err := store.RevokeDeviceToken(ctx, device.ID, now.Add(3*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := store.AuthenticateDevice(ctx, device.ID, token); !errors.Is(err, ErrInvalidDeviceCredentials) {
		t.Fatalf("revoked credential error=%v", err)
	}
	token, err = store.IssueDeviceToken(ctx, device.ID, now.Add(4*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(protocol.BonusPayload{LocalDate: "2026-08-10", Seconds: 300, Origin: "local"})
	heartbeat := protocol.HeartbeatRequest{
		PolicyRevision: 0, LocalDate: "2026-08-10", SecondsUsed: 120,
		Events: []protocol.PendingEvent{{
			UUID: "local-bonus-1", Kind: "bonus_added", Payload: payload, CreatedAt: now,
		}},
	}
	first, err := store.ReceiveHeartbeat(ctx, device.ID, heartbeat, now.Add(time.Second))
	if err != nil || first.Policy == nil || first.Policy.Revision != 1 || len(first.AcknowledgedEvents) != 1 {
		t.Fatalf("first heartbeat response=%+v err=%v", first, err)
	}
	var firstUsageSync string
	if err := store.db.QueryRowContext(ctx, `
		SELECT last_sync_at FROM daily_usage WHERE device_id=? AND local_date=?`, device.ID, heartbeat.LocalDate,
	).Scan(&firstUsageSync); err != nil {
		t.Fatal(err)
	}
	heartbeat.PolicyRevision = 1
	heartbeat.SecondsUsed = 60 // An older absolute checkpoint cannot reduce usage.
	second, err := store.ReceiveHeartbeat(ctx, device.ID, heartbeat, now.Add(2*time.Second))
	if err != nil || second.Policy != nil || len(second.AcknowledgedEvents) != 1 {
		t.Fatalf("duplicate heartbeat response=%+v err=%v", second, err)
	}
	var duplicateUsageSync string
	if err := store.db.QueryRowContext(ctx, `
		SELECT last_sync_at FROM daily_usage WHERE device_id=? AND local_date=?`, device.ID, heartbeat.LocalDate,
	).Scan(&duplicateUsageSync); err != nil || duplicateUsageSync != firstUsageSync {
		t.Fatalf("unchanged usage was rewritten first=%q duplicate=%q err=%v", firstUsageSync, duplicateUsageSync, err)
	}
	summary, err := store.LoadDailySummary(ctx, device.ID, "2026-08-10")
	if err != nil || summary.UsedSeconds != 120 || summary.BonusSeconds != 300 {
		t.Fatalf("duplicate changed summary=%+v err=%v", summary, err)
	}
	latestLocalDate, err := store.LatestHeartbeatLocalDate(ctx, device.ID)
	if err != nil || latestLocalDate != "2026-08-10" {
		t.Fatalf("latest heartbeat local date=%q err=%v", latestLocalDate, err)
	}
	events, err := store.ListAudit(ctx, device.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	bonusEvents := 0
	for _, event := range events {
		if event.Kind == "bonus_added" && event.Origin == "local" {
			bonusEvents++
		}
	}
	if bonusEvents != 1 {
		t.Fatalf("duplicate heartbeat created %d bonus audit events", bonusEvents)
	}
}

func TestHeartbeatPersistsCanonicalAgentCapabilities(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	device, err := store.CreateDevice(ctx, "Windows", now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		LocalDate: "2026-08-10",
		Capabilities: []string{
			protocol.UnlockAuthenticationCapability,
			protocol.SessionLockCapability,
			protocol.SessionLockCapability,
		},
	}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	stored, _, err := store.LoadDevice(ctx, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.SupportsCapability(protocol.SessionLockCapability) ||
		!stored.SupportsCapability(protocol.UnlockAuthenticationCapability) || len(stored.AgentCapabilities) != 2 {
		t.Fatalf("stored capabilities=%v", stored.AgentCapabilities)
	}
	invalid := protocol.HeartbeatRequest{LocalDate: "2026-08-10", Capabilities: []string{"INVALID capability"}}
	if _, err := store.ReceiveHeartbeat(ctx, device.ID, invalid, now.Add(2*time.Second)); err == nil {
		t.Fatal("invalid capability was accepted")
	}
}

func TestInstallationBindingAndCredentialRotationAreAtomic(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	device, err := store.CreateDevice(ctx, "Vínculo", now)
	if err != nil {
		t.Fatal(err)
	}
	firstToken, err := store.IssueDeviceToken(ctx, device.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	const firstInstallation = "79e78a4f-713b-4e19-a756-b60a0505248a"
	const secondInstallation = "1c5b1779-953c-4ebd-8022-a9b6b99c243e"
	if _, err := store.ReceiveHeartbeatForInstallation(
		ctx, device.ID, firstInstallation, 30*time.Second,
		protocol.HeartbeatRequest{LocalDate: "invalid"}, now,
	); err == nil {
		t.Fatal("invalid heartbeat occupied installation binding")
	}
	var active string
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(active_installation_id, '') FROM device WHERE id=?`, device.ID).Scan(&active); err != nil || active != "" {
		t.Fatalf("binding after invalid payload=%q err=%v", active, err)
	}
	if _, err := store.ReceiveHeartbeatForInstallation(
		ctx, device.ID, firstInstallation, 30*time.Second,
		protocol.HeartbeatRequest{LocalDate: "2026-08-10"}, now,
	); err != nil {
		t.Fatal(err)
	}
	var generation int64
	var onlineUntil string
	if err := store.db.QueryRowContext(ctx, `
		SELECT active_installation_id, credential_generation, online_until FROM device WHERE id=?`, device.ID,
	).Scan(&active, &generation, &onlineUntil); err != nil || active != firstInstallation || generation != 1 || onlineUntil != formatTime(now.Add(2*time.Minute)) {
		t.Fatalf("first binding active=%q generation=%d online_until=%q err=%v", active, generation, onlineUntil, err)
	}
	if err := store.CheckDeviceInstallation(ctx, device.ID, secondInstallation); !errors.Is(err, ErrInstallationConflict) {
		t.Fatalf("concurrent preflight error=%v", err)
	}
	if _, err := store.ReceiveHeartbeatForInstallation(
		ctx, device.ID, secondInstallation, 30*time.Second,
		protocol.HeartbeatRequest{LocalDate: "2026-08-10"}, now.Add(time.Second),
	); !errors.Is(err, ErrInstallationConflict) {
		t.Fatalf("concurrent binding error=%v", err)
	}
	secondToken, err := store.IssueDeviceToken(ctx, device.ID, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AuthenticateDevice(ctx, device.ID, firstToken); !errors.Is(err, ErrInvalidDeviceCredentials) {
		t.Fatalf("old token error=%v", err)
	}
	if err := store.AuthenticateDevice(ctx, device.ID, secondToken); err != nil {
		t.Fatalf("new token error=%v", err)
	}
	if err := store.db.QueryRowContext(ctx, `
		SELECT COALESCE(active_installation_id, ''), credential_generation, COALESCE(online_until, '')
		FROM device WHERE id=?`, device.ID,
	).Scan(&active, &generation, &onlineUntil); err != nil || active != "" || generation != 2 || onlineUntil != "" {
		t.Fatalf("rotation active=%q generation=%d online_until=%q err=%v", active, generation, onlineUntil, err)
	}
	if _, err := store.ReceiveHeartbeatForInstallation(
		ctx, device.ID, secondInstallation, 5*time.Second,
		protocol.HeartbeatRequest{LocalDate: "2026-08-10"}, now.Add(3*time.Second),
	); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeDeviceToken(ctx, device.ID, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `
		SELECT COALESCE(active_installation_id, ''), credential_generation FROM device WHERE id=?`, device.ID,
	).Scan(&active, &generation); err != nil || active != "" || generation != 3 {
		t.Fatalf("revocation active=%q generation=%d err=%v", active, generation, err)
	}
}

func TestRevisionAheadErrorCarriesBothRevisions(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	device, err := store.CreateDevice(ctx, "New enrollment", now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: 9,
		LocalDate:      "2026-08-10",
	}, now.Add(time.Second))
	var revisionError *RevisionAheadError
	if !errors.As(err, &revisionError) {
		t.Fatalf("revision conflict error=%v", err)
	}
	if revisionError.ClientRevision != 9 || revisionError.ServerRevision != 1 {
		t.Fatalf("revision conflict=%+v", revisionError)
	}
}

func TestCommandsRemainPendingUntilAcknowledged(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	device, err := store.CreateDevice(ctx, "Zorin", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueRemoteBonus(ctx, device.ID, 600, now); err != nil {
		t.Fatal(err)
	}
	request := protocol.HeartbeatRequest{PolicyRevision: 1, LocalDate: "2026-08-10"}
	first, err := store.ReceiveHeartbeat(ctx, device.ID, request, now.Add(time.Second))
	if err != nil || len(first.Commands) != 1 || first.Commands[0].Kind != "add_bonus" {
		t.Fatalf("pending commands=%+v err=%v", first.Commands, err)
	}
	activity, err := store.LoadDeviceActivity(ctx, device.ID, first.Commands[0].ID)
	offered := findActivityStep(activity.Steps, "offered")
	if err != nil || activity.Status != "offered" || offered == nil || offered.Occurrences != 1 {
		t.Fatalf("first delivery activity=%+v err=%v", activity, err)
	}
	second, err := store.ReceiveHeartbeat(ctx, device.ID, request, now.Add(2*time.Second))
	if err != nil || len(second.Commands) != 1 {
		t.Fatalf("unacknowledged command was not redelivered: %+v err=%v", second.Commands, err)
	}
	activity, err = store.LoadDeviceActivity(ctx, device.ID, first.Commands[0].ID)
	offered = findActivityStep(activity.Steps, "offered")
	if err != nil || offered == nil || offered.Occurrences != 2 {
		t.Fatalf("second delivery activity=%+v err=%v", activity, err)
	}
	request.CommandAcks = []string{first.Commands[0].ID}
	third, err := store.ReceiveHeartbeat(ctx, device.ID, request, now.Add(3*time.Second))
	if err != nil || len(third.Commands) != 0 {
		t.Fatalf("acknowledged command still pending: %+v err=%v", third.Commands, err)
	}
	activity, err = store.LoadDeviceActivity(ctx, device.ID, first.Commands[0].ID)
	offered = findActivityStep(activity.Steps, "offered")
	if err != nil || activity.Status != "completed" || activity.CompletedAt == nil || offered == nil || offered.Occurrences != 2 || findActivityStep(activity.Steps, "completed") == nil {
		t.Fatalf("completed activity=%+v err=%v", activity, err)
	}
}

func TestUnknownHistoricalCommandAcknowledgementIsIgnored(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 24, 3, 0, 0, 0, time.UTC)
	device, err := store.CreateDevice(ctx, "Zorin", now)
	if err != nil {
		t.Fatal(err)
	}

	response, err := store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision,
		LocalDate:      "2026-08-24",
		CommandAcks:    []string{"historical-command-no-longer-on-server"},
	}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("historical acknowledgement rejected the heartbeat: %v", err)
	}
	if len(response.Commands) != 0 {
		t.Fatalf("unexpected commands after historical acknowledgement: %+v", response.Commands)
	}
}

func TestAcknowledgementRemainsHarmlessAfterHumanHistoryExpires(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 24, 3, 0, 0, 0, time.UTC)
	device, err := store.CreateDevice(ctx, "Zorin", now)
	if err != nil {
		t.Fatal(err)
	}
	commandID, err := store.QueueControlOperation(ctx, device.ID, "pause_monitoring", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision,
		LocalDate:      "2026-08-24",
		CommandAcks:    []string{commandID},
	}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if deleted, err := store.CleanupExpiredCompletedActivities(
		ctx, now.AddDate(0, 0, completedActivityRetentionDays+1),
	); err != nil || deleted != 2 {
		t.Fatalf("expired activity cleanup deleted=%d err=%v", deleted, err)
	}
	if _, err := store.LoadDeviceActivity(ctx, device.ID, commandID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired activity remained visible: %v", err)
	}

	if _, err := store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision,
		LocalDate:      "2026-09-24",
		CommandAcks:    []string{commandID},
	}, now.AddDate(0, 0, completedActivityRetentionDays+1).Add(time.Second)); err != nil {
		t.Fatalf("acknowledgement for command with expired activity rejected heartbeat: %v", err)
	}
}

func TestPendingCommandDeliveryDoesNotDependOnHumanHistory(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 24, 3, 0, 0, 0, time.UTC)
	device, err := store.CreateDevice(ctx, "Zorin", now)
	if err != nil {
		t.Fatal(err)
	}
	commandID, err := store.QueueRemoteBonus(ctx, device.ID, 15*60, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM activity WHERE id=?`, commandID); err != nil {
		t.Fatal(err)
	}

	response, err := store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision,
		LocalDate:      "2026-08-24",
	}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("missing human history rejected command delivery: %v", err)
	}
	if len(response.Commands) != 1 || response.Commands[0].ID != commandID {
		t.Fatalf("pending command was not delivered: %+v", response.Commands)
	}
}

func TestRemoteControlDoesNotBecomeOfflinePolicy(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	device, err := store.CreateDevice(ctx, "Zorin", now)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.loadPolicy(ctx, device.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, command := range []string{"block_now", "pause_monitoring"} {
		if err := store.QueueControl(ctx, device.ID, command, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		after, err := store.loadPolicy(ctx, device.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Revision != before.Revision || after.ManualBlock || after.MonitoringPaused {
			t.Fatalf("remote command %q changed offline policy: before=%+v after=%+v", command, before, after)
		}
	}
	heartbeat, err := store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: before.Revision, LocalDate: "2026-08-10",
	}, now.Add(2*time.Second))
	if err != nil || heartbeat.Control.ManualBlock || !heartbeat.Control.MonitoringPaused ||
		len(heartbeat.Commands) != 1 || heartbeat.Commands[0].Kind != "pause_monitoring" {
		t.Fatalf("heartbeat control=%+v err=%v", heartbeat.Control, err)
	}
}

func TestControlQueueKeepsOnlyLatestDesiredState(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	device, err := store.CreateDevice(ctx, "Trabalho", now)
	if err != nil {
		t.Fatal(err)
	}

	pauseID, err := store.QueueControlOperation(ctx, device.ID, "pause_monitoring", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	duplicateID, err := store.QueueControlOperation(ctx, device.ID, "pause_monitoring", now.Add(2*time.Second))
	if err != nil || duplicateID != pauseID {
		t.Fatalf("duplicate pause id=%q want=%q err=%v", duplicateID, pauseID, err)
	}
	control, err := store.LoadControl(ctx, device.ID)
	if err != nil || control.Revision != 2 || !control.MonitoringPaused || control.ManualBlock {
		t.Fatalf("duplicate pause changed control=%+v err=%v", control, err)
	}
	blockID, err := store.QueueControlOperation(ctx, device.ID, "block_now", now.Add(3*time.Second))
	if err != nil || blockID == pauseID {
		t.Fatalf("replacement block id=%q old=%q err=%v", blockID, pauseID, err)
	}
	if _, err := store.LoadDeviceActivity(ctx, device.ID, pauseID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("superseded pause activity remained: %v", err)
	}

	response, err := store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision, LocalDate: "2026-08-24",
	}, now.Add(4*time.Second))
	if err != nil || len(response.Commands) != 1 || response.Commands[0].ID != blockID ||
		response.Control.MonitoringPaused || !response.Control.ManualBlock {
		t.Fatalf("latest block response=%+v err=%v", response, err)
	}

	latestPauseID, err := store.QueueControlOperation(ctx, device.ID, "pause_monitoring", now.Add(5*time.Second))
	if err != nil || latestPauseID == blockID {
		t.Fatalf("replacement pause id=%q old=%q err=%v", latestPauseID, blockID, err)
	}
	if _, err := store.LoadDeviceActivity(ctx, device.ID, blockID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("superseded offered block activity remained: %v", err)
	}
	response, err = store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision, LocalDate: "2026-08-24",
	}, now.Add(6*time.Second))
	if err != nil || len(response.Commands) != 1 || response.Commands[0].ID != latestPauseID ||
		!response.Control.MonitoringPaused || response.Control.ManualBlock {
		t.Fatalf("latest pause response=%+v err=%v", response, err)
	}

	var controlAudits int
	if err := store.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM audit_event
		WHERE device_id=? AND kind IN (
			'pause_monitoring', 'resume_monitoring', 'block_now', 'clear_manual_block'
		)`, device.ID).Scan(&controlAudits); err != nil || controlAudits != 0 {
		t.Fatalf("control audit count=%d err=%v", controlAudits, err)
	}
}

func TestAcknowledgedControlCommandKeepsActivityAndDeletesEnvelope(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	device, err := store.CreateDevice(ctx, "Trabalho", now)
	if err != nil {
		t.Fatal(err)
	}
	commandID, err := store.QueueControlOperation(ctx, device.ID, "block_now", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision, LocalDate: "2026-08-24",
	}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision, LocalDate: "2026-08-24", CommandAcks: []string{commandID},
	}, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}

	var envelopes int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_command WHERE id=?`, commandID).Scan(&envelopes); err != nil || envelopes != 0 {
		t.Fatalf("acknowledged control envelope count=%d err=%v", envelopes, err)
	}
	activity, err := store.LoadDeviceActivity(ctx, device.ID, commandID)
	if err != nil || activity.Status != "completed" || findActivityStep(activity.Steps, "completed") == nil {
		t.Fatalf("completed control activity=%+v err=%v", activity, err)
	}
}

func TestHeartbeatRejectsUnexpectedAuditFields(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	device, err := store.CreateDevice(ctx, "Zorin", now)
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"local_date":"2026-08-10","seconds":300,"origin":"local","device_token":"must-not-be-stored"}`)
	_, err = store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: 1, LocalDate: "2026-08-10",
		Events: []protocol.PendingEvent{{UUID: "unexpected-field", Kind: "bonus_added", Payload: payload, CreatedAt: now}},
	}, now)
	if err == nil {
		t.Fatal("event with an unexpected sensitive field was accepted")
	}
	events, listError := store.ListAudit(ctx, device.ID, 20)
	if listError != nil {
		t.Fatal(listError)
	}
	for _, event := range events {
		if strings.Contains(event.Details, "must-not-be-stored") {
			t.Fatalf("audit stored a sensitive field: %+v", event)
		}
	}
}
