package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/ssergio100/compasso/agent/localauth"
	protocol "github.com/ssergio100/compasso/protocol/v1"
	"github.com/ssergio100/compasso/server/storage"
)

const (
	confirmationLifetime  = 24 * time.Hour
	passwordResetLifetime = 30 * time.Minute
)

type registerAccountRequest struct {
	FamilyName           string `json:"family_name"`
	Email                string `json:"email"`
	Password             string `json:"password"`
	PasswordConfirmation string `json:"password_confirmation"`
	CSRFToken            string `json:"csrf_token"`
}

type emailAccountRequest struct {
	Email     string `json:"email"`
	CSRFToken string `json:"csrf_token"`
}

type tokenAccountRequest struct {
	Token string `json:"token"`
}

type resetPasswordRequest struct {
	Token                string `json:"token"`
	Password             string `json:"password"`
	PasswordConfirmation string `json:"password_confirmation"`
}

type changeAccountPasswordRequest struct {
	CurrentPassword      string `json:"current_password"`
	Password             string `json:"password"`
	PasswordConfirmation string `json:"password_confirmation"`
}

type changeAccountEmailRequest struct {
	CurrentPassword string `json:"current_password"`
	Email           string `json:"email"`
}

type deleteAccountRequest struct {
	CurrentPassword string `json:"current_password"`
	FamilyName      string `json:"family_name"`
}

func (a *App) accountRegisterAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.requireAccountMailer(w) {
		return
	}
	var request registerAccountRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid registration")
		return
	}
	if !requireLoginCSRF(w, r, request.CSRFToken) {
		return
	}
	email, valid := normalizedEmail(request.Email)
	familyName := strings.TrimSpace(request.FamilyName)
	if !valid || familyName == "" || len(familyName) > 120 || !validAccountPassword(request.Password, request.PasswordConfirmation) {
		writeJSONError(w, http.StatusBadRequest, "invalid registration")
		return
	}
	if !a.allowPublicAccountRequest(w, r, "register", email) {
		return
	}
	passwordHash, err := localauth.HashPassword(request.Password, localauth.DefaultArgon2Params)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not secure account")
		return
	}
	rawToken, tokenHash, err := newAccountToken()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	now := a.now()
	shouldSend, err := a.store.BeginRegistration(
		r.Context(), familyName, email, passwordHash, tokenHash, now.Add(confirmationLifetime), now,
	)
	if err != nil && !errors.Is(err, storage.ErrConflict) {
		writeJSONError(w, http.StatusInternalServerError, "could not begin registration")
		return
	}
	if shouldSend {
		a.sendAccountLink(r, email, "Confirme sua conta no Compasso", "confirm", rawToken)
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"message": "Se o endereço puder ser cadastrado, enviaremos as próximas instruções por e-mail.",
	})
}

func (a *App) accountResendConfirmationAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.requireAccountMailer(w) {
		return
	}
	var request emailAccountRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !requireLoginCSRF(w, r, request.CSRFToken) {
		return
	}
	email, valid := normalizedEmail(request.Email)
	if !valid {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !a.allowPublicAccountRequest(w, r, "resend", email) {
		return
	}
	rawToken, tokenHash, err := newAccountToken()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	now := a.now()
	shouldSend, err := a.store.ResendRegistrationConfirmation(r.Context(), email, tokenHash, now.Add(confirmationLifetime), now)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not request confirmation")
		return
	}
	if shouldSend {
		a.sendAccountLink(r, email, "Confirme sua conta no Compasso", "confirm", rawToken)
	}
	writeGenericAccountAccepted(w)
}

func (a *App) accountConfirmAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request tokenAccountRequest
	if err := decodeJSONBody(w, r, &request); err != nil || request.Token == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid or expired confirmation")
		return
	}
	if !a.allowAccountTokenAttempt(w, r, "confirm") {
		return
	}
	_, err := a.store.ConfirmRegistration(r.Context(), hashAccountToken(request.Token), a.now())
	if errors.Is(err, storage.ErrFamilyLimit) || errors.Is(err, storage.ErrPilotNotReady) {
		writeJSONErrorResponse(w, http.StatusConflict, protocol.ErrorResponse{
			Error: "registrations are temporarily closed", Code: "registrations_closed",
		})
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid or expired confirmation")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Conta confirmada. Você já pode entrar."})
}

func (a *App) accountPasswordResetAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.requireAccountMailer(w) {
		return
	}
	var request emailAccountRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !requireLoginCSRF(w, r, request.CSRFToken) {
		return
	}
	email, valid := normalizedEmail(request.Email)
	if !valid {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !a.allowPublicAccountRequest(w, r, "password-reset", email) {
		return
	}
	rawToken, tokenHash, err := newAccountToken()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	now := a.now()
	destination, shouldSend, err := a.store.BeginPasswordReset(r.Context(), email, tokenHash, now.Add(passwordResetLifetime), now)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not request password reset")
		return
	}
	if shouldSend {
		a.sendAccountLink(r, destination, "Redefina sua senha do Compasso", "reset-password", rawToken)
	}
	writeGenericAccountAccepted(w)
}

func (a *App) accountPasswordResetConfirmAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request resetPasswordRequest
	if err := decodeJSONBody(w, r, &request); err != nil || request.Token == "" ||
		!validAccountPassword(request.Password, request.PasswordConfirmation) {
		writeJSONError(w, http.StatusBadRequest, "invalid password reset")
		return
	}
	if !a.allowAccountTokenAttempt(w, r, "password-reset-confirm") {
		return
	}
	passwordHash, err := localauth.HashPassword(request.Password, localauth.DefaultArgon2Params)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not secure password")
		return
	}
	if err := a.store.ResetPassword(r.Context(), hashAccountToken(request.Token), passwordHash, a.now()); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid or expired password reset")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Senha alterada. Entre novamente."})
}

func (a *App) accountEmailConfirmAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request tokenAccountRequest
	if err := decodeJSONBody(w, r, &request); err != nil || request.Token == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid or expired confirmation")
		return
	}
	if !a.allowAccountTokenAttempt(w, r, "email-confirm") {
		return
	}
	if err := a.store.ConfirmEmailChange(r.Context(), hashAccountToken(request.Token), a.now()); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid or expired confirmation")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "E-mail alterado. Entre novamente."})
}

func (a *App) adminAccountAPI(w http.ResponseWriter, r *http.Request) {
	current, sessionToken, authenticated := a.requireAdminAPISession(w, r)
	if !authenticated {
		return
	}
	admin, err := a.store.AdminByID(r.Context(), current.AdminID)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]string{"email": admin.Email, "family_name": admin.FamilyName})
	case http.MethodDelete:
		if !requireAdminCSRF(w, r, current) {
			return
		}
		var request deleteAccountRequest
		if err := decodeJSONBody(w, r, &request); err != nil || !verifyAccountPassword(request.CurrentPassword, admin.PasswordHash) {
			writeJSONError(w, http.StatusForbidden, "current password is invalid")
			return
		}
		if err := a.store.DeleteActiveFamily(r.Context(), current.FamilyID, request.FamilyName); err != nil {
			if errors.Is(err, storage.ErrConflict) {
				writeJSONError(w, http.StatusConflict, "family name does not match")
			} else {
				writeJSONError(w, http.StatusBadRequest, "could not delete account")
			}
			return
		}
		a.sessions.delete(sessionToken)
		a.setCookie(w, &http.Cookie{Name: sessionCookieName, MaxAge: -1, HttpOnly: true})
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) adminAccountPasswordAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	current, _, authenticated := a.requireAdminAPISession(w, r)
	if !authenticated || !requireAdminCSRF(w, r, current) {
		return
	}
	var request changeAccountPasswordRequest
	if err := decodeJSONBody(w, r, &request); err != nil || !validAccountPassword(request.Password, request.PasswordConfirmation) {
		writeJSONError(w, http.StatusBadRequest, "invalid password")
		return
	}
	admin, err := a.store.AdminByID(r.Context(), current.AdminID)
	if err != nil || !verifyAccountPassword(request.CurrentPassword, admin.PasswordHash) {
		writeJSONError(w, http.StatusForbidden, "current password is invalid")
		return
	}
	passwordHash, err := localauth.HashPassword(request.Password, localauth.DefaultArgon2Params)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not secure password")
		return
	}
	if err := a.store.ChangePassword(r.Context(), current.AdminID, passwordHash, a.now()); err != nil {
		writeJSONError(w, http.StatusBadRequest, "could not change password")
		return
	}
	a.sessions.deleteByAdmin(current.AdminID)
	writeJSON(w, http.StatusOK, map[string]string{"message": "Senha alterada. Entre novamente."})
}

func (a *App) adminAccountEmailAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.requireAccountMailer(w) {
		return
	}
	current, _, authenticated := a.requireAdminAPISession(w, r)
	if !authenticated || !requireAdminCSRF(w, r, current) {
		return
	}
	var request changeAccountEmailRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request")
		return
	}
	email, valid := normalizedEmail(request.Email)
	admin, err := a.store.AdminByID(r.Context(), current.AdminID)
	if !valid || err != nil || !verifyAccountPassword(request.CurrentPassword, admin.PasswordHash) {
		writeJSONError(w, http.StatusForbidden, "current password or e-mail is invalid")
		return
	}
	rawToken, tokenHash, err := newAccountToken()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	now := a.now()
	if err := a.store.BeginEmailChange(r.Context(), current.AdminID, email, tokenHash, now.Add(confirmationLifetime), now); err != nil {
		if errors.Is(err, storage.ErrConflict) {
			writeJSONError(w, http.StatusConflict, "e-mail is unavailable")
		} else {
			writeJSONError(w, http.StatusBadRequest, "could not change e-mail")
		}
		return
	}
	a.sendAccountLink(r, email, "Confirme seu novo e-mail no Compasso", "confirm-email", rawToken)
	writeGenericAccountAccepted(w)
}

func requireLoginCSRF(w http.ResponseWriter, r *http.Request, provided string) bool {
	cookie, err := r.Cookie(loginCSRFCookie)
	if err != nil || !constantEqual(cookie.Value, provided) {
		writeJSONError(w, http.StatusForbidden, "invalid CSRF token")
		return false
	}
	return true
}

func normalizedEmail(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || len(normalized) > 254 || strings.ContainsAny(normalized, "\r\n") {
		return "", false
	}
	address, err := mail.ParseAddress(normalized)
	return normalized, err == nil && address.Address == normalized && strings.Contains(normalized, "@")
}

func validAccountPassword(password, confirmation string) bool {
	return password == confirmation && len(password) >= 12 && len(password) <= 4096
}

func verifyAccountPassword(password, passwordHash string) bool {
	valid, err := localauth.VerifyPassword(password, passwordHash)
	return err == nil && valid
}

func newAccountToken() (string, string, error) {
	raw, err := randomToken()
	if err != nil {
		return "", "", err
	}
	return raw, hashAccountToken(raw), nil
}

func hashAccountToken(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func (a *App) allowPublicAccountRequest(w http.ResponseWriter, r *http.Request, operation, email string) bool {
	now := a.now()
	if !a.rateLimits.take("account:"+operation+":email:"+email, 5, 15*time.Minute, now) ||
		!a.rateLimits.take("account:"+operation+":network:"+requestNetwork(r), 20, 15*time.Minute, now) {
		w.Header().Set("Retry-After", "900")
		writeJSONError(w, http.StatusTooManyRequests, "too many requests")
		return false
	}
	return true
}

func (a *App) allowAccountTokenAttempt(w http.ResponseWriter, r *http.Request, operation string) bool {
	if a.rateLimits.take("account:"+operation+":network:"+requestNetwork(r), 20, 15*time.Minute, a.now()) {
		return true
	}
	w.Header().Set("Retry-After", "900")
	writeJSONError(w, http.StatusTooManyRequests, "too many requests")
	return false
}

func requestNetwork(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func (a *App) sendAccountLink(r *http.Request, destination, subject, action, rawToken string) {
	link, err := a.accountLink(r, action, rawToken)
	if err != nil {
		log.Printf("account e-mail link unavailable action=%s error=%v", action, err)
		return
	}
	if err := a.accountMailer.SendAccountMessage(r.Context(), AccountMessage{
		To: destination, Subject: subject,
		Text: "Use este link para continuar no Compasso:\n\n" + link + "\n\nSe você não fez este pedido, ignore esta mensagem.",
	}); err != nil {
		log.Printf("account e-mail delivery failed action=%s error=%v", action, err)
	}
}

func (a *App) accountLink(r *http.Request, action, rawToken string) (string, error) {
	origin := a.adminOrigin
	if origin == "" {
		return "", errors.New("administrative origin is not configured")
	}
	if origin == "same-host" {
		scheme := "http"
		if a.secureCookies {
			scheme = "https"
		}
		origin = scheme + "://" + r.Host
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return "", errors.New("invalid administrative origin")
	}
	parsed.Path = "/"
	query := parsed.Query()
	query.Set("account_action", action)
	query.Set("token", rawToken)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func writeGenericAccountAccepted(w http.ResponseWriter) {
	writeJSON(w, http.StatusAccepted, map[string]string{
		"message": "Se o endereço corresponder a uma solicitação válida, enviaremos as próximas instruções por e-mail.",
	})
}

func (a *App) requireAccountMailer(w http.ResponseWriter) bool {
	if !a.accountMailer.Available() {
		writeJSONError(w, http.StatusServiceUnavailable, "account e-mail delivery is unavailable")
		return false
	}
	return true
}

// StartAccountMaintenance bounds pending-account and expired-token retention
// without requiring an external scheduler in the single-process pilot.
func (a *App) StartAccountMaintenance(ctx context.Context) {
	go func() {
		cleanup := func() {
			if _, err := a.store.CleanupExpiredAccounts(ctx, a.now()); err != nil && ctx.Err() == nil {
				log.Printf("account cleanup failed: %v", err)
			}
		}
		cleanup()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleanup()
			}
		}
	}()
}
