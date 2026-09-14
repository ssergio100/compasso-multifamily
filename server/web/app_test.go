package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ssergio100/compasso/agent/localauth"
	protocol "github.com/ssergio100/compasso/protocol/v1"
	serverstorage "github.com/ssergio100/compasso/server/storage"
)

type webFixture struct {
	app           *App
	store         *serverstorage.Store
	now           time.Time
	sessionCookie *http.Cookie
	csrfToken     string
}

func TestAdministrativeSessionCORSAndSecureCookie(t *testing.T) {
	fixture := newWebFixture(t, true, time.Hour)
	defer fixture.store.Close()
	adminOrigin := "https://admin.example"
	if err := fixture.app.SetAdminOrigin(adminOrigin); err != nil {
		t.Fatal(err)
	}

	forbiddenRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/session", nil)
	forbiddenRequest.Header.Set("Origin", "https://attacker.example")
	forbiddenResponse := httptest.NewRecorder()
	fixture.app.ServeHTTP(forbiddenResponse, forbiddenRequest)
	if forbiddenResponse.Code != http.StatusForbidden {
		t.Fatalf("forbidden origin status=%d", forbiddenResponse.Code)
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/session", nil)
	sessionRequest.Header.Set("Origin", adminOrigin)
	sessionResponse := httptest.NewRecorder()
	fixture.app.ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK || sessionResponse.Header().Get("Access-Control-Allow-Origin") != adminOrigin {
		t.Fatalf("session bootstrap status=%d headers=%v", sessionResponse.Code, sessionResponse.Header())
	}
	loginCSRFTokenCookie := findCookie(t, sessionResponse.Result().Cookies(), loginCSRFCookie)
	if loginCSRFTokenCookie.Path != "/api/v1" || !loginCSRFTokenCookie.HttpOnly || !loginCSRFTokenCookie.Secure {
		t.Fatalf("unsafe login CSRF cookie: %+v", loginCSRFTokenCookie)
	}
	var anonymousSession adminSessionResponse
	decodeResponse(t, sessionResponse, &anonymousSession)

	wrongLoginBody, _ := json.Marshal(adminLoginRequest{
		Login: "admin", Password: "wrong", CSRFToken: anonymousSession.CSRFToken,
	})
	wrongLoginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/session", bytes.NewReader(wrongLoginBody))
	wrongLoginRequest.Header.Set("Content-Type", "application/json")
	wrongLoginRequest.AddCookie(loginCSRFTokenCookie)
	wrongLoginResponse := httptest.NewRecorder()
	fixture.app.ServeHTTP(wrongLoginResponse, wrongLoginRequest)
	if wrongLoginResponse.Code != http.StatusUnauthorized || strings.Contains(wrongLoginResponse.Body.String(), "admin") {
		t.Fatalf("wrong login status=%d body=%s", wrongLoginResponse.Code, wrongLoginResponse.Body.String())
	}

	loginBody, _ := json.Marshal(adminLoginRequest{
		Login: "admin", Password: "secret", CSRFToken: anonymousSession.CSRFToken,
	})
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/session", bytes.NewReader(loginBody))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("Origin", adminOrigin)
	loginRequest.AddCookie(loginCSRFTokenCookie)
	loginResponse := httptest.NewRecorder()
	fixture.app.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("API login status=%d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	adminSessionCookie := findCookie(t, loginResponse.Result().Cookies(), sessionCookieName)
	if !adminSessionCookie.HttpOnly || !adminSessionCookie.Secure || adminSessionCookie.SameSite != http.SameSiteStrictMode ||
		adminSessionCookie.Path != "/api/v1/admin" {
		t.Fatalf("unsafe session cookie: %+v", adminSessionCookie)
	}
	if loginResponse.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("production API response is missing HSTS")
	}
}

func TestAdministrativeLoginBlocksAfterTenFailures(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	sessionResponse := fixture.requestJSON(http.MethodGet, "/api/v1/admin/session", nil, false)
	var anonymousSession adminSessionResponse
	decodeResponse(t, sessionResponse, &anonymousSession)
	csrfCookie := findCookie(t, sessionResponse.Result().Cookies(), loginCSRFCookie)

	requestLogin := func(password string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(adminLoginRequest{
			Login: "admin", Password: password, CSRFToken: anonymousSession.CSRFToken,
		})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/session", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(csrfCookie)
		response := httptest.NewRecorder()
		fixture.app.ServeHTTP(response, request)
		return response
	}
	for attempt := 1; attempt <= 10; attempt++ {
		response := requestLogin("wrong-password")
		want := http.StatusUnauthorized
		if attempt == 10 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("login failure %d status=%d want=%d body=%s", attempt, response.Code, want, response.Body.String())
		}
	}
	if response := requestLogin("secret"); response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "900" {
		t.Fatalf("blocked correct login status=%d retry=%q", response.Code, response.Header().Get("Retry-After"))
	}
	fixture.app.now = func() time.Time { return fixture.now.Add(16 * time.Minute) }
	if response := requestLogin("secret"); response.Code != http.StatusOK {
		t.Fatalf("login remained blocked status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSameHostAdministrativeOriginForLocalNetworkInstallation(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	if err := fixture.app.SetAdminOrigin("same-host"); err != nil {
		t.Fatal(err)
	}

	allowedRequest := httptest.NewRequest(http.MethodGet, "http://192.168.1.20:8181/api/v1/admin/session", nil)
	allowedRequest.Header.Set("Origin", "http://192.168.1.20:8182")
	allowedResponse := httptest.NewRecorder()
	fixture.app.ServeHTTP(allowedResponse, allowedRequest)
	if allowedResponse.Code != http.StatusOK || allowedResponse.Header().Get("Access-Control-Allow-Origin") != "http://192.168.1.20:8182" {
		t.Fatalf("same-host origin rejected: status=%d headers=%v", allowedResponse.Code, allowedResponse.Header())
	}

	forbiddenRequest := httptest.NewRequest(http.MethodGet, "http://192.168.1.20:8181/api/v1/admin/session", nil)
	forbiddenRequest.Header.Set("Origin", "http://192.168.1.30:8182")
	forbiddenResponse := httptest.NewRecorder()
	fixture.app.ServeHTTP(forbiddenResponse, forbiddenRequest)
	if forbiddenResponse.Code != http.StatusForbidden {
		t.Fatalf("different-host origin status=%d", forbiddenResponse.Code)
	}
}

func TestFirstFamilyUsesPublicRegistrationInsteadOfInitialSetup(t *testing.T) {
	ctx := context.Background()
	store, err := serverstorage.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	mailer := &recordingAccountMailer{}
	application, err := New(
		store, false, time.Hour, time.Minute, 3*time.Second, "https://admin.example",
		WithAccountMailer(mailer),
	)
	if err != nil {
		t.Fatal(err)
	}
	application.now = func() time.Time { return time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC) }

	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/session", nil)
	sessionResponse := httptest.NewRecorder()
	application.ServeHTTP(sessionResponse, sessionRequest)
	var anonymousSession adminSessionResponse
	decodeResponse(t, sessionResponse, &anonymousSession)
	if anonymousSession.SetupRequired || anonymousSession.Authenticated {
		t.Fatalf("fresh installation session=%+v", anonymousSession)
	}
	loginCSRFCookie := findCookie(t, sessionResponse.Result().Cookies(), loginCSRFCookie)

	registrationBody, _ := json.Marshal(registerAccountRequest{
		FamilyName: "Família Silva", Email: "sergio@example.com",
		Password: "senha-memoravel", PasswordConfirmation: "senha-memoravel",
		CSRFToken: anonymousSession.CSRFToken,
	})
	registrationRequest := httptest.NewRequest(http.MethodPost, "/api/v1/account/register", bytes.NewReader(registrationBody))
	registrationRequest.Header.Set("Content-Type", "application/json")
	registrationRequest.AddCookie(loginCSRFCookie)
	registrationResponse := httptest.NewRecorder()
	application.ServeHTTP(registrationResponse, registrationRequest)
	if registrationResponse.Code != http.StatusAccepted || len(mailer.messages) != 1 {
		t.Fatalf("registration status=%d messages=%+v body=%s", registrationResponse.Code, mailer.messages, registrationResponse.Body.String())
	}
	if _, err := store.AdminByLogin(ctx, "sergio@example.com"); !errors.Is(err, serverstorage.ErrNotFound) {
		t.Fatalf("unconfirmed account could log in: %v", err)
	}
	token := accountTokenFromMessage(t, mailer.messages[0])
	confirmationBody, _ := json.Marshal(tokenAccountRequest{Token: token})
	confirmationRequest := httptest.NewRequest(http.MethodPost, "/api/v1/account/confirm", bytes.NewReader(confirmationBody))
	confirmationRequest.Header.Set("Content-Type", "application/json")
	confirmationResponse := httptest.NewRecorder()
	application.ServeHTTP(confirmationResponse, confirmationRequest)
	if confirmationResponse.Code != http.StatusOK {
		t.Fatalf("confirmation status=%d body=%s", confirmationResponse.Code, confirmationResponse.Body.String())
	}
	administrator, err := store.AdminByLogin(ctx, "sergio@example.com")
	if err != nil {
		t.Fatal(err)
	}
	passwordMatches, err := localauth.VerifyPassword("senha-memoravel", administrator.PasswordHash)
	if err != nil || !passwordMatches || !administrator.EmailVerified || administrator.FamilyName != "Família Silva" {
		t.Fatalf("configured password was not stored securely: matches=%t err=%v", passwordMatches, err)
	}
	duplicateRequest := httptest.NewRequest(http.MethodPost, "/api/v1/account/register", bytes.NewReader(registrationBody))
	duplicateRequest.Header.Set("Content-Type", "application/json")
	duplicateRequest.AddCookie(loginCSRFCookie)
	duplicateResponse := httptest.NewRecorder()
	application.ServeHTTP(duplicateResponse, duplicateRequest)
	if duplicateResponse.Code != http.StatusAccepted || len(mailer.messages) != 1 || duplicateResponse.Body.String() != registrationResponse.Body.String() {
		t.Fatalf("existing account was enumerated: status=%d messages=%d body=%s", duplicateResponse.Code, len(mailer.messages), duplicateResponse.Body.String())
	}

	resetBody, _ := json.Marshal(emailAccountRequest{
		Email: "sergio@example.com", CSRFToken: anonymousSession.CSRFToken,
	})
	resetRequest := httptest.NewRequest(http.MethodPost, "/api/v1/account/password-reset", bytes.NewReader(resetBody))
	resetRequest.Header.Set("Content-Type", "application/json")
	resetRequest.AddCookie(loginCSRFCookie)
	resetResponse := httptest.NewRecorder()
	application.ServeHTTP(resetResponse, resetRequest)
	if resetResponse.Code != http.StatusAccepted || len(mailer.messages) != 2 {
		t.Fatalf("reset request status=%d messages=%d body=%s", resetResponse.Code, len(mailer.messages), resetResponse.Body.String())
	}
	resetToken := accountTokenFromMessage(t, mailer.messages[1])
	resetConfirmationBody, _ := json.Marshal(resetPasswordRequest{
		Token: resetToken, Password: "nova-senha-memoravel", PasswordConfirmation: "nova-senha-memoravel",
	})
	resetConfirmationRequest := httptest.NewRequest(http.MethodPost, "/api/v1/account/password-reset/confirm", bytes.NewReader(resetConfirmationBody))
	resetConfirmationRequest.Header.Set("Content-Type", "application/json")
	resetConfirmationResponse := httptest.NewRecorder()
	application.ServeHTTP(resetConfirmationResponse, resetConfirmationRequest)
	if resetConfirmationResponse.Code != http.StatusOK {
		t.Fatalf("reset confirmation status=%d body=%s", resetConfirmationResponse.Code, resetConfirmationResponse.Body.String())
	}

	accountFixture := &webFixture{app: application, store: store, now: application.now()}
	accountFixture.loginAs(t, "sergio@example.com", "nova-senha-memoravel")
	emailChange := accountFixture.requestJSON(http.MethodPost, "/api/v1/admin/account/email", changeAccountEmailRequest{
		CurrentPassword: "nova-senha-memoravel", Email: "novo@example.com",
	}, true)
	if emailChange.Code != http.StatusAccepted || len(mailer.messages) != 3 {
		t.Fatalf("email change status=%d messages=%d body=%s", emailChange.Code, len(mailer.messages), emailChange.Body.String())
	}
	emailToken := accountTokenFromMessage(t, mailer.messages[2])
	emailConfirmation := accountFixture.requestJSON(http.MethodPost, "/api/v1/account/email/confirm", tokenAccountRequest{Token: emailToken}, false)
	if emailConfirmation.Code != http.StatusOK {
		t.Fatalf("email confirmation status=%d body=%s", emailConfirmation.Code, emailConfirmation.Body.String())
	}
	if stale := accountFixture.requestJSON(http.MethodGet, "/api/v1/admin/account", nil, false); stale.Code != http.StatusUnauthorized {
		t.Fatalf("email change kept old session status=%d", stale.Code)
	}
	accountFixture.sessionCookie = nil
	accountFixture.loginAs(t, "novo@example.com", "nova-senha-memoravel")
	passwordChange := accountFixture.requestJSON(http.MethodPut, "/api/v1/admin/account/password", changeAccountPasswordRequest{
		CurrentPassword: "nova-senha-memoravel", Password: "senha-final-memoravel", PasswordConfirmation: "senha-final-memoravel",
	}, true)
	if passwordChange.Code != http.StatusOK {
		t.Fatalf("password change status=%d body=%s", passwordChange.Code, passwordChange.Body.String())
	}
	accountFixture.sessionCookie = nil
	accountFixture.loginAs(t, "novo@example.com", "senha-final-memoravel")
	deviceResponse := accountFixture.requestJSON(http.MethodPost, "/api/v1/admin/devices", createAdminDeviceRequest{
		Name: "Computador da família", AvatarKey: "cat_bow",
	}, true)
	var familyDevice adminDeviceResponse
	decodeResponse(t, deviceResponse, &familyDevice)
	if deviceResponse.Code != http.StatusCreated || familyDevice.ID == "" {
		t.Fatalf("family device creation status=%d body=%s", deviceResponse.Code, deviceResponse.Body.String())
	}
	credentialResponse := accountFixture.requestJSON(
		http.MethodPost, "/api/v1/admin/devices/"+familyDevice.ID+"/token",
		sensitiveDeviceRequest{CurrentPassword: "senha-final-memoravel"}, true,
	)
	var familyCredential map[string]string
	decodeResponse(t, credentialResponse, &familyCredential)
	if credentialResponse.Code != http.StatusCreated || familyCredential["device_token"] == "" {
		t.Fatalf("family credential status=%d body=%s", credentialResponse.Code, credentialResponse.Body.String())
	}
	accountResponse := accountFixture.requestJSON(http.MethodGet, "/api/v1/admin/account", nil, false)
	var accountDetails map[string]string
	decodeResponse(t, accountResponse, &accountDetails)
	if accountResponse.Code != http.StatusOK || accountDetails["family_name"] != "Família Silva" || accountDetails["email"] != "novo@example.com" {
		t.Fatalf("account details status=%d values=%+v", accountResponse.Code, accountDetails)
	}
	deleteResponse := accountFixture.requestJSON(http.MethodDelete, "/api/v1/admin/account", deleteAccountRequest{
		CurrentPassword: "senha-final-memoravel", FamilyName: "Família Silva",
	}, true)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("account deletion status=%d body=%s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if _, err := store.AdminByID(ctx, administrator.ID); !errors.Is(err, serverstorage.ErrNotFound) {
		t.Fatalf("deleted account remains: %v", err)
	}
	if _, _, err := store.LoadDevice(ctx, familyDevice.ID); !errors.Is(err, serverstorage.ErrNotFound) {
		t.Fatalf("deleted family device remains: %v", err)
	}

	legacySetupRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/setup", bytes.NewReader(registrationBody))
	legacySetupResponse := httptest.NewRecorder()
	application.ServeHTTP(legacySetupResponse, legacySetupRequest)
	if legacySetupResponse.Code != http.StatusNotFound {
		t.Fatalf("legacy setup remains public: status=%d body=%s", legacySetupResponse.Code, legacySetupResponse.Body.String())
	}
}

func TestAdministrativeJSONAPIWorkflow(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	fixture.login(t)

	missingCSRFResponse := fixture.requestJSON(
		http.MethodPost, "/api/v1/admin/devices", createAdminDeviceRequest{Name: "PC"}, false,
	)
	if missingCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d", missingCSRFResponse.Code)
	}

	createdResponse := fixture.requestJSON(
		http.MethodPost, "/api/v1/admin/devices", createAdminDeviceRequest{Name: "Debian KDE", AvatarKey: "cat_bow"}, true,
	)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create device status=%d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var createdDevice adminDeviceResponse
	decodeResponse(t, createdResponse, &createdDevice)
	devicePath := "/api/v1/admin/devices/" + createdDevice.ID

	policyResponse := fixture.requestJSON(http.MethodPut, devicePath+"/policy", updateAdminPolicyRequest{
		WeeklyQuota: [7]int64{0, 3600, 7200, 0, 0, 0, 0}, WarningMinutes: 5,
	}, true)
	if policyResponse.Code != http.StatusOK {
		t.Fatalf("update policy status=%d body=%s", policyResponse.Code, policyResponse.Body.String())
	}

	routineResponse := fixture.requestJSON(http.MethodPost, devicePath+"/routines", saveAdminRoutineRequest{
		Name: "Dormir", IconKey: "sleep", Days: [7]bool{false, true, true, true, true, true, false},
		Start: 22 * 3600, End: 8 * 3600, Enabled: true,
	}, true)
	if routineResponse.Code != http.StatusCreated {
		t.Fatalf("create routine status=%d body=%s", routineResponse.Code, routineResponse.Body.String())
	}
	var createdRoutine map[string]string
	decodeResponse(t, routineResponse, &createdRoutine)
	routineID := createdRoutine["id"]
	conflictResponse := fixture.requestJSON(http.MethodPost, devicePath+"/routines", saveAdminRoutineRequest{
		Name: "Leitura", Days: [7]bool{false, true, false, false, false, false, false},
		Start: 23 * 3600, End: 23*3600 + 30*60, Enabled: true,
	}, true)
	if conflictResponse.Code != http.StatusConflict || !strings.Contains(conflictResponse.Body.String(), "Dormir") {
		t.Fatalf("routine conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}
	updatedRoutineResponse := fixture.requestJSON(http.MethodPut, devicePath+"/routines/"+routineID, saveAdminRoutineRequest{
		Name: "Dormir cedo", IconKey: "reading", Days: [7]bool{false, true, true, true, true, true, false},
		Start: 21 * 3600, End: 8 * 3600, Enabled: true,
	}, true)
	if updatedRoutineResponse.Code != http.StatusOK {
		t.Fatalf("update routine status=%d body=%s", updatedRoutineResponse.Code, updatedRoutineResponse.Body.String())
	}

	password := "new responsible secret"
	passwordResponse := fixture.requestJSON(http.MethodPut, devicePath+"/password", updateAdminPasswordRequest{
		Password: password, PasswordConfirmation: password,
	}, true)
	if passwordResponse.Code != http.StatusOK || strings.Contains(passwordResponse.Body.String(), password) ||
		strings.Contains(passwordResponse.Body.String(), "$argon2id$") {
		t.Fatalf("password response status=%d body=%s", passwordResponse.Code, passwordResponse.Body.String())
	}

	wrongTokenResponse := fixture.requestJSON(http.MethodPost, devicePath+"/token", sensitiveDeviceRequest{CurrentPassword: "wrong"}, true)
	if wrongTokenResponse.Code != http.StatusForbidden {
		t.Fatalf("issue token with wrong account password status=%d body=%s", wrongTokenResponse.Code, wrongTokenResponse.Body.String())
	}
	tokenResponse := fixture.requestJSON(http.MethodPost, devicePath+"/token", sensitiveDeviceRequest{CurrentPassword: "secret"}, true)
	if tokenResponse.Code != http.StatusCreated {
		t.Fatalf("issue token status=%d body=%s", tokenResponse.Code, tokenResponse.Body.String())
	}
	var issuedToken map[string]string
	decodeResponse(t, tokenResponse, &issuedToken)
	if issuedToken["device_token"] == "" {
		t.Fatal("issued device token is empty")
	}
	bonusResponse := fixture.requestJSON(
		http.MethodPost, devicePath+"/bonus", addAdminBonusRequest{Minutes: 10}, true,
	)
	if bonusResponse.Code != http.StatusAccepted {
		t.Fatalf("queue bonus status=%d body=%s", bonusResponse.Code, bonusResponse.Body.String())
	}
	var queuedBonus adminBonusResponse
	decodeResponse(t, bonusResponse, &queuedBonus)
	commandResponse := fixture.requestJSON(http.MethodPost, devicePath+"/commands", queueAdminCommandRequest{
		Command: "pause_monitoring",
	}, true)
	if commandResponse.Code != http.StatusAccepted {
		t.Fatalf("queue command status=%d body=%s", commandResponse.Code, commandResponse.Body.String())
	}
	if _, err := fixture.store.ReceiveHeartbeat(context.Background(), createdDevice.ID, protocol.HeartbeatRequest{
		PolicyRevision: 0, LocalDate: fixture.now.Format("2006-01-02"),
	}, fixture.now); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ReceiveHeartbeat(context.Background(), createdDevice.ID, protocol.HeartbeatRequest{
		PolicyRevision: 0, LocalDate: fixture.now.Format("2006-01-02"), CommandAcks: []string{queuedBonus.OperationID},
	}, fixture.now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	detailResponse := fixture.requestJSON(http.MethodGet, devicePath, nil, false)
	if detailResponse.Code != http.StatusOK || strings.Contains(detailResponse.Body.String(), "local_password_verifier") ||
		strings.Contains(detailResponse.Body.String(), "$argon2id$") || strings.Contains(detailResponse.Body.String(), issuedToken["device_token"]) {
		t.Fatalf("device detail leaked secret or failed: status=%d body=%s", detailResponse.Code, detailResponse.Body.String())
	}
	var detail adminDeviceDetailResponse
	decodeResponse(t, detailResponse, &detail)
	if detail.Device.AvatarKey != "cat_bow" || detail.Policy.WeeklyQuota[time.Monday] != 3600 || detail.Policy.WarningMinutes != 5 ||
		len(detail.Policy.Routines) != 1 || detail.Policy.Routines[0].IconKey != "reading" || !detail.Policy.PasswordSet || detail.Policy.MonitoringPaused {
		t.Fatalf("unexpected device detail: %+v", detail)
	}

	statusResponse := fixture.requestJSON(http.MethodGet, devicePath+"/status", nil, false)
	var liveStatus deviceLiveStatus
	decodeResponse(t, statusResponse, &liveStatus)
	if statusResponse.Code != http.StatusOK || liveStatus.LocalDate != "2026-08-10" ||
		liveStatus.TodayQuotaSeconds != 3600 || liveStatus.BonusSeconds != 600 || liveStatus.RemainingSeconds != 4200 {
		t.Fatalf("unexpected live status: code=%d value=%+v", statusResponse.Code, liveStatus)
	}
	eventsResponse := fixture.requestJSON(http.MethodGet, devicePath+"/events?limit=20", nil, false)
	if eventsResponse.Code != http.StatusOK || !strings.Contains(eventsResponse.Body.String(), "local_password_changed") {
		t.Fatalf("events status=%d body=%s", eventsResponse.Code, eventsResponse.Body.String())
	}

	renameResponse := fixture.requestJSON(
		http.MethodPatch, devicePath, updateAdminDeviceRequest{Name: "PC do quarto", AvatarKey: "fox_bow"}, true,
	)
	if renameResponse.Code != http.StatusOK {
		t.Fatalf("rename status=%d body=%s", renameResponse.Code, renameResponse.Body.String())
	}
	renamedDetailResponse := fixture.requestJSON(http.MethodGet, devicePath, nil, false)
	var renamedDetail adminDeviceDetailResponse
	decodeResponse(t, renamedDetailResponse, &renamedDetail)
	if renamedDetailResponse.Code != http.StatusOK || renamedDetail.Device.AvatarKey != "fox_bow" {
		t.Fatalf("updated avatar status=%d detail=%+v", renamedDetailResponse.Code, renamedDetail.Device)
	}
	deleteRoutineResponse := fixture.requestJSON(http.MethodDelete, devicePath+"/routines/"+routineID, nil, true)
	if deleteRoutineResponse.Code != http.StatusNoContent {
		t.Fatalf("delete routine status=%d", deleteRoutineResponse.Code)
	}
	wrongRevokeResponse := fixture.requestJSON(http.MethodDelete, devicePath+"/token", sensitiveDeviceRequest{CurrentPassword: "wrong"}, true)
	if wrongRevokeResponse.Code != http.StatusForbidden {
		t.Fatalf("revoke token with wrong account password status=%d body=%s", wrongRevokeResponse.Code, wrongRevokeResponse.Body.String())
	}
	if err := fixture.store.AuthenticateDevice(context.Background(), createdDevice.ID, issuedToken["device_token"]); err != nil {
		t.Fatalf("wrong account password revoked token: %v", err)
	}
	revokeTokenResponse := fixture.requestJSON(http.MethodDelete, devicePath+"/token", sensitiveDeviceRequest{CurrentPassword: "secret"}, true)
	if revokeTokenResponse.Code != http.StatusNoContent {
		t.Fatalf("revoke token status=%d", revokeTokenResponse.Code)
	}
	listResponse := fixture.requestJSON(http.MethodGet, "/api/v1/admin/devices", nil, false)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), "PC do quarto") {
		t.Fatalf("list devices status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	wrongDeleteResponse := fixture.requestJSON(http.MethodDelete, devicePath, sensitiveDeviceRequest{CurrentPassword: "wrong"}, true)
	if wrongDeleteResponse.Code != http.StatusForbidden {
		t.Fatalf("delete device with wrong account password status=%d body=%s", wrongDeleteResponse.Code, wrongDeleteResponse.Body.String())
	}
	if _, _, err := fixture.store.LoadDevice(context.Background(), createdDevice.ID); err != nil {
		t.Fatalf("wrong account password deleted device: %v", err)
	}
	deleteDeviceResponse := fixture.requestJSON(http.MethodDelete, devicePath, sensitiveDeviceRequest{CurrentPassword: "secret"}, true)
	if deleteDeviceResponse.Code != http.StatusNoContent {
		t.Fatalf("delete device status=%d", deleteDeviceResponse.Code)
	}
}

func TestAdministrativeVisualIdentityValidation(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	fixture.login(t)

	invalidDevice := fixture.requestJSON(http.MethodPost, "/api/v1/admin/devices", createAdminDeviceRequest{
		Name: "PC", AvatarKey: "not-an-avatar",
	}, true)
	if invalidDevice.Code != http.StatusBadRequest {
		t.Fatalf("invalid avatar status=%d body=%s", invalidDevice.Code, invalidDevice.Body.String())
	}
	created := fixture.requestJSON(http.MethodPost, "/api/v1/admin/devices", createAdminDeviceRequest{
		Name: "PC", AvatarKey: "rabbit_flower",
	}, true)
	var device adminDeviceResponse
	decodeResponse(t, created, &device)
	if created.Code != http.StatusCreated || device.AvatarKey != "rabbit_flower" {
		t.Fatalf("created identity status=%d device=%+v", created.Code, device)
	}
	invalidRoutine := fixture.requestJSON(http.MethodPost, "/api/v1/admin/devices/"+device.ID+"/routines", saveAdminRoutineRequest{
		Name: "Rotina", IconKey: "not-an-icon", Days: [7]bool{false, true}, Start: 3600, End: 7200, Enabled: true,
	}, true)
	if invalidRoutine.Code != http.StatusBadRequest {
		t.Fatalf("invalid routine icon status=%d body=%s", invalidRoutine.Code, invalidRoutine.Body.String())
	}
	invalidUpdate := fixture.requestJSON(http.MethodPatch, "/api/v1/admin/devices/"+device.ID, updateAdminDeviceRequest{
		Name: "PC", AvatarKey: "not-an-avatar",
	}, true)
	if invalidUpdate.Code != http.StatusBadRequest {
		t.Fatalf("invalid avatar update status=%d body=%s", invalidUpdate.Code, invalidUpdate.Body.String())
	}
}

func TestWebBonusAddsCreditToDayActiveAtDelivery(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()

	serverNow := time.Date(2026, time.August, 14, 1, 5, 0, 0, time.UTC)
	fixture.app.now = func() time.Time { return serverNow }
	fixture.login(t)
	device, err := fixture.store.CreateDevice(context.Background(), "Brazil timezone", serverNow)
	if err != nil {
		t.Fatal(err)
	}
	response := fixture.requestJSON(http.MethodPost, "/api/v1/admin/devices/"+device.ID+"/bonus",
		addAdminBonusRequest{Minutes: 30}, true)
	if response.Code != http.StatusAccepted {
		t.Fatalf("bonus status=%d body=%s", response.Code, response.Body.String())
	}
	var queued adminBonusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &queued); err != nil || queued.OperationID == "" {
		t.Fatalf("bonus confirmation=%+v err=%v", queued, err)
	}
	operationPath := "/api/v1/admin/devices/" + device.ID + "/commands/" + queued.OperationID
	pendingOperation := fixture.requestJSON(http.MethodGet, operationPath, nil, false)
	if pendingOperation.Code != http.StatusOK || !strings.Contains(pendingOperation.Body.String(), `"acknowledged":false`) {
		t.Fatalf("pending operation status=%d body=%s", pendingOperation.Code, pendingOperation.Body.String())
	}
	beforeDelivery, err := fixture.store.LoadDailySummary(context.Background(), device.ID, "2026-08-14")
	if err != nil {
		t.Fatal(err)
	}
	if beforeDelivery.BonusSeconds != 0 {
		t.Fatalf("queued bonus was attached to the server date: %+v", beforeDelivery)
	}
	_, _, pendingStatus, err := fixture.app.loadDeviceLiveStatus(context.Background(), device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pendingStatus.BonusSeconds != 0 || pendingStatus.RemainingSeconds != 0 {
		t.Fatalf("queued credit was presented as already applied: %+v", pendingStatus)
	}
	heartbeat, err := fixture.store.ReceiveHeartbeat(context.Background(), device.ID, protocol.HeartbeatRequest{
		PolicyRevision: 1, LocalDate: "2026-08-13",
	}, serverNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(heartbeat.Commands) != 1 || heartbeat.Commands[0].Kind != "add_bonus" {
		t.Fatalf("bonus command not delivered: %+v", heartbeat.Commands)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(heartbeat.Commands[0].Payload, &payload); err != nil || payload["local_date"] != nil {
		t.Fatalf("credit increment command carried a date: payload=%+v err=%v", payload, err)
	}
	localDay, err := fixture.store.LoadDailySummary(context.Background(), device.ID, "2026-08-13")
	if err != nil {
		t.Fatal(err)
	}
	utcDay, err := fixture.store.LoadDailySummary(context.Background(), device.ID, "2026-08-14")
	if err != nil {
		t.Fatal(err)
	}
	if localDay.BonusSeconds != 30*60 || utcDay.BonusSeconds != 0 {
		t.Fatalf("bonus not applied to current daily credit: local=%+v UTC=%+v", localDay, utcDay)
	}
	_, _, deliveredStatus, err := fixture.app.loadDeviceLiveStatus(context.Background(), device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deliveredStatus.BonusSeconds != 0 || deliveredStatus.RemainingSeconds != 0 {
		t.Fatalf("unacknowledged credit was presented as available: %+v", deliveredStatus)
	}
	if _, err := fixture.store.ReceiveHeartbeat(context.Background(), device.ID, protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision + 1, LocalDate: "2026-08-13", CommandAcks: []string{queued.OperationID},
	}, serverNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	acknowledgedOperation := fixture.requestJSON(http.MethodGet, operationPath, nil, false)
	if acknowledgedOperation.Code != http.StatusOK || !strings.Contains(acknowledgedOperation.Body.String(), `"acknowledged":true`) {
		t.Fatalf("acknowledged operation status=%d body=%s", acknowledgedOperation.Code, acknowledgedOperation.Body.String())
	}
	_, _, synchronizedStatus, err := fixture.app.loadDeviceLiveStatus(context.Background(), device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if synchronizedStatus.BonusSeconds != 30*60 || synchronizedStatus.RemainingSeconds != 30*60 {
		t.Fatalf("acknowledged credit not presented as available: %+v", synchronizedStatus)
	}
}

func TestLegacyAgentCannotConsumeNewBonusCommand(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	device, err := fixture.store.CreateDevice(context.Background(), "Legacy agent", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := fixture.store.IssueDeviceToken(context.Background(), device.ID, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.QueueRemoteBonus(context.Background(), device.ID, 30*60, fixture.now); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(protocol.HeartbeatRequest{PolicyRevision: device.PolicyRevision, LocalDate: "2026-08-13"})
	request := httptest.NewRequest(http.MethodPost, protocol.HeartbeatPath, bytes.NewReader(payload))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Tempo-Device-ID", device.ID)
	response := httptest.NewRecorder()
	fixture.app.ServeHTTP(response, request)
	if response.Code != http.StatusUpgradeRequired || !strings.Contains(response.Body.String(), "agent_upgrade_required") {
		t.Fatalf("legacy heartbeat status=%d body=%s", response.Code, response.Body.String())
	}
	summary, err := fixture.store.LoadDailySummary(context.Background(), device.ID, "2026-08-13")
	if err != nil || summary.BonusSeconds != 0 {
		t.Fatalf("legacy agent materialized bonus: summary=%+v err=%v", summary, err)
	}
}

func TestExpiredAdministrativeSessionReturnsUnauthorized(t *testing.T) {
	fixture := newWebFixture(t, false, time.Minute)
	defer fixture.store.Close()
	fixture.login(t)
	fixture.app.now = func() time.Time { return fixture.now.Add(2 * time.Minute) }
	response := fixture.requestJSON(http.MethodGet, "/api/v1/admin/devices", nil, false)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBackendIsAPIOnlyAndHealthEndpointWorks(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	rootResponse := httptest.NewRecorder()
	fixture.app.ServeHTTP(rootResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if rootResponse.Code != http.StatusNotFound || strings.Contains(rootResponse.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("backend unexpectedly served frontend: status=%d type=%q", rootResponse.Code, rootResponse.Header().Get("Content-Type"))
	}
	healthResponse := httptest.NewRecorder()
	fixture.app.ServeHTTP(healthResponse, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if healthResponse.Code != http.StatusOK || !strings.Contains(healthResponse.Body.String(), `"status":"ok"`) {
		t.Fatalf("health status=%d body=%s", healthResponse.Code, healthResponse.Body.String())
	}
}

func TestLiveStatusUsesControlledComputerLocalDateWhileOnline(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	device, err := fixture.store.CreateDevice(context.Background(), "Different timezone", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	var quotas [7]int64
	quotas[time.Sunday] = 600
	quotas[time.Monday] = 3600
	if err := fixture.store.SaveQuotas(context.Background(), device.ID, quotas, 5, fixture.now); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ReceiveHeartbeat(context.Background(), device.ID, protocol.HeartbeatRequest{
		PolicyRevision: 2, LocalDate: "2026-08-09", SecondsUsed: 1,
	}, fixture.now); err != nil {
		t.Fatal(err)
	}

	_, _, status, err := fixture.app.loadDeviceLiveStatus(context.Background(), device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.LocalDate != "2026-08-09" || status.TodayQuotaSeconds != 600 || status.UsedSeconds != 1 {
		t.Fatalf("status did not use client date: %+v", status)
	}
	if status.GraphicalSessionActive || status.Counting {
		t.Fatalf("heartbeat without a graphical session was treated as active: %+v", status)
	}
	if _, err := fixture.store.ReceiveHeartbeat(context.Background(), device.ID, protocol.HeartbeatRequest{
		PolicyRevision: 2, LocalDate: "2026-08-09", SecondsUsed: 1,
		GraphicalSessionActive: true, GraphicalSessionID: "session-9",
		RequestSessionState: true,
	}, fixture.now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _, status, err = fixture.app.loadDeviceLiveStatus(context.Background(), device.ID)
	if err != nil || !status.GraphicalSessionActive || !status.Counting {
		t.Fatalf("active graphical session not reflected in live status: %+v err=%v", status, err)
	}
}

func TestLiveStatusDistinguishesRequestedFromConfirmedBlock(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	ctx := context.Background()
	device, err := fixture.store.CreateDevice(ctx, "Zorin", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.QueueControl(ctx, device.ID, "block_now", fixture.now); err != nil {
		t.Fatal(err)
	}
	response, err := fixture.store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: 1, LocalDate: "2026-08-10", GraphicalSessionActive: true,
		GraphicalSessionID: "session-9",
	}, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	_, _, requested, err := fixture.app.loadDeviceLiveStatus(ctx, device.ID)
	if err != nil || requested.ControlStatus != "block_requested" || requested.ActualState != "unblocked" {
		t.Fatalf("requested status=%+v err=%v", requested, err)
	}
	if len(response.Commands) != 1 {
		t.Fatalf("block command was not offered: %+v", response.Commands)
	}
	if _, err := fixture.store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: 1, ControlRevision: response.Control.Revision, LocalDate: "2026-08-10",
		GraphicalSessionActive: true, GraphicalSessionLocked: true, GraphicalSessionID: "session-9",
		CommandAcks: []string{response.Commands[0].ID},
	}, fixture.now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _, confirmed, err := fixture.app.loadDeviceLiveStatus(ctx, device.ID)
	if err != nil || confirmed.ControlStatus != "blocked" || confirmed.ActualState != "blocked" {
		t.Fatalf("confirmed status=%+v err=%v", confirmed, err)
	}
}

func TestLiveStatusNamesReverseControlTransitions(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	ctx := context.Background()
	device, err := fixture.store.CreateDevice(ctx, "Zorin", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: 1, LocalDate: "2026-08-10", GraphicalSessionActive: true,
		GraphicalSessionID: "session-9",
	}, fixture.now); err != nil {
		t.Fatal(err)
	}
	blockID, err := fixture.store.QueueControlOperation(ctx, device.ID, "block_now", fixture.now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	control, err := fixture.store.LoadControl(ctx, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: 1, ControlRevision: control.Revision, LocalDate: "2026-08-10",
		GraphicalSessionActive: true, GraphicalSessionLocked: true, GraphicalSessionID: "session-9",
		CommandAcks: []string{blockID},
	}, fixture.now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}

	clearID, err := fixture.store.QueueControlOperation(ctx, device.ID, "clear_manual_block", fixture.now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	_, _, unlocking, err := fixture.app.loadDeviceLiveStatus(ctx, device.ID)
	if err != nil || unlocking.ControlStatus != "unblock_requested" || unlocking.ActualState != "blocked" {
		t.Fatalf("unlocking status=%+v err=%v", unlocking, err)
	}
	control, err = fixture.store.LoadControl(ctx, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: 1, ControlRevision: control.Revision, LocalDate: "2026-08-10",
		GraphicalSessionActive: true, GraphicalSessionID: "session-9", CommandAcks: []string{clearID},
	}, fixture.now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _, active, err := fixture.app.loadDeviceLiveStatus(ctx, device.ID)
	if err != nil || active.ControlStatus != "active" || active.ActualState != "unblocked" {
		t.Fatalf("active status after unlock=%+v err=%v", active, err)
	}

	pauseID, err := fixture.store.QueueControlOperation(ctx, device.ID, "pause_monitoring", fixture.now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	_, _, pausing, err := fixture.app.loadDeviceLiveStatus(ctx, device.ID)
	if err != nil || pausing.ControlStatus != "pause_requested" {
		t.Fatalf("pausing status=%+v err=%v", pausing, err)
	}
	control, err = fixture.store.LoadControl(ctx, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: 1, ControlRevision: control.Revision, LocalDate: "2026-08-10",
		GraphicalSessionActive: true, GraphicalSessionID: "session-9", CommandAcks: []string{pauseID},
	}, fixture.now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	resumeID, err := fixture.store.QueueControlOperation(ctx, device.ID, "resume_monitoring", fixture.now.Add(7*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	_, _, resuming, err := fixture.app.loadDeviceLiveStatus(ctx, device.ID)
	if err != nil || resuming.ControlStatus != "resume_requested" {
		t.Fatalf("resuming status=%+v err=%v", resuming, err)
	}
	control, err = fixture.store.LoadControl(ctx, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ReceiveHeartbeat(ctx, device.ID, protocol.HeartbeatRequest{
		PolicyRevision: 1, ControlRevision: control.Revision, LocalDate: "2026-08-10",
		GraphicalSessionActive: true, GraphicalSessionID: "session-9", CommandAcks: []string{resumeID},
	}, fixture.now.Add(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _, active, err = fixture.app.loadDeviceLiveStatus(ctx, device.ID)
	if err != nil || active.ControlStatus != "active" {
		t.Fatalf("active status after resume=%+v err=%v", active, err)
	}
}

func TestHeartbeatRequiresDeviceCredential(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	device, err := fixture.store.CreateDevice(context.Background(), "Zorin", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := fixture.store.IssueDeviceToken(context.Background(), device.ID, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(protocol.HeartbeatRequest{LocalDate: fixture.now.Format("2006-01-02")})
	request := httptest.NewRequest(http.MethodPost, protocol.HeartbeatPath, bytes.NewReader(payload))
	request.Header.Set(deviceIDHeader, device.ID)
	request.Header.Set("Authorization", "Bearer wrong")
	response := httptest.NewRecorder()
	fixture.app.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, protocol.HeartbeatPath, bytes.NewReader(payload))
	request.Header.Set(deviceIDHeader, device.ID)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	fixture.app.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid token status=%d body=%s", response.Code, response.Body.String())
	}
	logs, err := fixture.store.ListCommunicationLogs(context.Background(), device.ID, 0, 10)
	if err != nil || len(logs) != 3 || logs[0].Source != "api" || logs[0].Target != "agent" ||
		logs[1].Source != "agent" || logs[1].Result != "success" || logs[2].Result != "warning" {
		t.Fatalf("heartbeat communication logs=%+v err=%v", logs, err)
	}
	for _, event := range logs {
		encoded, _ := json.Marshal(event)
		if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), "wrong") {
			t.Fatalf("communication log exposed credential: %s", encoded)
		}
	}
}

func TestHeartbeatIntervalIsSentOnlyToCapableAgentsAndChangesGlobally(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	device, err := fixture.store.CreateDevice(context.Background(), "Interval negotiation", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := fixture.store.IssueDeviceToken(context.Background(), device.ID, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision,
		LocalDate:      fixture.now.Format("2006-01-02"),
	})
	request := func(capabilities string) *httptest.ResponseRecorder {
		heartbeatRequest := httptest.NewRequest(http.MethodPost, protocol.HeartbeatPath, bytes.NewReader(payload))
		heartbeatRequest.Header.Set(deviceIDHeader, device.ID)
		heartbeatRequest.Header.Set("Authorization", "Bearer "+token)
		heartbeatRequest.Header.Set(protocol.VersionHeader, protocol.CurrentProtocolVersion)
		if capabilities != "" {
			heartbeatRequest.Header.Set(protocol.CapabilitiesHeader, capabilities)
		}
		response := httptest.NewRecorder()
		fixture.app.ServeHTTP(response, heartbeatRequest)
		if response.Code != http.StatusOK {
			t.Fatalf("heartbeat status=%d body=%s", response.Code, response.Body.String())
		}
		return response
	}

	legacyResponse := request("")
	var legacyPayload map[string]json.RawMessage
	decodeResponse(t, legacyResponse, &legacyPayload)
	if _, exists := legacyPayload["next_heartbeat_seconds"]; exists {
		t.Fatal("server sent the new field to an agent that did not advertise support")
	}

	fixture.app.heartbeatInterval = 9 * time.Second
	capableResponse := request("another-capability, " + protocol.NextHeartbeatCapability)
	var heartbeatResponse protocol.HeartbeatResponse
	decodeResponse(t, capableResponse, &heartbeatResponse)
	if heartbeatResponse.NextHeartbeatSeconds != 9 {
		t.Fatalf("next heartbeat seconds=%d, want updated global value", heartbeatResponse.NextHeartbeatSeconds)
	}

	operationID, err := fixture.store.QueueControlOperation(context.Background(), device.ID, "block_now", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ = json.Marshal(protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision, LocalDate: fixture.now.Format("2006-01-02"),
		CommandAcks: []string{operationID},
	})
	legacyAckResponse := request("")
	var legacyAckPayload map[string]json.RawMessage
	decodeResponse(t, legacyAckResponse, &legacyAckPayload)
	if _, exists := legacyAckPayload["acknowledged_commands"]; exists {
		t.Fatal("server sent command acknowledgement receipts to an incapable agent")
	}
	capableAckResponse := request(protocol.CommandAckReceiptCapability)
	var capableAckPayload map[string]json.RawMessage
	decodeResponse(t, capableAckResponse, &capableAckPayload)
	if _, exists := capableAckPayload["acknowledged_commands"]; !exists {
		t.Fatal("server omitted command acknowledgement receipts for a capable agent")
	}
}

func TestRejectedHeartbeatExplainsTheFailureInHumanLanguage(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	device, err := fixture.store.CreateDevice(context.Background(), "Zorin", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := fixture.store.IssueDeviceToken(context.Background(), device.ID, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision,
		LocalDate:      fixture.now.Format("2006-01-02"),
		Events: []protocol.PendingEvent{{
			UUID: "invalid-event", Kind: "unknown", Payload: json.RawMessage(`{}`), CreatedAt: fixture.now,
		}},
	})
	request := httptest.NewRequest(http.MethodPost, protocol.HeartbeatPath, bytes.NewReader(payload))
	request.Header.Set(deviceIDHeader, device.ID)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(protocol.VersionHeader, protocol.CurrentProtocolVersion)
	response := httptest.NewRecorder()
	fixture.app.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("rejected heartbeat status=%d body=%s", response.Code, response.Body.String())
	}
	logs, err := fixture.store.ListCommunicationLogs(context.Background(), device.ID, 0, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("rejected heartbeat logs=%+v err=%v", logs, err)
	}
	want := "O servidor não reconheceu uma atividade pendente enviada pelo computador."
	if logs[0].Summary != want || logs[0].Details["rejection_reason"] != want ||
		logs[0].Details["failure_stage"] != "processamento_no_servidor" {
		t.Fatalf("rejected heartbeat explanation=%+v", logs[0])
	}
}

func TestAdministrativeCommunicationLogsExposeOperatorRetentionAndCanBeDeleted(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	fixture.login(t)
	device, err := fixture.store.CreateDevice(context.Background(), "Zorin", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	devicePath := "/api/v1/admin/devices/" + device.ID
	if response := fixture.requestJSON(http.MethodGet, devicePath, nil, false); response.Code != http.StatusOK {
		t.Fatalf("device detail status=%d body=%s", response.Code, response.Body.String())
	}
	response := fixture.requestJSON(http.MethodGet, devicePath+"/communication", nil, false)
	if response.Code != http.StatusOK {
		t.Fatalf("communication status=%d body=%s", response.Code, response.Body.String())
	}
	var listing struct {
		Events        []serverstorage.CommunicationLog `json:"events"`
		RetentionDays int                              `json:"retention_days"`
	}
	decodeResponse(t, response, &listing)
	if len(listing.Events) != 0 || listing.RetentionDays != 30 {
		t.Fatalf("communication listing=%+v", listing)
	}
	response = fixture.requestJSON(http.MethodPut, devicePath+"/communication/settings", map[string]int{"retention_days": 7}, true)
	if response.Code != http.StatusNotFound {
		t.Fatalf("family changed operator retention status=%d body=%s", response.Code, response.Body.String())
	}
	response = fixture.requestJSON(http.MethodDelete, devicePath+"/communication", nil, true)
	if response.Code != http.StatusOK {
		t.Fatalf("delete logs status=%d body=%s", response.Code, response.Body.String())
	}
	response = fixture.requestJSON(http.MethodGet, devicePath+"/communication", nil, false)
	decodeResponse(t, response, &listing)
	if len(listing.Events) != 0 || listing.RetentionDays != 30 {
		t.Fatalf("communication after delete=%+v", listing)
	}
}

func TestCommunicationLogIncludesBusinessDetails(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	fixture.login(t)
	device, err := fixture.store.CreateDevice(context.Background(), "Zorin", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	devicePath := "/api/v1/admin/devices/" + device.ID
	bonusResponse := fixture.requestJSON(http.MethodPost, devicePath+"/bonus", addAdminBonusRequest{Minutes: 30}, true)
	if bonusResponse.Code != http.StatusAccepted {
		t.Fatalf("bonus status=%d body=%s", bonusResponse.Code, bonusResponse.Body.String())
	}
	commandResponse := fixture.requestJSON(http.MethodPost, devicePath+"/commands", queueAdminCommandRequest{Command: "pause_monitoring"}, true)
	if commandResponse.Code != http.StatusAccepted {
		t.Fatalf("command status=%d body=%s", commandResponse.Code, commandResponse.Body.String())
	}
	listingResponse := fixture.requestJSON(http.MethodGet, devicePath+"/communication", nil, false)
	if listingResponse.Code != http.StatusOK {
		t.Fatalf("communication status=%d body=%s", listingResponse.Code, listingResponse.Body.String())
	}
	var listing struct {
		Events []serverstorage.CommunicationLog `json:"events"`
	}
	decodeResponse(t, listingResponse, &listing)
	var bonus, command *serverstorage.CommunicationLog
	for i := range listing.Events {
		event := &listing.Events[i]
		switch event.Operation {
		case "POST bonus":
			bonus = event
		case "POST commands":
			command = event
		}
	}
	if bonus == nil || bonus.Details["bonus_minutes"] != "30" {
		t.Fatalf("bonus log=%+v", bonus)
	}
	if command == nil || command.Details["command"] != "pause_monitoring" {
		t.Fatalf("command log=%+v", command)
	}
}

func TestAdministrativeActivitiesTellTheCommandLifecycle(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	fixture.login(t)
	device, err := fixture.store.CreateDevice(context.Background(), "Zorin", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	devicePath := "/api/v1/admin/devices/" + device.ID
	bonusResponse := fixture.requestJSON(http.MethodPost, devicePath+"/bonus", addAdminBonusRequest{Minutes: 15}, true)
	if bonusResponse.Code != http.StatusAccepted {
		t.Fatalf("bonus status=%d body=%s", bonusResponse.Code, bonusResponse.Body.String())
	}
	var queued adminBonusResponse
	decodeResponse(t, bonusResponse, &queued)

	loadActivity := func() serverstorage.DeviceActivity {
		response := fixture.requestJSON(http.MethodGet, devicePath+"/activities/"+queued.OperationID, nil, false)
		if response.Code != http.StatusOK {
			t.Fatalf("activity status=%d body=%s", response.Code, response.Body.String())
		}
		var activity serverstorage.DeviceActivity
		decodeResponse(t, response, &activity)
		return activity
	}
	step := func(activity serverstorage.DeviceActivity, kind string) *serverstorage.ActivityStep {
		for index := range activity.Steps {
			if activity.Steps[index].Kind == kind {
				return &activity.Steps[index]
			}
		}
		return nil
	}

	waiting := loadActivity()
	if waiting.Status != "waiting_device" || step(waiting, "offered") != nil || waiting.Details["minutes"] != "15" {
		t.Fatalf("waiting activity=%+v", waiting)
	}
	first, err := fixture.store.ReceiveHeartbeat(context.Background(), device.ID, protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision, LocalDate: "2026-08-10",
	}, fixture.now.Add(time.Second))
	if err != nil || len(first.Commands) != 1 {
		t.Fatalf("first heartbeat=%+v err=%v", first, err)
	}
	offered := loadActivity()
	if offered.Status != "offered" || step(offered, "offered") == nil || step(offered, "offered").Occurrences != 1 {
		t.Fatalf("offered activity=%+v", offered)
	}
	if _, err := fixture.store.ReceiveHeartbeat(context.Background(), device.ID, protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision + 1, LocalDate: "2026-08-10", CommandAcks: []string{queued.OperationID},
	}, fixture.now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	completed := loadActivity()
	if completed.Status != "completed" || completed.CompletedAt == nil || step(completed, "completed") == nil || step(completed, "offered").Occurrences != 1 {
		t.Fatalf("completed activity=%+v", completed)
	}

	listingResponse := fixture.requestJSON(http.MethodGet, devicePath+"/activities?limit=10", nil, false)
	var listing struct {
		Activities []serverstorage.DeviceActivity `json:"activities"`
	}
	decodeResponse(t, listingResponse, &listing)
	listedBonus := false
	for _, activity := range listing.Activities {
		listedBonus = listedBonus || activity.ID == queued.OperationID
	}
	if listingResponse.Code != http.StatusOK || !listedBonus {
		t.Fatalf("activity listing status=%d value=%+v", listingResponse.Code, listing)
	}

	pendingResponse := fixture.requestJSON(http.MethodPost, devicePath+"/commands", queueAdminCommandRequest{Command: "pause_monitoring"}, true)
	if pendingResponse.Code != http.StatusAccepted {
		t.Fatalf("pending command status=%d body=%s", pendingResponse.Code, pendingResponse.Body.String())
	}
	var pending struct {
		OperationID string `json:"operation_id"`
	}
	decodeResponse(t, pendingResponse, &pending)

	withoutCSRF := fixture.requestJSON(http.MethodDelete, devicePath+"/activities/completed", nil, false)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("completed cleanup without CSRF status=%d", withoutCSRF.Code)
	}
	cleanupResponse := fixture.requestJSON(http.MethodDelete, devicePath+"/activities/completed", nil, true)
	var cleanup struct {
		Deleted int64 `json:"deleted"`
	}
	decodeResponse(t, cleanupResponse, &cleanup)
	if cleanupResponse.Code != http.StatusOK || cleanup.Deleted != 2 {
		t.Fatalf("completed cleanup status=%d value=%+v", cleanupResponse.Code, cleanup)
	}
	if response := fixture.requestJSON(http.MethodGet, devicePath+"/activities/"+queued.OperationID, nil, false); response.Code != http.StatusNotFound {
		t.Fatalf("completed activity remained after cleanup: status=%d", response.Code)
	}
	if acknowledged, err := fixture.store.RemoteBonusAcknowledged(context.Background(), device.ID, queued.OperationID); err != nil || !acknowledged {
		t.Fatalf("history cleanup changed the real bonus command: acknowledged=%t err=%v", acknowledged, err)
	}
	if response := fixture.requestJSON(http.MethodGet, devicePath+"/activities/"+pending.OperationID, nil, false); response.Code != http.StatusOK {
		t.Fatalf("pending activity removed by cleanup: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLocalAgentBonusAppearsInAdministrativeActivities(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	fixture.login(t)
	device, err := fixture.store.CreateDevice(context.Background(), "Zorin", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(protocol.BonusPayload{
		LocalDate: "2026-08-10", Seconds: 30 * 60, Origin: "local",
	})
	if _, err := fixture.store.ReceiveHeartbeat(context.Background(), device.ID, protocol.HeartbeatRequest{
		PolicyRevision: device.PolicyRevision,
		LocalDate:      "2026-08-10",
		Events: []protocol.PendingEvent{{
			UUID: "local-bonus-web-30m", Kind: "bonus_added", Payload: payload, CreatedAt: fixture.now,
		}},
	}, fixture.now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	response := fixture.requestJSON(http.MethodGet,
		"/api/v1/admin/devices/"+device.ID+"/activities?limit=10", nil, false)
	var listing struct {
		Activities []serverstorage.DeviceActivity `json:"activities"`
	}
	decodeResponse(t, response, &listing)
	if response.Code != http.StatusOK {
		t.Fatalf("activities status=%d value=%+v", response.Code, listing)
	}
	var activity serverstorage.DeviceActivity
	for _, candidate := range listing.Activities {
		if candidate.ID == "local-bonus-web-30m" {
			activity = candidate
			break
		}
	}
	if activity.ID != "local-bonus-web-30m" || activity.Origin != "device" ||
		activity.Status != "completed" || activity.Details["minutes"] != "30" {
		t.Fatalf("local activity=%+v", activity)
	}
}

func TestAdministrativeRoutesIsolateFamiliesWithoutSideEffects(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	fixture.login(t)
	ctx := context.Background()

	firstOwner, err := fixture.store.AdminByLogin(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	firstDevice, err := fixture.store.CreateDeviceForFamily(ctx, firstOwner.FamilyID, "Da primeira família", "cat", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	secondOwner, err := fixture.store.CreateFamilyOwner(ctx, "Família Dois", "owner@example.com", "hash", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	secondDevice, err := fixture.store.CreateDeviceForFamily(ctx, secondOwner.FamilyID, "Da segunda família", "dog", fixture.now)
	if err != nil {
		t.Fatal(err)
	}

	listingResponse := fixture.requestJSON(http.MethodGet, "/api/v1/admin/devices", nil, false)
	var listing struct {
		Devices []adminDeviceResponse `json:"devices"`
	}
	decodeResponse(t, listingResponse, &listing)
	if listingResponse.Code != http.StatusOK || len(listing.Devices) != 1 || listing.Devices[0].ID != firstDevice.ID {
		t.Fatalf("scoped listing status=%d devices=%+v", listingResponse.Code, listing.Devices)
	}

	beforeAudit, err := fixture.store.ListAudit(ctx, secondDevice.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	devicePath := "/api/v1/admin/devices/" + secondDevice.ID
	crossFamilyRequests := []struct {
		name, method, path string
		payload            interface{}
	}{
		{"detail", http.MethodGet, devicePath, nil},
		{"status", http.MethodGet, devicePath + "/status", nil},
		{"stream", http.MethodGet, devicePath + "/stream", nil},
		{"policy", http.MethodPut, devicePath + "/policy", updateAdminPolicyRequest{}},
		{"routine", http.MethodPost, devicePath + "/routines", saveAdminRoutineRequest{}},
		{"password", http.MethodPut, devicePath + "/password", updateAdminPasswordRequest{}},
		{"token", http.MethodPost, devicePath + "/token", nil},
		{"bonus", http.MethodPost, devicePath + "/bonus", addAdminBonusRequest{Minutes: 30}},
		{"command", http.MethodPost, devicePath + "/commands", queueAdminCommandRequest{Command: "pause_monitoring"}},
		{"command status", http.MethodGet, devicePath + "/commands/unknown", nil},
		{"audit", http.MethodGet, devicePath + "/events", nil},
		{"activities", http.MethodGet, devicePath + "/activities", nil},
		{"communication", http.MethodGet, devicePath + "/communication", nil},
		{"identity", http.MethodPatch, devicePath, updateAdminDeviceRequest{Name: "Nome invadido", AvatarKey: "dog"}},
	}
	for _, request := range crossFamilyRequests {
		response := fixture.requestJSON(request.method, request.path, request.payload, true)
		if response.Code != http.StatusNotFound {
			t.Fatalf("cross-family %s status=%d body=%s", request.name, response.Code, response.Body.String())
		}
	}
	if fixture.app.hub.hasSubscribers(secondDevice.ID) {
		t.Fatal("cross-family stream subscribed to target device")
	}

	storedDevice, _, err := fixture.store.LoadDevice(ctx, secondDevice.ID)
	if err != nil || storedDevice.Name != "Da segunda família" || storedDevice.AvatarKey != "dog" {
		t.Fatalf("cross-family mutation changed target=%+v err=%v", storedDevice, err)
	}
	afterAudit, err := fixture.store.ListAudit(ctx, secondDevice.ID, 100)
	if err != nil || len(afterAudit) != len(beforeAudit) {
		t.Fatalf("cross-family audit before=%d after=%d err=%v", len(beforeAudit), len(afterAudit), err)
	}
	logs, err := fixture.store.ListCommunicationLogs(ctx, secondDevice.ID, 0, 100)
	if err != nil || len(logs) != 0 {
		t.Fatalf("cross-family communication logs=%+v err=%v", logs, err)
	}
}

func TestAdministrativeSessionIsRevalidatedAgainstDurableIdentity(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	fixture.login(t)
	admin, err := fixture.store.AdminByLogin(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	token := fixture.sessionCookie.Value
	if _, changed, err := fixture.store.SetFamilyState(
		context.Background(), admin.FamilyID, "suspended", fixture.now.Add(time.Minute),
	); err != nil || !changed {
		t.Fatalf("suspend family changed=%t err=%v", changed, err)
	}
	response := fixture.requestJSON(http.MethodGet, "/api/v1/admin/devices", nil, false)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("stale session status=%d body=%s", response.Code, response.Body.String())
	}
	if _, ok := fixture.app.sessions.get(token, fixture.now); ok {
		t.Fatal("suspended family session remained in memory")
	}
	if _, changed, err := fixture.store.SetFamilyState(
		context.Background(), admin.FamilyID, "active", fixture.now.Add(2*time.Minute),
	); err != nil || !changed {
		t.Fatalf("reactivate family changed=%t err=%v", changed, err)
	}
	response = fixture.requestJSON(http.MethodGet, "/api/v1/admin/devices", nil, false)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("old session revived after reactivation status=%d", response.Code)
	}
}

func TestSuspendedFamilyHeartbeatIsForbiddenWithoutLeakingCredentials(t *testing.T) {
	fixture := newWebFixture(t, false, time.Hour)
	defer fixture.store.Close()
	ctx := context.Background()
	device, err := fixture.store.CreateDevice(ctx, "Suspenso", fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := fixture.store.IssueDeviceToken(ctx, device.ID, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := fixture.store.AdminByLogin(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err := fixture.store.SetFamilyState(ctx, admin.FamilyID, "suspended", fixture.now.Add(time.Minute)); err != nil || !changed {
		t.Fatalf("suspend family changed=%t err=%v", changed, err)
	}
	payload, _ := json.Marshal(protocol.HeartbeatRequest{LocalDate: "2026-08-10"})
	requestHeartbeat := func(credential string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, protocol.HeartbeatPath, bytes.NewReader(payload))
		request.Header.Set(deviceIDHeader, device.ID)
		request.Header.Set("Authorization", "Bearer "+credential)
		request.Header.Set(protocol.VersionHeader, protocol.CurrentProtocolVersion)
		response := httptest.NewRecorder()
		fixture.app.ServeHTTP(response, request)
		return response
	}
	wrong := requestHeartbeat(strings.Repeat("x", 43))
	if wrong.Code != http.StatusUnauthorized || strings.Contains(wrong.Body.String(), "suspended") {
		t.Fatalf("invalid credential leaked family state: status=%d body=%s", wrong.Code, wrong.Body.String())
	}
	response := requestHeartbeat(token)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"family_suspended"`) {
		t.Fatalf("suspended heartbeat status=%d body=%s", response.Code, response.Body.String())
	}
}

type testContext interface {
	Helper()
	TempDir() string
	Fatal(...interface{})
}

type recordingAccountMailer struct {
	messages []AccountMessage
}

func (*recordingAccountMailer) Available() bool { return true }

func (m *recordingAccountMailer) SendAccountMessage(_ context.Context, message AccountMessage) error {
	m.messages = append(m.messages, message)
	return nil
}

func accountTokenFromMessage(t *testing.T, message AccountMessage) string {
	t.Helper()
	for _, field := range strings.Fields(message.Text) {
		if !strings.HasPrefix(field, "https://") && !strings.HasPrefix(field, "http://") {
			continue
		}
		link, err := url.Parse(field)
		if err == nil && link.Query().Get("token") != "" {
			return link.Query().Get("token")
		}
	}
	t.Fatalf("account token not found in message %+v", message)
	return ""
}

func newWebFixture(t testContext, secureCookies bool, sessionLifetime time.Duration) *webFixture {
	t.Helper()
	ctx := context.Background()
	store, err := serverstorage.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	parameters := localauth.Argon2Params{
		Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16,
	}
	passwordHash, err := localauth.HashPassword("secret", parameters)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.Local)
	if _, err := store.BootstrapAdmin(ctx, "admin", passwordHash, now); err != nil {
		t.Fatal(err)
	}
	application, err := New(store, secureCookies, sessionLifetime, 60*time.Second, 3*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	application.now = func() time.Time { return now }
	return &webFixture{app: application, store: store, now: now}
}

func (f *webFixture) login(t *testing.T) {
	f.loginAs(t, "admin", "secret")
}

func (f *webFixture) loginAs(t *testing.T, login, password string) {
	t.Helper()
	bootstrapResponse := f.requestJSON(http.MethodGet, "/api/v1/admin/session", nil, false)
	var anonymousSession adminSessionResponse
	decodeResponse(t, bootstrapResponse, &anonymousSession)
	loginCSRFTokenCookie := findCookie(t, bootstrapResponse.Result().Cookies(), loginCSRFCookie)

	loginBody, _ := json.Marshal(adminLoginRequest{
		Login: login, Password: password, CSRFToken: anonymousSession.CSRFToken,
	})
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/session", bytes.NewReader(loginBody))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.AddCookie(loginCSRFTokenCookie)
	loginResponse := httptest.NewRecorder()
	f.app.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	f.sessionCookie = findCookie(t, loginResponse.Result().Cookies(), sessionCookieName)
	var authenticatedSession adminSessionResponse
	decodeResponse(t, loginResponse, &authenticatedSession)
	f.csrfToken = authenticatedSession.CSRFToken
}

func (f *webFixture) requestJSON(method, path string, payload interface{}, includeCSRF bool) *httptest.ResponseRecorder {
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		encoded, _ := json.Marshal(payload)
		body = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, body)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if includeCSRF {
		request.Header.Set(csrfHeaderName, f.csrfToken)
	}
	if f.sessionCookie != nil {
		request.AddCookie(f.sessionCookie)
	}
	response := httptest.NewRecorder()
	f.app.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, destination interface{}) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response status=%d body=%s err=%v", response.Code, response.Body.String(), err)
	}
}

func findCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.MaxAge >= 0 {
			return cookie
		}
	}
	t.Fatalf("cookie %s not found in %+v", name, cookies)
	return nil
}
