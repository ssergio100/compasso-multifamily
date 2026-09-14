package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var ErrInvalidAccountToken = errors.New("invalid or expired account token")

const pendingAccountLifetime = 24 * time.Hour

// BeginRegistration creates or refreshes an unverified account. It returns
// false for an already confirmed e-mail so the HTTP layer can keep a generic
// response without sending a misleading message.
func (s *Store) BeginRegistration(
	ctx context.Context,
	familyName, email, passwordHash, tokenHash string,
	expiresAt, now time.Time,
) (bool, error) {
	familyName = strings.TrimSpace(familyName)
	email = strings.ToLower(strings.TrimSpace(email))
	if familyName == "" || len(familyName) > 120 || email == "" || len(email) > 254 ||
		passwordHash == "" || tokenHash == "" || !expiresAt.After(now) || now.IsZero() {
		return false, errors.New("invalid pending registration")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := cleanupExpiredPendingAccounts(ctx, tx, now); err != nil {
		return false, err
	}
	var adminID string
	var verifiedAt sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id, email_verified_at FROM admin_user WHERE lower(email)=?`, email).
		Scan(&adminID, &verifiedAt)
	switch {
	case err == nil && verifiedAt.Valid:
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	case err == nil:
		if _, err := tx.ExecContext(ctx, `
			UPDATE admin_user
			SET login=?, password_hash=?, active=0, pending_family_name=?, updated_at=?
			WHERE id=?`, email, passwordHash, familyName, formatTime(now), adminID); err != nil {
			return false, err
		}
	case errors.Is(err, sql.ErrNoRows):
		adminID, err = newID()
		if err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO admin_user(
				id, login, password_hash, active, created_at, updated_at,
				email, auth_generation, pending_family_name
			) VALUES (?, ?, ?, 0, ?, ?, ?, 1, ?)`,
			adminID, email, passwordHash, formatTime(now), formatTime(now), email, familyName,
		); err != nil {
			if isUniqueConstraint(err) {
				return false, ErrConflict
			}
			return false, err
		}
	default:
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM account_token WHERE admin_user_id=? AND purpose='verify_email'`, adminID); err != nil {
		return false, err
	}
	if err := insertAccountToken(ctx, tx, adminID, "verify_email", tokenHash, "", expiresAt, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ResendRegistrationConfirmation(
	ctx context.Context, email, tokenHash string, expiresAt, now time.Time,
) (bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || tokenHash == "" || !expiresAt.After(now) || now.IsZero() {
		return false, errors.New("invalid confirmation request")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := cleanupExpiredPendingAccounts(ctx, tx, now); err != nil {
		return false, err
	}
	var adminID string
	err = tx.QueryRowContext(ctx, `
		SELECT u.id FROM admin_user u
		WHERE lower(u.email)=? AND u.email_verified_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM family_member m WHERE m.admin_user_id=u.id)`, email,
	).Scan(&adminID)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM account_token WHERE admin_user_id=? AND purpose='verify_email'`, adminID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE admin_user SET updated_at=? WHERE id=?`, formatTime(now), adminID); err != nil {
		return false, err
	}
	if err := insertAccountToken(ctx, tx, adminID, "verify_email", tokenHash, "", expiresAt, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ConfirmRegistration(ctx context.Context, tokenHash string, now time.Time) (Admin, error) {
	if tokenHash == "" || now.IsZero() {
		return Admin{}, ErrInvalidAccountToken
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Admin{}, err
	}
	defer tx.Rollback()
	var admin Admin
	var familyName, expires string
	err = tx.QueryRowContext(ctx, `
		SELECT u.id, u.login, u.password_hash, u.auth_generation,
		       u.pending_family_name, t.expires_at
		FROM account_token t JOIN admin_user u ON u.id=t.admin_user_id
		WHERE t.token_hash=? AND t.purpose='verify_email' AND t.consumed_at IS NULL`, tokenHash,
	).Scan(&admin.ID, &admin.Login, &admin.PasswordHash, &admin.AuthGeneration, &familyName, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Admin{}, ErrInvalidAccountToken
	}
	if err != nil {
		return Admin{}, err
	}
	expiresAt, err := parseTime(expires)
	if err != nil || !expiresAt.After(now) || strings.TrimSpace(familyName) == "" {
		return Admin{}, ErrInvalidAccountToken
	}
	var familyCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM family`).Scan(&familyCount); err != nil {
		return Admin{}, err
	}
	if familyCount >= 100 {
		return Admin{}, ErrFamilyLimit
	}
	familyID, err := newID()
	if err != nil {
		return Admin{}, err
	}
	stamp := formatTime(now)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO family(id, name, state, created_at, updated_at)
		VALUES (?, ?, 'active', ?, ?)`, familyID, familyName, stamp, stamp); err != nil {
		return Admin{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO family_member(family_id, admin_user_id, role, created_at)
		VALUES (?, ?, 'owner', ?)`, familyID, admin.ID, stamp); err != nil {
		return Admin{}, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE admin_user SET active=1, email_verified_at=?, pending_family_name=NULL, updated_at=?
		WHERE id=? AND email_verified_at IS NULL`, stamp, stamp, admin.ID)
	if err != nil {
		return Admin{}, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return Admin{}, ErrInvalidAccountToken
	}
	if _, err := tx.ExecContext(ctx, `UPDATE account_token SET consumed_at=? WHERE token_hash=?`, stamp, tokenHash); err != nil {
		return Admin{}, err
	}
	if err := tx.Commit(); err != nil {
		return Admin{}, err
	}
	admin.Active = true
	admin.Email = admin.Login
	admin.EmailVerified = true
	admin.FamilyID = familyID
	admin.FamilyName = familyName
	admin.FamilyState = "active"
	return admin, nil
}

func (s *Store) BeginPasswordReset(
	ctx context.Context, email, tokenHash string, expiresAt, now time.Time,
) (string, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || tokenHash == "" || !expiresAt.After(now) || now.IsZero() {
		return "", false, errors.New("invalid password reset request")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var adminID, destination string
	err = tx.QueryRowContext(ctx, `
		SELECT u.id, u.email FROM admin_user u
		JOIN family_member m ON m.admin_user_id=u.id AND m.role='owner'
		WHERE lower(u.email)=? AND u.email_verified_at IS NOT NULL AND u.active=1`, email,
	).Scan(&adminID, &destination)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return "", false, err
		}
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM account_token WHERE admin_user_id=? AND purpose='reset_password'`, adminID); err != nil {
		return "", false, err
	}
	if err := insertAccountToken(ctx, tx, adminID, "reset_password", tokenHash, "", expiresAt, now); err != nil {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return destination, true, nil
}

func (s *Store) ResetPassword(ctx context.Context, tokenHash, passwordHash string, now time.Time) error {
	if tokenHash == "" || passwordHash == "" || now.IsZero() {
		return ErrInvalidAccountToken
	}
	return s.consumeAccountToken(ctx, tokenHash, "reset_password", now, func(tx *sql.Tx, adminID, _ string) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE admin_user SET password_hash=?, auth_generation=auth_generation+1, updated_at=?
			WHERE id=? AND active=1 AND email_verified_at IS NOT NULL`, passwordHash, formatTime(now), adminID)
		return err
	})
}

func (s *Store) ChangePassword(ctx context.Context, adminID, passwordHash string, now time.Time) error {
	if adminID == "" || passwordHash == "" || now.IsZero() {
		return errors.New("administrator, password hash and time are required")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE admin_user SET password_hash=?, auth_generation=auth_generation+1, updated_at=?
		WHERE id=? AND active=1 AND email_verified_at IS NOT NULL`, passwordHash, formatTime(now), adminID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) BeginEmailChange(
	ctx context.Context, adminID, pendingEmail, tokenHash string, expiresAt, now time.Time,
) error {
	pendingEmail = strings.ToLower(strings.TrimSpace(pendingEmail))
	if adminID == "" || pendingEmail == "" || tokenHash == "" || !expiresAt.After(now) || now.IsZero() {
		return errors.New("invalid email change request")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_user WHERE lower(email)=? AND id<>?)`, pendingEmail, adminID).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM account_token WHERE admin_user_id=? AND purpose='change_email'`, adminID); err != nil {
		return err
	}
	if err := insertAccountToken(ctx, tx, adminID, "change_email", tokenHash, pendingEmail, expiresAt, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ConfirmEmailChange(ctx context.Context, tokenHash string, now time.Time) error {
	return s.consumeAccountToken(ctx, tokenHash, "change_email", now, func(tx *sql.Tx, adminID, pendingEmail string) error {
		if pendingEmail == "" {
			return ErrInvalidAccountToken
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE admin_user SET login=?, email=?, email_verified_at=?,
				auth_generation=auth_generation+1, updated_at=? WHERE id=?`,
			pendingEmail, pendingEmail, formatTime(now), formatTime(now), adminID)
		if isUniqueConstraint(err) {
			return ErrConflict
		}
		return err
	})
}

func (s *Store) DeleteActiveFamily(ctx context.Context, familyID, exactName string) error {
	if familyID == "" || exactName == "" {
		return errors.New("family id and exact name are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var storedName string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM family WHERE id=? AND state='active'`, familyID).Scan(&storedName); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if storedName != exactName {
		return ErrConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT admin_user_id FROM family_member WHERE family_id=?`, familyID)
	if err != nil {
		return err
	}
	var adminIDs []string
	for rows.Next() {
		var adminID string
		if err := rows.Scan(&adminID); err != nil {
			rows.Close()
			return err
		}
		adminIDs = append(adminIDs, adminID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM family WHERE id=?`, familyID); err != nil {
		return err
	}
	for _, adminID := range adminIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM admin_user WHERE id=?`, adminID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CleanupExpiredAccounts removes abandoned unverified identities and expired
// account tokens. It is safe to run repeatedly from the single API process.
func (s *Store) CleanupExpiredAccounts(ctx context.Context, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := cleanupExpiredPendingAccounts(ctx, tx, now); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM account_token WHERE expires_at<=?`, formatTime(now))
	if err != nil {
		return 0, err
	}
	deleted, _ := result.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func cleanupExpiredPendingAccounts(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM admin_user
		WHERE email_verified_at IS NULL AND updated_at<=?
		AND NOT EXISTS (SELECT 1 FROM family_member m WHERE m.admin_user_id=admin_user.id)`,
		formatTime(now.Add(-pendingAccountLifetime)),
	)
	return err
}

func insertAccountToken(
	ctx context.Context, tx *sql.Tx, adminID, purpose, tokenHash, pendingEmail string,
	expiresAt, now time.Time,
) error {
	id, err := newID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO account_token(
			id, admin_user_id, purpose, token_hash, pending_email, expires_at, created_at
		) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		id, adminID, purpose, tokenHash, pendingEmail, formatTime(expiresAt), formatTime(now),
	)
	return err
}

func (s *Store) consumeAccountToken(
	ctx context.Context, tokenHash, purpose string, now time.Time,
	apply func(*sql.Tx, string, string) error,
) error {
	if tokenHash == "" || now.IsZero() {
		return ErrInvalidAccountToken
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var adminID, expires string
	var pendingEmail sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT admin_user_id, expires_at, pending_email FROM account_token
		WHERE token_hash=? AND purpose=? AND consumed_at IS NULL`, tokenHash, purpose,
	).Scan(&adminID, &expires, &pendingEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidAccountToken
	}
	if err != nil {
		return err
	}
	expiresAt, err := parseTime(expires)
	if err != nil || !expiresAt.After(now) {
		return ErrInvalidAccountToken
	}
	if err := apply(tx, adminID, pendingEmail.String); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE account_token SET consumed_at=?
		WHERE token_hash=? AND consumed_at IS NULL`, formatTime(now), tokenHash)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return ErrInvalidAccountToken
	}
	return tx.Commit()
}

func isUniqueConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}
