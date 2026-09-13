package storage

import (
	"context"
	"time"
)

// FamilyDevice is an administrative capability for one device. Its fields
// are private so callers can obtain it only after Store verifies the pair
// family_id + device_id. Every operation revalidates that pair before touching
// device data; agent-facing persistence can continue to authenticate by the
// device credential instead.
type FamilyDevice struct {
	store    *Store
	familyID string
	deviceID string
}

func (s *Store) AuthorizeFamilyDevice(ctx context.Context, familyID, deviceID string) (FamilyDevice, error) {
	if err := s.DeviceBelongsToFamily(ctx, familyID, deviceID); err != nil {
		return FamilyDevice{}, err
	}
	return FamilyDevice{store: s, familyID: familyID, deviceID: deviceID}, nil
}

func (d FamilyDevice) ID() string { return d.deviceID }

func (d FamilyDevice) authorize(ctx context.Context) error {
	if d.store == nil {
		return ErrNotFound
	}
	return d.store.DeviceBelongsToFamily(ctx, d.familyID, d.deviceID)
}

func (d FamilyDevice) Load(ctx context.Context) (Device, Policy, error) {
	if err := d.authorize(ctx); err != nil {
		return Device{}, Policy{}, err
	}
	return d.store.LoadDevice(ctx, d.deviceID)
}

func (d FamilyDevice) LoadControl(ctx context.Context) (Control, error) {
	if err := d.authorize(ctx); err != nil {
		return Control{}, err
	}
	return d.store.LoadControl(ctx, d.deviceID)
}

func (d FamilyDevice) PendingControlKind(ctx context.Context) (string, error) {
	if err := d.authorize(ctx); err != nil {
		return "", err
	}
	return d.store.PendingControlKind(ctx, d.deviceID)
}

func (d FamilyDevice) Rename(ctx context.Context, name string, now time.Time) error {
	if err := d.authorize(ctx); err != nil {
		return err
	}
	return d.store.RenameDevice(ctx, d.deviceID, name, now)
}

func (d FamilyDevice) UpdateIdentity(ctx context.Context, name, avatarKey string, now time.Time) error {
	if err := d.authorize(ctx); err != nil {
		return err
	}
	return d.store.UpdateDeviceIdentity(ctx, d.deviceID, name, avatarKey, now)
}

func (d FamilyDevice) Delete(ctx context.Context) error {
	if err := d.authorize(ctx); err != nil {
		return err
	}
	return d.store.DeleteDevice(ctx, d.deviceID)
}

func (d FamilyDevice) SaveQuotas(ctx context.Context, quotas [7]int64, warningMinutes int, now time.Time) error {
	if err := d.authorize(ctx); err != nil {
		return err
	}
	return d.store.SaveQuotas(ctx, d.deviceID, quotas, warningMinutes, now)
}

func (d FamilyDevice) SaveRoutine(ctx context.Context, routine Routine, now time.Time) (string, error) {
	if err := d.authorize(ctx); err != nil {
		return "", err
	}
	return d.store.SaveRoutine(ctx, d.deviceID, routine, now)
}

func (d FamilyDevice) DeleteRoutine(ctx context.Context, routineID string, now time.Time) error {
	if err := d.authorize(ctx); err != nil {
		return err
	}
	return d.store.DeleteRoutine(ctx, d.deviceID, routineID, now)
}

func (d FamilyDevice) SetLocalPassword(ctx context.Context, verifier string, now time.Time) error {
	if err := d.authorize(ctx); err != nil {
		return err
	}
	return d.store.SetLocalPassword(ctx, d.deviceID, verifier, now)
}

func (d FamilyDevice) IssueToken(ctx context.Context, now time.Time) (string, error) {
	if err := d.authorize(ctx); err != nil {
		return "", err
	}
	return d.store.IssueDeviceToken(ctx, d.deviceID, now)
}

func (d FamilyDevice) RevokeToken(ctx context.Context, now time.Time) error {
	if err := d.authorize(ctx); err != nil {
		return err
	}
	return d.store.RevokeDeviceToken(ctx, d.deviceID, now)
}

func (d FamilyDevice) QueueRemoteBonus(ctx context.Context, seconds int64, now time.Time) (string, error) {
	if err := d.authorize(ctx); err != nil {
		return "", err
	}
	return d.store.QueueRemoteBonus(ctx, d.deviceID, seconds, now)
}

func (d FamilyDevice) RemoteBonusAcknowledged(ctx context.Context, operationID string) (bool, error) {
	if err := d.authorize(ctx); err != nil {
		return false, err
	}
	return d.store.RemoteBonusAcknowledged(ctx, d.deviceID, operationID)
}

func (d FamilyDevice) QueueControlOperation(ctx context.Context, kind string, now time.Time) (string, error) {
	if err := d.authorize(ctx); err != nil {
		return "", err
	}
	return d.store.QueueControlOperation(ctx, d.deviceID, kind, now)
}

func (d FamilyDevice) ListActivities(ctx context.Context, limit int) ([]DeviceActivity, error) {
	if err := d.authorize(ctx); err != nil {
		return nil, err
	}
	return d.store.ListDeviceActivities(ctx, d.deviceID, limit)
}

func (d FamilyDevice) LoadActivity(ctx context.Context, activityID string) (DeviceActivity, error) {
	if err := d.authorize(ctx); err != nil {
		return DeviceActivity{}, err
	}
	return d.store.LoadDeviceActivity(ctx, d.deviceID, activityID)
}

func (d FamilyDevice) DeleteCompletedActivities(ctx context.Context) (int64, error) {
	if err := d.authorize(ctx); err != nil {
		return 0, err
	}
	return d.store.DeleteCompletedDeviceActivities(ctx, d.deviceID)
}

func (d FamilyDevice) ListAudit(ctx context.Context, limit int) ([]AuditEvent, error) {
	if err := d.authorize(ctx); err != nil {
		return nil, err
	}
	return d.store.ListAudit(ctx, d.deviceID, limit)
}

func (d FamilyDevice) ListCommunicationLogs(ctx context.Context, afterID int64, limit int) ([]CommunicationLog, error) {
	if err := d.authorize(ctx); err != nil {
		return nil, err
	}
	return d.store.ListCommunicationLogs(ctx, d.deviceID, afterID, limit)
}

func (d FamilyDevice) AppendCommunicationLog(ctx context.Context, event CommunicationLog, now time.Time) (CommunicationLog, error) {
	if err := d.authorize(ctx); err != nil {
		return CommunicationLog{}, err
	}
	event.DeviceID = d.deviceID
	return d.store.AppendCommunicationLog(ctx, event, now)
}

func (d FamilyDevice) DeleteCommunicationLogs(ctx context.Context) (int64, error) {
	if err := d.authorize(ctx); err != nil {
		return 0, err
	}
	return d.store.DeleteCommunicationLogs(ctx, d.deviceID)
}

func (d FamilyDevice) LatestHeartbeatLocalDate(ctx context.Context) (string, error) {
	if err := d.authorize(ctx); err != nil {
		return "", err
	}
	return d.store.LatestHeartbeatLocalDate(ctx, d.deviceID)
}

func (d FamilyDevice) LoadDailySummary(ctx context.Context, localDate string) (DailySummary, error) {
	if err := d.authorize(ctx); err != nil {
		return DailySummary{}, err
	}
	return d.store.LoadDailySummary(ctx, d.deviceID, localDate)
}

func (d FamilyDevice) UnacknowledgedRemoteBonusSeconds(ctx context.Context, localDate string) (int64, error) {
	if err := d.authorize(ctx); err != nil {
		return 0, err
	}
	return d.store.UnacknowledgedRemoteBonusSeconds(ctx, d.deviceID, localDate)
}
