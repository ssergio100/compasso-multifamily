package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestAccountLifecycleCreatesFamilyOnlyAfterConfirmation(t *testing.T) {
	ctx := context.Background()
	store := openEmptyTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)

	shouldSend, err := store.BeginRegistration(
		ctx, "Família Silva", " OWNER@EXAMPLE.COM ", "password-hash", "confirmation-hash", now.Add(24*time.Hour), now,
	)
	if err != nil || !shouldSend {
		t.Fatalf("begin registration send=%t err=%v", shouldSend, err)
	}
	var families, pendingAccounts int
	if err := store.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM family),
		       (SELECT COUNT(*) FROM admin_user WHERE email_verified_at IS NULL)`,
	).Scan(&families, &pendingAccounts); err != nil {
		t.Fatal(err)
	}
	if families != 0 || pendingAccounts != 1 {
		t.Fatalf("before confirmation families=%d pending=%d", families, pendingAccounts)
	}
	if _, err := store.AdminByLogin(ctx, "owner@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pending account could log in: %v", err)
	}
	var storedTokenHash string
	if err := store.db.QueryRowContext(ctx, `SELECT token_hash FROM account_token`).Scan(&storedTokenHash); err != nil {
		t.Fatal(err)
	}
	if storedTokenHash != "confirmation-hash" {
		t.Fatalf("stored confirmation token=%q", storedTokenHash)
	}

	owner, err := store.ConfirmRegistration(ctx, "confirmation-hash", now.Add(time.Hour))
	if err != nil || !owner.Active || !owner.EmailVerified || owner.Email != "owner@example.com" || owner.FamilyName != "Família Silva" {
		t.Fatalf("confirmed owner=%+v err=%v", owner, err)
	}
	if _, err := store.ConfirmRegistration(ctx, "confirmation-hash", now.Add(2*time.Hour)); !errors.Is(err, ErrInvalidAccountToken) {
		t.Fatalf("confirmation token reused: %v", err)
	}
	shouldSend, err = store.BeginRegistration(
		ctx, "Outra", "owner@example.com", "replacement", "other-hash", now.Add(25*time.Hour), now.Add(time.Hour),
	)
	if err != nil || shouldSend {
		t.Fatalf("confirmed duplicate send=%t err=%v", shouldSend, err)
	}

	destination, shouldSend, err := store.BeginPasswordReset(
		ctx, "owner@example.com", "reset-hash", now.Add(90*time.Minute), now.Add(time.Hour),
	)
	if err != nil || !shouldSend || destination != "owner@example.com" {
		t.Fatalf("begin reset destination=%q send=%t err=%v", destination, shouldSend, err)
	}
	if err := store.ResetPassword(ctx, "reset-hash", "new-password-hash", now.Add(70*time.Minute)); err != nil {
		t.Fatal(err)
	}
	resetOwner, err := store.AdminByID(ctx, owner.ID)
	if err != nil || resetOwner.PasswordHash != "new-password-hash" || resetOwner.AuthGeneration != 2 {
		t.Fatalf("reset owner=%+v err=%v", resetOwner, err)
	}
	if err := store.ResetPassword(ctx, "reset-hash", "again", now.Add(75*time.Minute)); !errors.Is(err, ErrInvalidAccountToken) {
		t.Fatalf("reset token reused: %v", err)
	}

	if err := store.BeginEmailChange(
		ctx, owner.ID, " NEW@EXAMPLE.COM ", "change-hash", now.Add(3*time.Hour), now.Add(2*time.Hour),
	); err != nil {
		t.Fatal(err)
	}
	if err := store.ConfirmEmailChange(ctx, "change-hash", now.Add(150*time.Minute)); err != nil {
		t.Fatal(err)
	}
	changedOwner, err := store.AdminByLogin(ctx, "new@example.com")
	if err != nil || changedOwner.AuthGeneration != 3 || changedOwner.Email != "new@example.com" {
		t.Fatalf("changed owner=%+v err=%v", changedOwner, err)
	}

	device, err := store.CreateDeviceForFamily(ctx, owner.FamilyID, "Computador", "cat", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteActiveFamily(ctx, owner.FamilyID, "nome incorreto"); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong family confirmation error=%v", err)
	}
	if err := store.DeleteActiveFamily(ctx, owner.FamilyID, "Família Silva"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdminByID(ctx, owner.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted owner remains: %v", err)
	}
	if _, _, err := store.LoadDevice(ctx, device.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted family device remains: %v", err)
	}
}

func TestExpiredPendingAccountDoesNotOccupyIdentity(t *testing.T) {
	ctx := context.Background()
	store := openEmptyTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	if send, err := store.BeginRegistration(
		ctx, "Antiga", "pending@example.com", "old-hash", "old-token-hash", now.Add(24*time.Hour), now,
	); err != nil || !send {
		t.Fatalf("old registration send=%t err=%v", send, err)
	}
	later := now.Add(25 * time.Hour)
	if send, err := store.BeginRegistration(
		ctx, "Nova", "pending@example.com", "new-hash", "new-token-hash", later.Add(24*time.Hour), later,
	); err != nil || !send {
		t.Fatalf("replacement registration send=%t err=%v", send, err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_user WHERE lower(email)='pending@example.com'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("pending account count=%d err=%v", count, err)
	}
	if _, err := store.ConfirmRegistration(ctx, "old-token-hash", later); !errors.Is(err, ErrInvalidAccountToken) {
		t.Fatalf("expired token remained usable: %v", err)
	}
	if send, err := store.BeginRegistration(
		ctx, "Abandonada", "abandoned@example.com", "hash", "abandoned-token", later.Add(24*time.Hour), later,
	); err != nil || !send {
		t.Fatalf("abandoned registration send=%t err=%v", send, err)
	}
	if _, err := store.CleanupExpiredAccounts(ctx, later.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_user WHERE email='abandoned@example.com'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expired pending account count=%d err=%v", count, err)
	}
}

func TestConfirmationAppliesPilotFamilyLimitAtomically(t *testing.T) {
	ctx := context.Background()
	store := openEmptyTestStore(t)
	defer store.Close()
	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	if send, err := store.BeginRegistration(
		ctx, "Sem vaga", "waiting@example.com", "hash", "waiting-token-hash", now.Add(24*time.Hour), now,
	); err != nil || !send {
		t.Fatalf("pending registration send=%t err=%v", send, err)
	}
	for index := 0; index < 100; index++ {
		if _, err := store.CreateFamilyOwner(
			ctx, fmt.Sprintf("Família %d", index), fmt.Sprintf("owner-%d@example.com", index), "hash", now,
		); err != nil {
			t.Fatalf("create family %d: %v", index, err)
		}
	}
	if _, err := store.ConfirmRegistration(ctx, "waiting-token-hash", now.Add(time.Hour)); !errors.Is(err, ErrFamilyLimit) {
		t.Fatalf("confirmation beyond family limit error=%v", err)
	}
	var families, memberships int
	if err := store.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM family),
		       (SELECT COUNT(*) FROM family_member WHERE admin_user_id=(SELECT id FROM admin_user WHERE email='waiting@example.com'))`,
	).Scan(&families, &memberships); err != nil {
		t.Fatal(err)
	}
	if families != 100 || memberships != 0 {
		t.Fatalf("family limit was partial: families=%d memberships=%d", families, memberships)
	}
}
