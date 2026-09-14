package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const (
	completedActivityRetentionDays = 30
	activityHistoryCleanupInterval = time.Hour
)

// DeviceActivity is a human-facing read model. Business tables such as bonus
// and device_command remain the source of truth; deleting an activity changes
// only what is shown in the administrative history.
type DeviceActivity struct {
	ID          string            `json:"id"`
	DeviceID    string            `json:"device_id"`
	Kind        string            `json:"kind"`
	Origin      string            `json:"origin"`
	Status      string            `json:"status"`
	Details     map[string]string `json:"details"`
	OccurredAt  time.Time         `json:"occurred_at"`
	ObservedAt  time.Time         `json:"observed_at"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
	Steps       []ActivityStep    `json:"steps"`
}

type ActivityStep struct {
	Kind           string            `json:"kind"`
	Actor          string            `json:"actor"`
	OccurredAt     time.Time         `json:"occurred_at"`
	LastOccurredAt time.Time         `json:"last_occurred_at"`
	Occurrences    int64             `json:"occurrences"`
	Details        map[string]string `json:"details"`
}

func (s *Store) ListDeviceActivities(ctx context.Context, deviceID string, limit int) ([]DeviceActivity, error) {
	if !validOpaqueIdentifier(deviceID) || limit < 1 || limit > 200 {
		return nil, errors.New("invalid activity query")
	}
	if err := ensureDeviceExists(ctx, s.db, deviceID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, activitySelectSQL(`
		WHERE device_id=? AND hidden_at IS NULL ORDER BY occurred_at DESC, id DESC LIMIT ?`), deviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list device activities: %w", err)
	}
	defer rows.Close()
	return scanActivities(rows)
}

func (s *Store) LoadDeviceActivity(ctx context.Context, deviceID, activityID string) (DeviceActivity, error) {
	if !validOpaqueIdentifier(deviceID) || !validOpaqueIdentifier(activityID) {
		return DeviceActivity{}, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, activitySelectSQL(`WHERE device_id=? AND id=? AND hidden_at IS NULL`), deviceID, activityID)
	if err != nil {
		return DeviceActivity{}, err
	}
	defer rows.Close()
	activities, err := scanActivities(rows)
	if err != nil {
		return DeviceActivity{}, err
	}
	if len(activities) == 0 {
		return DeviceActivity{}, ErrNotFound
	}
	return activities[0], nil
}

func activitySelectSQL(selection string) string {
	return `
		WITH selected AS (
			SELECT id, device_id, kind, origin, status, details_json,
			       occurred_at, observed_at, completed_at, expires_at
			FROM activity ` + selection + `
		)
		SELECT a.id, a.device_id, a.kind, a.origin, a.status, a.details_json,
		       a.occurred_at, a.observed_at, a.completed_at, a.expires_at,
		       s.kind, s.actor, s.occurred_at, s.last_occurred_at,
		       s.occurrences, s.details_json
		FROM selected a LEFT JOIN activity_step s ON s.activity_id=a.id
		ORDER BY a.occurred_at DESC, a.id DESC, s.occurred_at, s.id`
}

func scanActivities(rows *sql.Rows) ([]DeviceActivity, error) {
	activities := make([]DeviceActivity, 0)
	byID := make(map[string]int)
	for rows.Next() {
		var activity DeviceActivity
		var detailsJSON, occurred, observed string
		var completed, expires sql.NullString
		var stepKind, stepActor, stepOccurred, stepLast, stepDetails sql.NullString
		var stepOccurrences sql.NullInt64
		if err := rows.Scan(
			&activity.ID, &activity.DeviceID, &activity.Kind, &activity.Origin,
			&activity.Status, &detailsJSON, &occurred, &observed, &completed, &expires,
			&stepKind, &stepActor, &stepOccurred, &stepLast, &stepOccurrences, &stepDetails,
		); err != nil {
			return nil, err
		}
		index, exists := byID[activity.ID]
		if !exists {
			var err error
			activity.Details = map[string]string{}
			if err = json.Unmarshal([]byte(detailsJSON), &activity.Details); err != nil {
				return nil, fmt.Errorf("decode activity details: %w", err)
			}
			if activity.OccurredAt, err = parseTime(occurred); err != nil {
				return nil, err
			}
			if activity.ObservedAt, err = parseTime(observed); err != nil {
				return nil, err
			}
			if activity.CompletedAt, err = nullableActivityTime(completed); err != nil {
				return nil, err
			}
			if activity.ExpiresAt, err = nullableActivityTime(expires); err != nil {
				return nil, err
			}
			activity.Steps = make([]ActivityStep, 0)
			activities = append(activities, activity)
			index = len(activities) - 1
			byID[activity.ID] = index
		}
		if stepKind.Valid {
			step := ActivityStep{
				Kind: stepKind.String, Actor: stepActor.String,
				Occurrences: stepOccurrences.Int64, Details: map[string]string{},
			}
			var err error
			if step.OccurredAt, err = parseTime(stepOccurred.String); err != nil {
				return nil, err
			}
			if step.LastOccurredAt, err = parseTime(stepLast.String); err != nil {
				return nil, err
			}
			if err := json.Unmarshal([]byte(stepDetails.String), &step.Details); err != nil {
				return nil, fmt.Errorf("decode activity step details: %w", err)
			}
			activities[index].Steps = append(activities[index].Steps, step)
		}
	}
	return activities, rows.Err()
}

func nullableActivityTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func insertAdminActivity(ctx context.Context, tx *sql.Tx, id, deviceID, kind string, details map[string]string, now time.Time) error {
	details = includeAdminActor(ctx, details)
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	stamp := formatTime(now)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO activity(
			id, device_id, kind, origin, status, details_json, occurred_at, observed_at
		) VALUES (?, ?, ?, 'admin', 'waiting_device', ?, ?, ?)`,
		id, deviceID, kind, string(encoded), stamp, stamp); err != nil {
		return fmt.Errorf("insert administrative activity: %w", err)
	}
	if err := upsertActivityStep(ctx, tx, id, "requested", "admin", now, nil, false); err != nil {
		return err
	}
	return upsertActivityStep(ctx, tx, id, "stored", "server", now, nil, false)
}

var auditsWithDedicatedActivity = map[string]bool{
	"bonus_queued":       true,
	"pause_monitoring":   true,
	"resume_monitoring":  true,
	"block_now":          true,
	"clear_manual_block": true,
}

func completedAdminAuditKind(kind string) bool { return !auditsWithDedicatedActivity[kind] }

func insertCompletedAdminActivity(ctx context.Context, executor execer, id, deviceID, kind string, details map[string]string, now time.Time) error {
	details = includeAdminActor(ctx, details)
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	stamp := formatTime(now)
	expires := formatTime(now.AddDate(0, 0, completedActivityRetentionDays))
	if _, err := executor.ExecContext(ctx, `
		INSERT OR IGNORE INTO activity(
			id, device_id, kind, origin, status, details_json, occurred_at,
			observed_at, completed_at, expires_at
		) VALUES (?, ?, ?, 'admin', 'completed', ?, ?, ?, ?, ?)`,
		id, deviceID, kind, string(encoded), stamp, stamp, stamp, expires); err != nil {
		return fmt.Errorf("insert completed administrative activity: %w", err)
	}
	if _, err := executor.ExecContext(ctx, `
		INSERT OR IGNORE INTO activity_step(
			activity_id, kind, actor, occurred_at, last_occurred_at, details_json
		) VALUES (?, 'requested', 'admin', ?, ?, '{}')`, id, stamp, stamp); err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `
		INSERT OR IGNORE INTO activity_step(
			activity_id, kind, actor, occurred_at, last_occurred_at, details_json
		) VALUES (?, 'completed', 'server', ?, ?, '{}')`, id, stamp, stamp)
	return err
}

func humanActivityDetails(kind string, encodedAudit []byte) map[string]string {
	var values map[string]interface{}
	if json.Unmarshal(encodedAudit, &values) != nil {
		return map[string]string{}
	}
	details := map[string]string{}
	copyValue := func(key string) {
		value, exists := values[key]
		if !exists || value == nil {
			return
		}
		switch typed := value.(type) {
		case string:
			details[key] = typed
		case float64:
			details[key] = strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			details[key] = strconv.FormatBool(typed)
		}
	}
	switch kind {
	case "device_created", "device_renamed":
		copyValue("name")
	case "quotas_updated":
		copyValue("warning_minutes")
	case "routine_saved", "routine_deleted":
		copyValue("routine_id")
		copyValue("name")
		copyValue("action")
	}
	return details
}

func insertLocalBonusActivity(ctx context.Context, tx *sql.Tx, eventID, deviceID string, seconds int64, localDate string, occurredAt, observedAt time.Time) error {
	details := map[string]string{
		"minutes": strconv.FormatInt(seconds/60, 10), "local_date": localDate,
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO activity(
			id, device_id, kind, origin, status, details_json, occurred_at,
			observed_at, completed_at, expires_at
		) VALUES (?, ?, 'add_bonus', 'device', 'completed', ?, ?, ?, ?, ?)`,
		eventID, deviceID, string(encoded), formatTime(occurredAt), formatTime(observedAt),
		formatTime(observedAt), formatTime(observedAt.AddDate(0, 0, completedActivityRetentionDays)))
	if err != nil {
		return fmt.Errorf("insert local bonus activity: %w", err)
	}
	inserted, _ := result.RowsAffected()
	if inserted == 0 {
		return nil
	}
	if err := upsertActivityStep(ctx, tx, eventID, "local_created", "device", occurredAt, nil, false); err != nil {
		return err
	}
	if err := upsertActivityStep(ctx, tx, eventID, "synchronized", "server", observedAt, nil, false); err != nil {
		return err
	}
	return upsertActivityStep(ctx, tx, eventID, "confirmed", "server", observedAt, nil, false)
}

func upsertActivityStep(ctx context.Context, tx *sql.Tx, activityID, kind, actor string, now time.Time, details map[string]string, repeated bool) error {
	if details == nil {
		details = map[string]string{}
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	stamp := formatTime(now)
	if repeated {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO activity_step(
				activity_id, kind, actor, occurred_at, last_occurred_at, details_json
			) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(activity_id, kind) DO UPDATE SET
				last_occurred_at=excluded.last_occurred_at,
				occurrences=activity_step.occurrences+1,
				details_json=excluded.details_json`,
			activityID, kind, actor, stamp, stamp, string(encoded))
	} else {
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO activity_step(
				activity_id, kind, actor, occurred_at, last_occurred_at, details_json
			) VALUES (?, ?, ?, ?, ?, ?)`,
			activityID, kind, actor, stamp, stamp, string(encoded))
	}
	return err
}

func (s *Store) DeleteCompletedDeviceActivities(ctx context.Context, deviceID string) (int64, error) {
	if !validOpaqueIdentifier(deviceID) {
		return 0, ErrNotFound
	}
	if err := ensureDeviceExists(ctx, s.db, deviceID); err != nil {
		return 0, err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE activity SET hidden_at=?
		WHERE device_id=? AND status='completed' AND hidden_at IS NULL`, formatTime(time.Now()), deviceID)
	if err != nil {
		return 0, fmt.Errorf("hide completed device activities: %w", err)
	}
	return result.RowsAffected()
}

func (s *Store) CleanupExpiredCompletedActivities(ctx context.Context, now time.Time) (int64, error) {
	if now.IsZero() {
		return 0, errors.New("cleanup time is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	deleted, err := cleanupExpiredCompletedActivitiesIfDue(ctx, tx, now)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func cleanupExpiredCompletedActivitiesIfDue(ctx context.Context, tx *sql.Tx, now time.Time) (int64, error) {
	var lastCleanup sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT last_cleanup_at FROM activity_history_maintenance WHERE singleton_id=1`,
	).Scan(&lastCleanup); err != nil {
		return 0, err
	}
	if lastCleanup.Valid {
		parsed, err := parseTime(lastCleanup.String)
		if err != nil {
			return 0, err
		}
		if now.Sub(parsed) < activityHistoryCleanupInterval {
			return 0, nil
		}
	}
	result, err := tx.ExecContext(ctx, `
		DELETE FROM activity
		WHERE status='completed' AND expires_at IS NOT NULL AND expires_at<=?`, formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("delete expired completed activities: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE activity_history_maintenance SET last_cleanup_at=? WHERE singleton_id=1`,
		formatTime(now)); err != nil {
		return 0, err
	}
	return deleted, nil
}
