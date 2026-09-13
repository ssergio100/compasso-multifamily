package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Admin struct {
	ID             string
	Login          string
	PasswordHash   string
	Active         bool
	FamilyID       string
	FamilyState    string
	AuthGeneration int64
}

type Device struct {
	ID                     string
	Name                   string
	AvatarKey              string
	LastSeenAt             *time.Time
	PolicyRevision         int64
	AppliedPolicyRevision  int64
	AppliedControlRevision int64
	GraphicalSessionActive bool
	GraphicalSessionLocked bool
	GraphicalSessionID     string
	CreatedAt              time.Time
	Online                 bool
}

type Policy struct {
	Revision              int64
	MonitoringPaused      bool
	ManualBlock           bool
	WarningMinutes        int
	LocalPasswordVerifier string
	WeeklyQuota           [7]int64
	Routines              []Routine
	UpdatedAt             time.Time
}

type Control struct {
	Revision         int64
	MonitoringPaused bool
	ManualBlock      bool
}

func (s *Store) loadControl(ctx context.Context, deviceID string) (Control, error) {
	var control Control
	var paused, blocked int
	err := s.db.QueryRowContext(ctx, `
		SELECT revision, monitoring_paused, manual_block FROM device_control WHERE device_id=?`, deviceID,
	).Scan(&control.Revision, &paused, &blocked)
	if errors.Is(err, sql.ErrNoRows) {
		return Control{}, ErrNotFound
	}
	control.MonitoringPaused, control.ManualBlock = paused != 0, blocked != 0
	return control, err
}

func (s *Store) LoadControl(ctx context.Context, deviceID string) (Control, error) {
	return s.loadControl(ctx, deviceID)
}

// PendingControlKind returns the latest control transition that has not yet
// been acknowledged by the agent. An empty kind means there is no transition
// in flight.
func (s *Store) PendingControlKind(ctx context.Context, deviceID string) (string, error) {
	var kind string
	err := s.db.QueryRowContext(ctx, `
		SELECT kind FROM device_command
		WHERE device_id=? AND acknowledged_at IS NULL AND kind IN (
			'pause_monitoring', 'resume_monitoring', 'block_now', 'clear_manual_block'
		)
		ORDER BY created_at DESC, id DESC LIMIT 1`, deviceID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return kind, err
}

type Routine struct {
	ID      string
	Name    string
	IconKey string
	Days    [7]bool
	Start   int64
	End     int64
	Enabled bool
}

const (
	DefaultAvatarKey      = "cat"
	DefaultRoutineIconKey = "general"
)

var validAvatarKeys = map[string]bool{
	"cat": true, "dog": true, "fox": true, "rabbit": true, "panda": true, "owl": true,
	"penguin": true, "capybara": true, "cat_bow": true, "rabbit_flower": true,
	"panda_flower": true, "fox_bow": true, "chick": true, "lion": true, "sheep": true, "tiger": true,
}

var validRoutineIconKeys = map[string]bool{
	"study": true, "reading": true, "sleep": true, "bath": true, "meal": true, "school": true,
	"exercise": true, "chores": true, "family": true, "music": true, "outdoor": true, "general": true,
}

func ValidAvatarKey(value string) bool      { return validAvatarKeys[value] }
func ValidRoutineIconKey(value string) bool { return validRoutineIconKeys[value] }

type RoutineConflictError struct {
	RoutineName string
}

func (e *RoutineConflictError) Error() string {
	return fmt.Sprintf("routine overlaps %q", e.RoutineName)
}

type AuditEvent struct {
	ID        string
	Kind      string
	Origin    string
	Details   string
	CreatedAt time.Time
}

type DailySummary struct {
	UsedSeconds  int64
	BonusSeconds int64
}

func (s *Store) BootstrapAdmin(ctx context.Context, login, passwordHash string, now time.Time) (bool, error) {
	login = strings.TrimSpace(login)
	if login == "" || passwordHash == "" || now.IsZero() {
		return false, errors.New("bootstrap admin requires login, password hash and time")
	}
	id, err := newID()
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin administrator setup: %w", err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_user`).Scan(&count); err != nil {
		return false, fmt.Errorf("count administrators: %w", err)
	}
	if count != 0 {
		return false, nil
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO admin_user(id, login, password_hash, active, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)`, id, login, passwordHash, formatTime(now), formatTime(now))
	if err != nil {
		return false, fmt.Errorf("bootstrap administrator: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO family(id, name, state, created_at, updated_at)
		VALUES (?, 'Família', 'active', ?, ?)`, id, formatTime(now), formatTime(now)); err != nil {
		return false, fmt.Errorf("bootstrap family: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO family_member(family_id, admin_user_id, role, created_at)
		VALUES (?, ?, 'owner', ?)`, id, id, formatTime(now)); err != nil {
		return false, fmt.Errorf("bootstrap family owner: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit administrator setup: %w", err)
	}
	return true, nil
}

func (s *Store) HasAdministrators(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_user`).Scan(&count); err != nil {
		return false, fmt.Errorf("count administrators: %w", err)
	}
	return count != 0, nil
}

func (s *Store) AdminByLogin(ctx context.Context, login string) (Admin, error) {
	var admin Admin
	var active int
	err := s.db.QueryRowContext(ctx,
		`SELECT u.id, u.login, u.password_hash, u.active, m.family_id, f.state, u.auth_generation
		 FROM admin_user u
		 JOIN family_member m ON m.admin_user_id=u.id AND m.role='owner'
		 JOIN family f ON f.id=m.family_id
		 WHERE u.login = ?`, strings.TrimSpace(login),
	).Scan(&admin.ID, &admin.Login, &admin.PasswordHash, &active, &admin.FamilyID, &admin.FamilyState, &admin.AuthGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return Admin{}, ErrNotFound
	}
	if err != nil {
		return Admin{}, fmt.Errorf("load administrator: %w", err)
	}
	admin.Active = active != 0
	return admin, nil
}

func (s *Store) CreateDevice(ctx context.Context, name string, now time.Time) (Device, error) {
	familyID, err := s.soleFamilyID(ctx)
	if err != nil {
		return Device{}, err
	}
	return s.CreateDeviceForFamily(ctx, familyID, name, DefaultAvatarKey, now)
}

func (s *Store) CreateDeviceWithAvatar(ctx context.Context, name, avatarKey string, now time.Time) (Device, error) {
	familyID, err := s.soleFamilyID(ctx)
	if err != nil {
		return Device{}, err
	}
	return s.CreateDeviceForFamily(ctx, familyID, name, avatarKey, now)
}

func (s *Store) soleFamilyID(ctx context.Context) (string, error) {
	var familyID string
	if err := s.db.QueryRowContext(ctx, `
		SELECT id FROM family WHERE state='active'
		AND (SELECT COUNT(*) FROM family WHERE state='active')=1`,
	).Scan(&familyID); errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	} else if err != nil {
		return "", err
	}
	return familyID, nil
}

func (s *Store) CreateDeviceForFamily(ctx context.Context, familyID, name, avatarKey string, now time.Time) (Device, error) {
	familyID = strings.TrimSpace(familyID)
	name = strings.TrimSpace(name)
	if avatarKey == "" {
		avatarKey = DefaultAvatarKey
	}
	if familyID == "" || name == "" || len(name) > 80 || !ValidAvatarKey(avatarKey) || now.IsZero() {
		return Device{}, errors.New("device requires a name of at most 80 characters and current time")
	}
	id, err := newID()
	if err != nil {
		return Device{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Device{}, err
	}
	defer tx.Rollback()
	var familyState string
	var deviceCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT f.state, COUNT(d.id)
		FROM family f LEFT JOIN device d ON d.family_id=f.id
		WHERE f.id=? GROUP BY f.id`, familyID,
	).Scan(&familyState, &deviceCount); errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	} else if err != nil {
		return Device{}, err
	}
	if familyState != "active" {
		return Device{}, ErrNotFound
	}
	if deviceCount >= 5 {
		return Device{}, ErrDeviceLimit
	}
	stamp := formatTime(now)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO device(id, family_id, name, avatar_key, device_token_hash, policy_revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', 1, ?, ?)`, id, familyID, name, avatarKey, stamp, stamp); err != nil {
		return Device{}, fmt.Errorf("create device: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO policy(device_id, revision, warning_minutes, updated_at) VALUES (?, 1, 10, ?)`, id, stamp); err != nil {
		return Device{}, fmt.Errorf("create device policy: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO device_control(device_id, revision, updated_at) VALUES (?, 1, ?)`, id, stamp); err != nil {
		return Device{}, fmt.Errorf("create device control: %w", err)
	}
	for weekday := 0; weekday < 7; weekday++ {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO weekly_quota(device_id, weekday, seconds_allowed) VALUES (?, ?, 0)`, id, weekday); err != nil {
			return Device{}, err
		}
	}
	if err := insertAudit(ctx, tx, id, "device_created", map[string]interface{}{"name": name}, now); err != nil {
		return Device{}, err
	}
	if err := tx.Commit(); err != nil {
		return Device{}, err
	}
	return Device{ID: id, Name: name, AvatarKey: avatarKey, PolicyRevision: 1, CreatedAt: now}, nil
}

func (s *Store) ListDevices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, avatar_key, last_seen_at, policy_revision, applied_policy_revision, applied_control_revision,
		       graphical_session_active, graphical_session_locked, graphical_session_id, created_at
		FROM device ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()
	var devices []Device
	for rows.Next() {
		var device Device
		var lastSeen sql.NullString
		var graphicalSessionID sql.NullString
		var graphicalSessionActive int
		var graphicalSessionLocked int
		var created string
		if err := rows.Scan(&device.ID, &device.Name, &device.AvatarKey, &lastSeen, &device.PolicyRevision,
			&device.AppliedPolicyRevision, &device.AppliedControlRevision, &graphicalSessionActive,
			&graphicalSessionLocked, &graphicalSessionID, &created); err != nil {
			return nil, err
		}
		device.GraphicalSessionActive = graphicalSessionActive != 0
		device.GraphicalSessionLocked = graphicalSessionLocked != 0
		device.GraphicalSessionID = graphicalSessionID.String
		device.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			value, err := parseTime(lastSeen.String)
			if err != nil {
				return nil, err
			}
			device.LastSeenAt = &value
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (s *Store) LoadDevice(ctx context.Context, id string) (Device, Policy, error) {
	var device Device
	var lastSeen sql.NullString
	var graphicalSessionID sql.NullString
	var graphicalSessionActive int
	var graphicalSessionLocked int
	var created string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, avatar_key, last_seen_at, policy_revision, applied_policy_revision, applied_control_revision,
		       graphical_session_active, graphical_session_locked, graphical_session_id, created_at
		FROM device WHERE id=?`, id,
	).Scan(&device.ID, &device.Name, &device.AvatarKey, &lastSeen, &device.PolicyRevision,
		&device.AppliedPolicyRevision, &device.AppliedControlRevision, &graphicalSessionActive,
		&graphicalSessionLocked, &graphicalSessionID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, Policy{}, ErrNotFound
	}
	if err != nil {
		return Device{}, Policy{}, err
	}
	device.CreatedAt, _ = parseTime(created)
	device.GraphicalSessionActive = graphicalSessionActive != 0
	device.GraphicalSessionLocked = graphicalSessionLocked != 0
	device.GraphicalSessionID = graphicalSessionID.String
	if lastSeen.Valid {
		value, parseErr := parseTime(lastSeen.String)
		if parseErr != nil {
			return Device{}, Policy{}, parseErr
		}
		device.LastSeenAt = &value
	}
	policy, err := s.loadPolicy(ctx, id)
	return device, policy, err
}

func (s *Store) RenameDevice(ctx context.Context, id, name string, now time.Time) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return errors.New("device name must contain at most 80 characters")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE device SET name=?, updated_at=? WHERE id=?`, name, formatTime(now), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrNotFound
	}
	if err := insertAudit(ctx, tx, id, "device_renamed", map[string]interface{}{"name": name}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateDeviceIdentity(ctx context.Context, id, name, avatarKey string, now time.Time) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 || !ValidAvatarKey(avatarKey) {
		return errors.New("invalid device identity")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE device SET name=?, avatar_key=?, updated_at=? WHERE id=?`, name, avatarKey, formatTime(now), id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrNotFound
	}
	if err := insertAudit(ctx, tx, id, "device_identity_updated", map[string]interface{}{"name": name, "avatar_key": avatarKey}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM device WHERE id=?`, id)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SaveQuotas(ctx context.Context, deviceID string, quotas [7]int64, warningMinutes int, now time.Time) error {
	for _, seconds := range quotas {
		if seconds < 0 || seconds > 24*60*60 {
			return errors.New("quota must be between 0 and 24 hours")
		}
	}
	if warningMinutes < 0 || warningMinutes > 120 {
		return errors.New("warning must be between 0 and 120 minutes")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for weekday, seconds := range quotas {
		if _, err := tx.ExecContext(ctx, `UPDATE weekly_quota SET seconds_allowed=? WHERE device_id=? AND weekday=?`, seconds, deviceID, weekday); err != nil {
			return err
		}
	}
	revision, err := bumpPolicy(ctx, tx, deviceID, warningMinutes, now)
	if err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, deviceID, "quotas_updated", map[string]interface{}{
		"revision": revision, "weekly_seconds": quotas, "warning_minutes": warningMinutes,
	}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SaveRoutine(ctx context.Context, deviceID string, routine Routine, now time.Time) (string, error) {
	routine.Name = strings.TrimSpace(routine.Name)
	if routine.Name == "" || len(routine.Name) > 80 || routine.Start < 0 || routine.Start >= 86400 || routine.End < 0 || routine.End >= 86400 || routine.IconKey != "" && !ValidRoutineIconKey(routine.IconKey) {
		return "", errors.New("routine has invalid name or time")
	}
	selected := false
	for _, day := range routine.Days {
		selected = selected || day
	}
	if !selected {
		return "", errors.New("routine requires at least one day")
	}
	isNew := routine.ID == ""
	if isNew {
		var err error
		routine.ID, err = newID()
		if err != nil {
			return "", err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if !isNew {
		var existingIconKey string
		if err := tx.QueryRowContext(ctx, `SELECT icon_key FROM routine WHERE id=? AND device_id=?`, routine.ID, deviceID).Scan(&existingIconKey); errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		} else if err != nil {
			return "", err
		}
		if routine.IconKey == "" {
			routine.IconKey = existingIconKey
		}
	} else if routine.IconKey == "" {
		routine.IconKey = DefaultRoutineIconKey
	}
	existing, err := loadRoutines(ctx, tx, deviceID)
	if err != nil {
		return "", err
	}
	for _, candidate := range existing {
		if candidate.ID != routine.ID && routinesOverlap(routine, candidate) {
			return "", &RoutineConflictError{RoutineName: candidate.Name}
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO routine(id, device_id, name, icon_key, start_second, end_second, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, start_second=excluded.start_second,
		end_second=excluded.end_second, enabled=excluded.enabled, icon_key=excluded.icon_key
		WHERE routine.device_id=excluded.device_id`, routine.ID, deviceID, routine.Name, routine.IconKey, routine.Start, routine.End, boolInt(routine.Enabled)); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM routine_day WHERE routine_id=?`, routine.ID); err != nil {
		return "", err
	}
	for weekday, enabled := range routine.Days {
		if enabled {
			if _, err := tx.ExecContext(ctx, `INSERT INTO routine_day(routine_id, weekday) VALUES (?, ?)`, routine.ID, weekday); err != nil {
				return "", err
			}
		}
	}
	revision, err := bumpPolicyKeepWarning(ctx, tx, deviceID, now)
	if err != nil {
		return "", err
	}
	action := "updated"
	if isNew {
		action = "created"
	}
	if err := insertAudit(ctx, tx, deviceID, "routine_saved", map[string]interface{}{
		"routine_id": routine.ID, "name": routine.Name, "icon_key": routine.IconKey, "revision": revision, "action": action,
	}, now); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return routine.ID, nil
}

type queryer interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
}

func loadRoutines(ctx context.Context, q queryer, deviceID string) ([]Routine, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT r.id, r.name, r.icon_key, r.start_second, r.end_second, r.enabled, rd.weekday
		FROM routine r LEFT JOIN routine_day rd ON rd.routine_id=r.id
		WHERE r.device_id=? ORDER BY r.id`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routines []Routine
	indexes := make(map[string]int)
	for rows.Next() {
		var id, name, iconKey string
		var start, end int64
		var enabled int
		var weekday sql.NullInt64
		if err := rows.Scan(&id, &name, &iconKey, &start, &end, &enabled, &weekday); err != nil {
			return nil, err
		}
		index, ok := indexes[id]
		if !ok {
			index = len(routines)
			indexes[id] = index
			routines = append(routines, Routine{ID: id, Name: name, IconKey: iconKey, Start: start, End: end, Enabled: enabled != 0})
		}
		if weekday.Valid {
			routines[index].Days[weekday.Int64] = true
		}
	}
	return routines, rows.Err()
}

type routineInterval struct{ start, end int64 }

func routineIntervals(routine Routine) []routineInterval {
	const daySeconds = int64(24 * 60 * 60)
	intervals := make([]routineInterval, 0, 14)
	for day := int64(0); day < 7; day++ {
		previousDay := (day + 6) % 7
		if routine.Start == routine.End {
			if routine.Days[day] {
				intervals = append(intervals, routineInterval{day * daySeconds, (day + 1) * daySeconds})
			}
			continue
		}
		if routine.Start < routine.End {
			if routine.Days[day] {
				intervals = append(intervals, routineInterval{day*daySeconds + routine.Start, day*daySeconds + routine.End})
			}
			continue
		}
		if routine.Days[previousDay] && routine.End > 0 {
			intervals = append(intervals, routineInterval{day * daySeconds, day*daySeconds + routine.End})
		}
		if routine.Days[day] {
			intervals = append(intervals, routineInterval{day*daySeconds + routine.Start, (day + 1) * daySeconds})
		}
	}
	return intervals
}

func routinesOverlap(first, second Routine) bool {
	for _, a := range routineIntervals(first) {
		for _, b := range routineIntervals(second) {
			if a.start < b.end && b.start < a.end {
				return true
			}
		}
	}
	return false
}

func (s *Store) DeleteRoutine(ctx context.Context, deviceID, routineID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var routineName string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM routine WHERE id=? AND device_id=?`, routineID, deviceID).Scan(&routineName); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM routine WHERE id=? AND device_id=?`, routineID, deviceID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrNotFound
	}
	if _, err := bumpPolicyKeepWarning(ctx, tx, deviceID, now); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, deviceID, "routine_deleted", map[string]interface{}{"routine_id": routineID, "name": routineName}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetLocalPassword(ctx context.Context, deviceID, verifier string, now time.Time) error {
	if verifier == "" {
		return errors.New("local password verifier cannot be empty")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE policy SET local_password_verifier=?, revision=revision+1, updated_at=? WHERE device_id=?`, verifier, formatTime(now), deviceID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device SET policy_revision=policy_revision+1, updated_at=? WHERE id=?`, formatTime(now), deviceID); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, deviceID, "local_password_changed", map[string]interface{}{"status": "awaiting_sync"}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListAudit(ctx context.Context, deviceID string, limit int) ([]AuditEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT uuid, kind, origin, payload_json, created_at FROM audit_event
		WHERE device_id=? ORDER BY created_at DESC, uuid DESC LIMIT ?`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		var created string
		if err := rows.Scan(&event.ID, &event.Kind, &event.Origin, &event.Details, &created); err != nil {
			return nil, err
		}
		event.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) LoadDailySummary(ctx context.Context, deviceID, localDate string) (DailySummary, error) {
	var summary DailySummary
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT seconds_used FROM daily_usage WHERE device_id=? AND local_date=?), 0),
		       COALESCE((SELECT SUM(seconds) FROM bonus WHERE device_id=? AND local_date=?), 0)`,
		deviceID, localDate, deviceID, localDate,
	).Scan(&summary.UsedSeconds, &summary.BonusSeconds)
	if err != nil {
		return DailySummary{}, fmt.Errorf("load daily summary: %w", err)
	}
	return summary, nil
}

// LatestHeartbeatLocalDate returns the controlled computer's local date from
// its most recent heartbeat. An empty result means the device has never sent a
// usage sample.
func (s *Store) LatestHeartbeatLocalDate(ctx context.Context, deviceID string) (string, error) {
	var localDate string
	err := s.db.QueryRowContext(ctx, `
		SELECT local_date FROM daily_usage
		WHERE device_id=? ORDER BY last_sync_at DESC LIMIT 1`, deviceID,
	).Scan(&localDate)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load latest heartbeat local date: %w", err)
	}
	parsedDate, err := time.Parse("2006-01-02", localDate)
	if err != nil || parsedDate.Format("2006-01-02") != localDate {
		return "", fmt.Errorf("stored heartbeat local date is invalid: %q", localDate)
	}
	return localDate, nil
}

func (s *Store) InsertAudit(ctx context.Context, deviceID, kind string, payload interface{}, now time.Time) error {
	return insertAudit(ctx, s.db, deviceID, kind, payload, now)
}

func (s *Store) loadPolicy(ctx context.Context, deviceID string) (Policy, error) {
	var policy Policy
	var paused, blocked int
	var updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT revision, monitoring_paused, manual_block, warning_minutes,
		COALESCE(local_password_verifier, ''), updated_at FROM policy WHERE device_id=?`, deviceID,
	).Scan(&policy.Revision, &paused, &blocked, &policy.WarningMinutes, &policy.LocalPasswordVerifier, &updatedAt)
	if err != nil {
		return Policy{}, err
	}
	policy.MonitoringPaused, policy.ManualBlock = paused != 0, blocked != 0
	policy.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Policy{}, err
	}
	quotaRows, err := s.db.QueryContext(ctx, `SELECT weekday, seconds_allowed FROM weekly_quota WHERE device_id=?`, deviceID)
	if err != nil {
		return Policy{}, err
	}
	for quotaRows.Next() {
		var weekday int
		var seconds int64
		if err := quotaRows.Scan(&weekday, &seconds); err != nil {
			_ = quotaRows.Close()
			return Policy{}, err
		}
		policy.WeeklyQuota[weekday] = seconds
	}
	_ = quotaRows.Close()
	routineRows, err := s.db.QueryContext(ctx, `SELECT id, name, icon_key, start_second, end_second, enabled FROM routine WHERE device_id=? ORDER BY name, id`, deviceID)
	if err != nil {
		return Policy{}, err
	}
	for routineRows.Next() {
		var routine Routine
		var enabled int
		if err := routineRows.Scan(&routine.ID, &routine.Name, &routine.IconKey, &routine.Start, &routine.End, &enabled); err != nil {
			_ = routineRows.Close()
			return Policy{}, err
		}
		routine.Enabled = enabled != 0
		policy.Routines = append(policy.Routines, routine)
	}
	_ = routineRows.Close()
	indexes := make(map[string]int)
	for index := range policy.Routines {
		indexes[policy.Routines[index].ID] = index
	}
	dayRows, err := s.db.QueryContext(ctx, `
		SELECT rd.routine_id, rd.weekday FROM routine_day rd JOIN routine r ON r.id=rd.routine_id
		WHERE r.device_id=?`, deviceID)
	if err != nil {
		return Policy{}, err
	}
	for dayRows.Next() {
		var id string
		var weekday int
		if err := dayRows.Scan(&id, &weekday); err != nil {
			_ = dayRows.Close()
			return Policy{}, err
		}
		policy.Routines[indexes[id]].Days[weekday] = true
	}
	_ = dayRows.Close()
	return policy, nil
}

type execer interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}

func insertAudit(ctx context.Context, executor execer, deviceID, kind string, payload interface{}, now time.Time) error {
	id, err := newID()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `
		INSERT INTO audit_event(uuid, device_id, kind, origin, payload_json, created_at)
		VALUES (?, ?, ?, 'web', ?, ?)`, id, deviceID, kind, string(encoded), formatTime(now))
	if err != nil {
		return err
	}
	if completedAdminAuditKind(kind) {
		return insertCompletedAdminActivity(ctx, executor, id, deviceID, kind, humanActivityDetails(kind, encoded), now)
	}
	return nil
}

func bumpPolicy(ctx context.Context, tx *sql.Tx, deviceID string, warningMinutes int, now time.Time) (int64, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE policy SET revision=revision+1, warning_minutes=?, updated_at=? WHERE device_id=?`, warningMinutes, formatTime(now), deviceID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device SET policy_revision=policy_revision+1, updated_at=? WHERE id=?`, formatTime(now), deviceID); err != nil {
		return 0, err
	}
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM policy WHERE device_id=?`, deviceID).Scan(&revision)
	return revision, err
}

func bumpPolicyKeepWarning(ctx context.Context, tx *sql.Tx, deviceID string, now time.Time) (int64, error) {
	var warning int
	if err := tx.QueryRowContext(ctx, `SELECT warning_minutes FROM policy WHERE device_id=?`, deviceID).Scan(&warning); err != nil {
		return 0, err
	}
	return bumpPolicy(ctx, tx, deviceID, warning, now)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func randomID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes)
	parts := []string{encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32]}
	return strings.Join(parts, "-"), nil
}
