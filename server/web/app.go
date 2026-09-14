// Package web implements the Compasso HTTP API.
package web

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ssergio100/compasso/server/storage"
)

const (
	sessionCookieName = "tempo_admin_session"
	loginCSRFCookie   = "tempo_login_csrf"
)

type App struct {
	store             *storage.Store
	sessions          *sessionStore
	secureCookies     bool
	onlineTimeout     time.Duration
	heartbeatInterval time.Duration
	adminOrigin       string
	now               func() time.Time
	handler           http.Handler
	hub               *eventHub
	accountMailer     AccountMailer
	rateLimits        *rateLimiter
}

// New creates an API-only HTTP application. The backend does not read, render
// or serve frontend files.
func New(
	store *storage.Store,
	secureCookies bool,
	sessionLifetime time.Duration,
	onlineTimeout time.Duration,
	heartbeatInterval time.Duration,
	adminOrigin string,
	options ...Option,
) (*App, error) {
	if store == nil {
		return nil, errors.New("server store is required")
	}
	if sessionLifetime < time.Minute || sessionLifetime > 7*24*time.Hour {
		return nil, errors.New("session lifetime must be between 1 minute and 7 days")
	}
	if onlineTimeout <= 0 {
		return nil, errors.New("online timeout must be positive")
	}
	if heartbeatInterval < time.Second || heartbeatInterval > 10*time.Minute || heartbeatInterval%time.Second != 0 {
		return nil, errors.New("heartbeat interval must be a whole number of seconds between 1 second and 10 minutes")
	}
	app := &App{
		store: store, sessions: newSessionStore(sessionLifetime), secureCookies: secureCookies,
		now: time.Now, onlineTimeout: onlineTimeout, heartbeatInterval: heartbeatInterval, hub: newEventHub(),
		accountMailer: unavailableAccountMailer{}, rateLimits: newRateLimiter(),
	}
	if err := app.SetAdminOrigin(adminOrigin); err != nil {
		return nil, err
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("web option is required")
		}
		if err := option(app); err != nil {
			return nil, err
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", app.health)
	mux.HandleFunc("/api/v1/device/heartbeat", app.heartbeat)
	mux.HandleFunc("/api/v1/admin/session", app.adminSessionAPI)
	mux.HandleFunc("/api/v1/account/register", app.accountRegisterAPI)
	mux.HandleFunc("/api/v1/account/resend-confirmation", app.accountResendConfirmationAPI)
	mux.HandleFunc("/api/v1/account/confirm", app.accountConfirmAPI)
	mux.HandleFunc("/api/v1/account/password-reset", app.accountPasswordResetAPI)
	mux.HandleFunc("/api/v1/account/password-reset/confirm", app.accountPasswordResetConfirmAPI)
	mux.HandleFunc("/api/v1/account/email/confirm", app.accountEmailConfirmAPI)
	mux.HandleFunc("/api/v1/admin/account", app.adminAccountAPI)
	mux.HandleFunc("/api/v1/admin/account/password", app.adminAccountPasswordAPI)
	mux.HandleFunc("/api/v1/admin/account/email", app.adminAccountEmailAPI)
	mux.HandleFunc("/api/v1/admin/devices", app.adminDevicesAPI)
	mux.HandleFunc("/api/v1/admin/devices/", app.adminDeviceAPI)
	app.handler = app.corsHeaders(securityHeaders(app.logAdministrativeCommunication(mux), secureCookies))
	return app, nil
}

// SetAdminOrigin configures the only browser origin allowed to call the
// administrative API with credentials. Empty keeps tests and same-origin
// development possible.
func (a *App) SetAdminOrigin(origin string) error {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin != "" && origin != "same-host" && !strings.HasPrefix(origin, "http://") && !strings.HasPrefix(origin, "https://") {
		return fmt.Errorf("admin origin must be an absolute HTTP URL")
	}
	a.adminOrigin = origin
	return nil
}

func (a *App) allowedAdminOrigin(requestOrigin string, requestHost string) (string, bool) {
	if a.adminOrigin != "same-host" {
		return a.adminOrigin, a.adminOrigin != "" && constantEqual(requestOrigin, a.adminOrigin)
	}
	parsedOrigin, err := url.Parse(requestOrigin)
	if err != nil || (parsedOrigin.Scheme != "http" && parsedOrigin.Scheme != "https") || parsedOrigin.Hostname() == "" {
		return "", false
	}
	requestHostname := requestHost
	if hostname, _, splitError := net.SplitHostPort(requestHost); splitError == nil {
		requestHostname = hostname
	} else {
		requestHostname = strings.Trim(requestHost, "[]")
	}
	if !constantEqual(strings.ToLower(parsedOrigin.Hostname()), strings.ToLower(requestHostname)) {
		return "", false
	}
	return requestOrigin, true
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) authenticated(r *http.Request) (session, string, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return session{}, "", false
	}
	value, ok := a.sessions.get(cookie.Value, a.now())
	if !ok {
		return session{}, "", false
	}
	admin, err := a.store.AdminByID(r.Context(), value.AdminID)
	if err != nil || !admin.Active || (admin.Email != "" && !admin.EmailVerified) || admin.FamilyState != "active" ||
		admin.FamilyID != value.FamilyID || admin.AuthGeneration != value.AuthGeneration {
		a.sessions.delete(cookie.Value)
		return session{}, "", false
	}
	value.Login = admin.Login
	return value, cookie.Value, true
}

func (a *App) setCookie(w http.ResponseWriter, cookie *http.Cookie) {
	cookie.Path = "/api/v1/admin"
	cookie.Secure = a.secureCookies
	cookie.SameSite = http.SameSiteStrictMode
	http.SetCookie(w, cookie)
}

func (a *App) setLoginCSRFCookie(w http.ResponseWriter, cookie *http.Cookie) {
	cookie.Path = "/api/v1"
	cookie.Secure = a.secureCookies
	cookie.SameSite = http.SameSiteStrictMode
	http.SetCookie(w, cookie)
}

func constantEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func securityHeaders(next http.Handler, productionHTTPS bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if productionHTTPS {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
