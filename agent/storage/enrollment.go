package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

type EnrollmentBinding struct {
	InstallationID  string
	StateReset      bool
	IdentityChanged bool
}

// BindEnrollment associates all durable agent state with one server device.
// Existing state without an identity is trusted only for an already-confirmed
// installation, allowing upgrades to preserve valid offline policy.
func (s *Store) BindEnrollment(ctx context.Context, serverURL, deviceID, deviceToken string, trustUnboundState bool) (EnrollmentBinding, error) {
	if serverURL == "" || deviceID == "" || deviceToken == "" {
		return EnrollmentBinding{}, errors.New("server URL, device ID and token are required")
	}
	tokenDigest := sha256.Sum256([]byte(deviceToken))
	tokenFingerprint := hex.EncodeToString(tokenDigest[:])

	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	s.sessionStateMu.Lock()
	defer s.sessionStateMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return EnrollmentBinding{}, fmt.Errorf("begin enrollment binding: %w", err)
	}
	defer tx.Rollback()

	var storedServerURL, storedDeviceID, storedInstallationID, storedTokenFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT server_url, device_id, COALESCE(installation_id, ''), COALESCE(token_fingerprint, '')
		FROM enrollment WHERE singleton_id=1`,
	).Scan(&storedServerURL, &storedDeviceID, &storedInstallationID, &storedTokenFingerprint)
	unbound := errors.Is(err, sql.ErrNoRows)
	if err != nil && !unbound {
		return EnrollmentBinding{}, fmt.Errorf("load enrollment binding: %w", err)
	}
	reset := (unbound && !trustUnboundState) || (!unbound && (storedServerURL != serverURL || storedDeviceID != deviceID))
	identityChanged := unbound || reset || storedInstallationID == "" || storedTokenFingerprint != tokenFingerprint
	installationID := storedInstallationID
	if identityChanged {
		installationID, err = newInstallationID()
		if err != nil {
			return EnrollmentBinding{}, err
		}
	}
	if reset {
		for _, statement := range []string{
			`DELETE FROM routine_day`,
			`DELETE FROM routine`,
			`DELETE FROM weekly_quota`,
			`DELETE FROM policy_state`,
			`DELETE FROM confirmed_session_state`,
			`DELETE FROM pending_control_effect`,
			`DELETE FROM applied_command`,
			`DELETE FROM pending_event`,
			`DELETE FROM bonus`,
			`DELETE FROM daily_usage`,
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return EnrollmentBinding{}, fmt.Errorf("clear previous enrollment state: %w", err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO enrollment(singleton_id, server_url, device_id, installation_id, token_fingerprint)
		VALUES (1, ?, ?, ?, ?)
		ON CONFLICT(singleton_id) DO UPDATE SET
			server_url=excluded.server_url, device_id=excluded.device_id,
			installation_id=excluded.installation_id, token_fingerprint=excluded.token_fingerprint`,
		serverURL, deviceID, installationID, tokenFingerprint); err != nil {
		return EnrollmentBinding{}, fmt.Errorf("store enrollment binding: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return EnrollmentBinding{}, fmt.Errorf("commit enrollment binding: %w", err)
	}
	if reset {
		s.cachedPolicy = PolicySnapshot{}
		s.hasCachedPolicy = false
		s.confirmedSessionState = ConfirmedSessionState{}
		s.hasConfirmedState = false
	}
	return EnrollmentBinding{InstallationID: installationID, StateReset: reset, IdentityChanged: identityChanged}, nil
}

func newInstallationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate installation identity: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
