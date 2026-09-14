package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	protocol "github.com/ssergio100/compasso/protocol/v1"
)

var (
	ErrInvalidDeviceCredentials = errors.New("invalid device credentials")
	ErrFamilySuspended          = errors.New("family suspended")
	ErrRevisionAhead            = errors.New("client policy revision is ahead of server")
)

// RevisionAheadError identifies stale state from another server/device
// enrollment without exposing credentials. Callers can present both revisions
// instead of reducing this condition to an opaque HTTP conflict.
type RevisionAheadError struct {
	ClientRevision int64
	ServerRevision int64
}

func (e *RevisionAheadError) Error() string {
	return fmt.Sprintf("%s: client=%d server=%d", ErrRevisionAhead, e.ClientRevision, e.ServerRevision)
}

func (e *RevisionAheadError) Is(target error) bool { return target == ErrRevisionAhead }

// IssueDeviceToken replaces a device credential and returns the secret once.
// Only its SHA-256 digest is persisted; the token has 256 bits of entropy.
func (s *Store) IssueDeviceToken(ctx context.Context, deviceID string, now time.Time) (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate device token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	digest := sha256.Sum256([]byte(token))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE device SET device_token_hash=?, updated_at=? WHERE id=?`,
		hex.EncodeToString(digest[:]), formatTime(now), deviceID)
	if err != nil {
		return "", fmt.Errorf("store device token: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return "", ErrNotFound
	}
	if err := insertAudit(ctx, tx, deviceID, "device_token_issued", map[string]string{"status": "replaced"}, now); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

// RevokeDeviceToken immediately rejects the current device credential without
// creating a replacement secret.
func (s *Store) RevokeDeviceToken(ctx context.Context, deviceID string, now time.Time) error {
	if deviceID == "" || now.IsZero() {
		return errors.New("device id and revocation time are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE device SET device_token_hash='', updated_at=? WHERE id=?`, formatTime(now), deviceID)
	if err != nil {
		return fmt.Errorf("revoke device token: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrNotFound
	}
	if err := insertAudit(ctx, tx, deviceID, "device_token_revoked", map[string]string{"status": "revoked"}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AuthenticateDevice(ctx context.Context, deviceID, token string) error {
	if !validOpaqueIdentifier(deviceID) || len(token) != 43 {
		return ErrInvalidDeviceCredentials
	}
	var stored, familyState string
	err := s.db.QueryRowContext(ctx, `
		SELECT d.device_token_hash, f.state
		FROM device d
		JOIN family f ON f.id=d.family_id
		WHERE d.id=?`, deviceID).Scan(&stored, &familyState)
	if errors.Is(err, sql.ErrNoRows) || stored == "" {
		return ErrInvalidDeviceCredentials
	}
	if err != nil {
		return fmt.Errorf("load device credential: %w", err)
	}
	digest := sha256.Sum256([]byte(token))
	provided := hex.EncodeToString(digest[:])
	if len(stored) != len(provided) || subtle.ConstantTimeCompare([]byte(stored), []byte(provided)) != 1 {
		return ErrInvalidDeviceCredentials
	}
	if familyState != "active" {
		return ErrFamilySuspended
	}
	return nil
}

func (s *Store) ReceiveHeartbeat(ctx context.Context, deviceID string, request protocol.HeartbeatRequest, now time.Time) (protocol.HeartbeatResponse, error) {
	const maximumDailyUsageSeconds = int64((48 * time.Hour) / time.Second)
	if !validOpaqueIdentifier(deviceID) || request.PolicyRevision < 0 || request.ControlRevision < 0 || request.SessionStateRevision < 0 ||
		request.SecondsUsed < 0 || request.SecondsUsed > maximumDailyUsageSeconds || now.IsZero() {
		return protocol.HeartbeatResponse{}, errors.New("invalid heartbeat counters")
	}
	if request.GraphicalSessionActive {
		if !validOpaqueIdentifier(request.GraphicalSessionID) {
			return protocol.HeartbeatResponse{}, errors.New("active graphical session requires a valid identifier")
		}
	} else if request.GraphicalSessionID != "" || request.RequestSessionState {
		return protocol.HeartbeatResponse{}, errors.New("inactive graphical session cannot request session state")
	}
	if request.GraphicalSessionLocked && !request.GraphicalSessionActive {
		return protocol.HeartbeatResponse{}, errors.New("inactive graphical session cannot be locked")
	}
	parsedDate, err := time.Parse("2006-01-02", request.LocalDate)
	if err != nil || parsedDate.Format("2006-01-02") != request.LocalDate {
		return protocol.HeartbeatResponse{}, errors.New("invalid heartbeat local date")
	}
	if len(request.Events) > 100 || len(request.CommandAcks) > 100 {
		return protocol.HeartbeatResponse{}, errors.New("heartbeat batch exceeds 100 items")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.HeartbeatResponse{}, err
	}
	defer tx.Rollback()
	var serverRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT policy_revision FROM device WHERE id=?`, deviceID).Scan(&serverRevision); errors.Is(err, sql.ErrNoRows) {
		return protocol.HeartbeatResponse{}, ErrNotFound
	} else if err != nil {
		return protocol.HeartbeatResponse{}, err
	}
	if request.PolicyRevision > serverRevision || request.SessionStateRevision > serverRevision {
		clientRevision := request.PolicyRevision
		if request.SessionStateRevision > clientRevision {
			clientRevision = request.SessionStateRevision
		}
		return protocol.HeartbeatResponse{}, &RevisionAheadError{
			ClientRevision: clientRevision,
			ServerRevision: serverRevision,
		}
	}
	stamp := formatTime(now)
	if _, err := tx.ExecContext(ctx, `
		UPDATE device SET last_seen_at=?, applied_policy_revision=?, applied_control_revision=?,
			graphical_session_active=?, graphical_session_locked=?, graphical_session_id=?, updated_at=? WHERE id=?`,
		stamp, request.PolicyRevision, request.ControlRevision, boolInt(request.GraphicalSessionActive),
		boolInt(request.GraphicalSessionLocked), nullableSessionID(request.GraphicalSessionActive, request.GraphicalSessionID), stamp, deviceID); err != nil {
		return protocol.HeartbeatResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO daily_usage(device_id, local_date, seconds_used, last_sync_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(device_id, local_date) DO UPDATE SET
			seconds_used=MAX(daily_usage.seconds_used, excluded.seconds_used),
			last_sync_at=excluded.last_sync_at`, deviceID, request.LocalDate, request.SecondsUsed, stamp); err != nil {
		return protocol.HeartbeatResponse{}, fmt.Errorf("store heartbeat usage: %w", err)
	}
	if err := applyPendingRemoteBonuses(ctx, tx, deviceID, request.LocalDate); err != nil {
		return protocol.HeartbeatResponse{}, err
	}

	acknowledged := make([]string, 0, len(request.Events))
	for _, event := range request.Events {
		if err := receiveEvent(ctx, tx, deviceID, event, now); err != nil {
			return protocol.HeartbeatResponse{}, err
		}
		acknowledged = append(acknowledged, event.UUID)
	}
	acknowledgedCommands := make([]string, 0, len(request.CommandAcks))
	for _, commandID := range request.CommandAcks {
		if !validOpaqueIdentifier(commandID) {
			return protocol.HeartbeatResponse{}, errors.New("invalid command acknowledgement")
		}
		acknowledgedCommands = append(acknowledgedCommands, commandID)
		commandResult, err := tx.ExecContext(ctx, `
			UPDATE device_command SET acknowledged_at=COALESCE(acknowledged_at, ?)
			WHERE id=? AND device_id=?`, stamp, commandID, deviceID)
		if err != nil {
			return protocol.HeartbeatResponse{}, err
		}
		matchedCommand, err := commandResult.RowsAffected()
		if err != nil {
			return protocol.HeartbeatResponse{}, err
		}
		// Agents keep applied command identifiers durably and may acknowledge a
		// command after its server-side history expired or after a restore. These
		// historical acknowledgements were always harmless and must not reject the
		// entire heartbeat merely because no activity can be updated.
		if matchedCommand == 0 {
			continue
		}
		activityResult, err := tx.ExecContext(ctx, `
			UPDATE activity SET
				status='completed',
				completed_at=COALESCE(completed_at, ?),
				expires_at=COALESCE(expires_at, ?)
			WHERE id=? AND device_id=?`, stamp,
			formatTime(now.AddDate(0, 0, completedActivityRetentionDays)), commandID, deviceID)
		if err != nil {
			return protocol.HeartbeatResponse{}, err
		}
		matchedActivity, err := activityResult.RowsAffected()
		if err != nil {
			return protocol.HeartbeatResponse{}, err
		}
		if matchedActivity != 0 {
			if err := upsertActivityStep(ctx, tx, commandID, "completed", "device", now, nil, false); err != nil {
				return protocol.HeartbeatResponse{}, err
			}
		}
		// Current agents acknowledge control only after observing its graphical
		// effect. Legacy agents may acknowledge delivery alone. In either case the
		// human-facing activity retains the result; keeping the transport envelope
		// forever would make this table grow with every button press. Bonus commands
		// remain durable because they carry an additive business operation.
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM device_command
			WHERE id=? AND device_id=? AND kind IN (
				'pause_monitoring', 'resume_monitoring', 'block_now', 'clear_manual_block'
			)`, commandID, deviceID); err != nil {
			return protocol.HeartbeatResponse{}, err
		}
	}
	if _, err := cleanupExpiredCompletedActivitiesIfDue(ctx, tx, now); err != nil {
		return protocol.HeartbeatResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.HeartbeatResponse{}, err
	}

	response := protocol.HeartbeatResponse{
		ServerTime: now.UTC(), AcknowledgedEvents: acknowledged, AcknowledgedCommands: acknowledgedCommands,
	}
	control, err := s.loadControl(ctx, deviceID)
	if err != nil {
		return protocol.HeartbeatResponse{}, err
	}
	response.Control = protocol.Control{
		Revision: control.Revision, MonitoringPaused: control.MonitoringPaused, ManualBlock: control.ManualBlock,
	}
	if request.PolicyRevision < serverRevision {
		policy, err := s.loadPolicy(ctx, deviceID)
		if err != nil {
			return protocol.HeartbeatResponse{}, err
		}
		converted := policyForAPI(policy)
		response.Policy = &converted
	}
	if request.GraphicalSessionActive &&
		(request.RequestSessionState || request.SessionStateRevision < serverRevision) {
		state, err := s.confirmedSessionState(ctx, deviceID, request.GraphicalSessionID,
			request.LocalDate, now)
		if err != nil {
			return protocol.HeartbeatResponse{}, err
		}
		response.SessionState = &state
	}
	response.Commands, err = s.pendingCommands(ctx, deviceID, 100, now)
	if err != nil {
		return protocol.HeartbeatResponse{}, err
	}
	return response, nil
}

func receiveEvent(ctx context.Context, tx *sql.Tx, deviceID string, event protocol.PendingEvent, observedAt time.Time) error {
	if !validOpaqueIdentifier(event.UUID) || event.Kind != "bonus_added" || event.CreatedAt.IsZero() || len(event.Payload) == 0 || len(event.Payload) > 16<<10 || !json.Valid(event.Payload) {
		return errors.New("invalid pending event")
	}
	var bonus protocol.BonusPayload
	decoder := json.NewDecoder(bytes.NewReader(event.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bonus); err != nil || bonus.LocalDate == "" || bonus.Seconds <= 0 || bonus.Seconds > int64((12*time.Hour)/time.Second) || bonus.Origin != "local" {
		return errors.New("invalid local bonus event")
	}
	parsed, err := time.Parse("2006-01-02", bonus.LocalDate)
	if err != nil || parsed.Format("2006-01-02") != bonus.LocalDate {
		return errors.New("invalid bonus local date")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO bonus(uuid, device_id, local_date, seconds, origin, created_at)
		VALUES (?, ?, ?, ?, 'local', ?)`, event.UUID, deviceID, bonus.LocalDate, bonus.Seconds, formatTime(event.CreatedAt)); err != nil {
		return fmt.Errorf("store local bonus event: %w", err)
	}
	sanitizedPayload, err := json.Marshal(struct {
		LocalDate string `json:"local_date"`
		Seconds   int64  `json:"seconds"`
		Origin    string `json:"origin"`
	}{LocalDate: bonus.LocalDate, Seconds: bonus.Seconds, Origin: "local"})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO audit_event(uuid, device_id, kind, origin, payload_json, created_at)
		VALUES (?, ?, 'bonus_added', 'local', ?, ?)`, event.UUID, deviceID, string(sanitizedPayload), formatTime(event.CreatedAt)); err != nil {
		return fmt.Errorf("audit local bonus event: %w", err)
	}
	return insertLocalBonusActivity(ctx, tx, event.UUID, deviceID, bonus.Seconds, bonus.LocalDate, event.CreatedAt, observedAt)
}

// applyPendingRemoteBonuses turns queued credit increments into today's
// balance only when the controlled computer reports its current daily state.
// This keeps the command independent from the server's calendar and lets the
// normal daily rollover discard yesterday's remaining credit.
func applyPendingRemoteBonuses(ctx context.Context, tx *sql.Tx, deviceID, localDate string) error {
	queued, err := loadPendingRemoteCredits(ctx, tx, deviceID)
	if err != nil {
		return err
	}
	for _, credit := range queued {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO bonus(uuid, device_id, local_date, seconds, origin, created_at)
			VALUES (?, ?, ?, ?, 'web', ?)`, credit.id, deviceID, localDate, credit.seconds, formatTime(credit.createdAt)); err != nil {
			return fmt.Errorf("apply pending remote bonus: %w", err)
		}
	}
	return nil
}

func decodeCreditIncrementCommand(commandID string, payloadJSON []byte, fallbackCreatedAt time.Time) (protocol.CreditIncrementPayload, error) {
	var bonus protocol.CreditIncrementPayload
	if err := json.Unmarshal(payloadJSON, &bonus); err != nil || bonus.Seconds <= 0 ||
		bonus.Seconds > int64((24*time.Hour)/time.Second) || bonus.Origin != "web" {
		return protocol.CreditIncrementPayload{}, errors.New("invalid credit increment command")
	}
	if bonus.UUID == "" {
		bonus.UUID = commandID
	}
	if bonus.UUID != commandID {
		return protocol.CreditIncrementPayload{}, errors.New("credit increment command identifier mismatch")
	}
	if bonus.CreatedAt.IsZero() {
		bonus.CreatedAt = fallbackCreatedAt
	}
	return bonus, nil
}

type queryContext interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
}

type pendingRemoteCredit struct {
	id        string
	seconds   int64
	createdAt time.Time
}

func loadPendingRemoteCredits(ctx context.Context, queryer queryContext, deviceID string) ([]pendingRemoteCredit, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT c.id, c.payload_json, c.created_at
		FROM device_command c
		LEFT JOIN bonus b ON b.uuid=c.id
		WHERE c.device_id=? AND c.kind='add_bonus' AND c.acknowledged_at IS NULL AND b.uuid IS NULL
		ORDER BY c.created_at, c.id`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("load pending credit increments: %w", err)
	}
	defer rows.Close()
	var queued []pendingRemoteCredit
	for rows.Next() {
		var id, payloadJSON, created string
		if err := rows.Scan(&id, &payloadJSON, &created); err != nil {
			return nil, err
		}
		createdAt, err := parseTime(created)
		if err != nil {
			return nil, err
		}
		credit, err := decodeCreditIncrementCommand(id, []byte(payloadJSON), createdAt)
		if err != nil {
			return nil, err
		}
		queued = append(queued, pendingRemoteCredit{id: id, seconds: credit.Seconds, createdAt: credit.CreatedAt})
	}
	return queued, rows.Err()
}

func validOpaqueIdentifier(identifier string) bool {
	if len(identifier) == 0 || len(identifier) > 128 {
		return false
	}
	for _, character := range identifier {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func (s *Store) pendingCommands(ctx context.Context, deviceID string, limit int, now time.Time) ([]protocol.Command, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, payload_json, created_at FROM device_command
		WHERE device_id=? AND acknowledged_at IS NULL ORDER BY created_at, id LIMIT ?`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	var commands []protocol.Command
	for rows.Next() {
		var command protocol.Command
		var payload, created string
		if err := rows.Scan(&command.ID, &command.Kind, &payload, &created); err != nil {
			return nil, err
		}
		command.Payload = json.RawMessage(payload)
		command.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(commands) == 0 {
		return commands, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stamp := formatTime(now)
	for _, command := range commands {
		if _, err := tx.ExecContext(ctx, `
			UPDATE device_command SET
				delivery_attempts=delivery_attempts+1,
				first_offered_at=COALESCE(first_offered_at, ?),
				last_offered_at=?
			WHERE id=? AND device_id=? AND acknowledged_at IS NULL`,
			stamp, stamp, command.ID, deviceID); err != nil {
			return nil, fmt.Errorf("record command delivery attempt: %w", err)
		}
		activityResult, err := tx.ExecContext(ctx, `
			UPDATE activity SET status='offered'
			WHERE id=? AND device_id=? AND status!='completed'`, command.ID, deviceID)
		if err != nil {
			return nil, fmt.Errorf("update offered activity: %w", err)
		}
		matchedActivity, err := activityResult.RowsAffected()
		if err != nil {
			return nil, err
		}
		// Command delivery is business-critical; the human-facing history is a
		// projection and may be absent after retention or a repaired deployment.
		// Never reject a heartbeat only because there is no activity to annotate.
		if matchedActivity == 0 {
			continue
		}
		if err := upsertActivityStep(ctx, tx, command.ID, "offered", "server", now, nil, true); err != nil {
			return nil, fmt.Errorf("record activity delivery: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return commands, nil
}

func policyForAPI(policy Policy) protocol.Policy {
	converted := protocol.Policy{
		Revision: policy.Revision, MonitoringPaused: policy.MonitoringPaused,
		ManualBlock: policy.ManualBlock, WarningMinutes: policy.WarningMinutes,
		LocalPasswordVerifier: policy.LocalPasswordVerifier,
		WeeklyQuotaSeconds:    policy.WeeklyQuota, UpdatedAt: policy.UpdatedAt,
	}
	for _, routine := range policy.Routines {
		converted.Routines = append(converted.Routines, protocol.Routine{
			ID: routine.ID, Name: routine.Name, Days: routine.Days,
			StartSecond: routine.Start, EndSecond: routine.End, Enabled: routine.Enabled,
		})
	}
	return converted
}

// QueueControl updates online-only control state and queues its transition.
func (s *Store) QueueControl(ctx context.Context, deviceID, kind string, now time.Time) error {
	_, err := s.QueueControlOperation(ctx, deviceID, kind, now)
	return err
}

// QueueControlOperation returns the durable identifier used to follow the
// command from the server queue until the agent acknowledges it.
func (s *Store) QueueControlOperation(ctx context.Context, deviceID, kind string, now time.Time) (string, error) {
	setClause := ""
	switch kind {
	case "pause_monitoring":
		setClause = "monitoring_paused=1, manual_block=0"
	case "resume_monitoring":
		setClause = "monitoring_paused=0, manual_block=0"
	case "block_now":
		setClause = "monitoring_paused=0, manual_block=1"
	case "clear_manual_block":
		setClause = "monitoring_paused=0, manual_block=0"
	default:
		return "", errors.New("unknown device control")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var pendingID, pendingKind string
	err = tx.QueryRowContext(ctx, `
		SELECT id, kind FROM device_command
		WHERE device_id=? AND acknowledged_at IS NULL AND kind IN (
			'pause_monitoring', 'resume_monitoring', 'block_now', 'clear_manual_block'
		)
		ORDER BY created_at DESC, id DESC LIMIT 1`, deviceID).Scan(&pendingID, &pendingKind)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err == nil && pendingKind == kind {
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return pendingID, nil
	}
	commandID, err := newID()
	if err != nil {
		return "", err
	}
	stamp := formatTime(now)
	// Remote control is a desired state, not an ordered job queue. Discard any
	// older, unacknowledged control operation and its human-facing projection;
	// additive bonus commands are intentionally not part of this compaction.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM activity WHERE id IN (
			SELECT id FROM device_command
			WHERE device_id=? AND acknowledged_at IS NULL AND kind IN (
				'pause_monitoring', 'resume_monitoring', 'block_now', 'clear_manual_block'
			)
		)`, deviceID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM device_command
		WHERE device_id=? AND acknowledged_at IS NULL AND kind IN (
			'pause_monitoring', 'resume_monitoring', 'block_now', 'clear_manual_block'
		)`, deviceID); err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `UPDATE device_control SET `+setClause+`, revision=revision+1, updated_at=? WHERE device_id=?`, stamp, deviceID)
	if err != nil {
		return "", err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return "", ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_command(id, device_id, kind, payload_json, created_at) VALUES (?, ?, ?, '{}', ?)`, commandID, deviceID, kind, stamp); err != nil {
		return "", err
	}
	if err := insertAdminActivity(ctx, tx, commandID, deviceID, kind, map[string]string{"command": kind}, now); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return commandID, nil
}

func (s *Store) QueueRemoteBonus(ctx context.Context, deviceID string, seconds int64, now time.Time) (string, error) {
	if seconds <= 0 || seconds > int64((24*time.Hour)/time.Second) || now.IsZero() {
		return "", errors.New("invalid remote bonus")
	}
	uuid, err := newID()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(protocol.CreditIncrementPayload{UUID: uuid, Seconds: seconds, Origin: "web", CreatedAt: now.UTC()})
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	stamp := formatTime(now)
	if _, err := bumpPolicyKeepWarning(ctx, tx, deviceID, now); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_command(id, device_id, kind, payload_json, created_at) VALUES (?, ?, 'add_bonus', ?, ?)`, uuid, deviceID, string(payload), stamp); err != nil {
		return "", err
	}
	if err := insertAdminActivity(ctx, tx, uuid, deviceID, "add_bonus", map[string]string{
		"minutes": strconv.FormatInt(seconds/60, 10),
	}, now); err != nil {
		return "", err
	}
	if err := insertAudit(ctx, tx, deviceID, "bonus_queued", map[string]interface{}{"origin": "web", "seconds": seconds}, now); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return uuid, nil
}

func (s *Store) RemoteBonusAcknowledged(ctx context.Context, deviceID, operationID string) (bool, error) {
	if !validOpaqueIdentifier(deviceID) || !validOpaqueIdentifier(operationID) {
		return false, ErrNotFound
	}
	var acknowledgedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT acknowledged_at FROM device_command
		WHERE id=? AND device_id=? AND kind='add_bonus'`, operationID, deviceID).Scan(&acknowledgedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return acknowledgedAt.Valid, nil
}

func (s *Store) HasPendingRemoteBonus(ctx context.Context, deviceID string) (bool, error) {
	var pending int
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM device_command
			WHERE device_id=? AND kind='add_bonus' AND acknowledged_at IS NULL
		)`, deviceID).Scan(&pending)
	return pending != 0, err
}

func (s *Store) UnacknowledgedRemoteBonusSeconds(ctx context.Context, deviceID, localDate string) (int64, error) {
	var seconds int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(b.seconds), 0)
		FROM bonus b
		JOIN device_command c ON c.id=b.uuid AND c.device_id=b.device_id
		WHERE b.device_id=? AND b.local_date=? AND c.kind='add_bonus' AND c.acknowledged_at IS NULL`,
		deviceID, localDate).Scan(&seconds)
	return seconds, err
}

func (s *Store) confirmedSessionState(
	ctx context.Context,
	deviceID string,
	sessionID string,
	localDate string,
	now time.Time,
) (protocol.SessionState, error) {
	policy, err := s.loadPolicy(ctx, deviceID)
	if err != nil {
		return protocol.SessionState{}, err
	}
	summary, err := s.LoadDailySummary(ctx, deviceID, localDate)
	if err != nil {
		return protocol.SessionState{}, err
	}
	parsedDate, err := time.Parse("2006-01-02", localDate)
	if err != nil {
		return protocol.SessionState{}, err
	}
	remaining := policy.WeeklyQuota[parsedDate.Weekday()] + summary.BonusSeconds - summary.UsedSeconds
	if remaining < 0 {
		remaining = 0
	}
	return protocol.SessionState{
		Revision: policy.Revision, SessionID: sessionID, LocalDate: localDate,
		RemainingSeconds: remaining, UsageSeconds: summary.UsedSeconds,
		ConfirmedAt: now.UTC(),
	}, nil
}

func nullableSessionID(active bool, sessionID string) interface{} {
	if !active {
		return nil
	}
	return sessionID
}
