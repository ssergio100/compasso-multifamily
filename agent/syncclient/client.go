// Package syncclient exchanges durable agent state with tempo-server.
package syncclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ssergio100/compasso/agent/storage"
	protocol "github.com/ssergio100/compasso/protocol/v1"
)

type Config struct {
	ServerURL         string
	DeviceID          string
	DeviceToken       string
	InstallationID    string
	HeartbeatInterval time.Duration
	AttemptTimeout    time.Duration
}

const (
	DefaultHeartbeatInterval   = 3 * time.Second
	MinimumHeartbeatInterval   = time.Second
	MaximumHeartbeatInterval   = 10 * time.Minute
	repeatedFailureLogInterval = time.Minute
	maximumTransientBackoff    = 5 * time.Minute
	permanentFailureDelay      = 6 * time.Hour
	incompatibleFailureDelay   = time.Hour
)

type heartbeatRetryKind int

const (
	retryTransient heartbeatRetryKind = iota
	retryRateLimited
	retryPermanent
	retryIncompatible
)

type remoteHeartbeatError struct {
	message    string
	retryKind  heartbeatRetryKind
	retryAfter time.Duration
}

func (e *remoteHeartbeatError) Error() string { return e.message }

type heartbeatError struct {
	stage string
	err   error
}

func (e *heartbeatError) Error() string { return e.err.Error() }
func (e *heartbeatError) Unwrap() error { return e.err }

func heartbeatErrorStage(err error) string {
	var failure *heartbeatError
	if errors.As(err, &failure) {
		return failure.stage
	}
	return "unknown"
}

type Client struct {
	store                  *storage.Store
	http                   *http.Client
	config                 Config
	now                    func() time.Time
	wait                   func(context.Context, time.Duration) bool
	jitter                 func(time.Duration) time.Duration
	graphicalSessionMu     sync.RWMutex
	graphicalSessionActive bool
	graphicalSessionID     string
	graphicalSessionLocked bool
	controlMu              sync.RWMutex
	controlOnline          bool
	controlChecked         bool
	controlPaused          bool
	controlBlocked         bool
	controlRevision        int64
	synchronizationDetail  string
	statusReporter         func(error)
}

func (c *Client) RemoteControl() (online, paused, blocked bool, revision int64) {
	c.controlMu.RLock()
	defer c.controlMu.RUnlock()
	return c.controlOnline, c.controlPaused, c.controlBlocked, c.controlRevision
}

// SynchronizationStatus reports whether a heartbeat attempt completed and
// whether the latest attempt succeeded.
func (c *Client) SynchronizationStatus() (checked, online bool) {
	c.controlMu.RLock()
	defer c.controlMu.RUnlock()
	return c.controlChecked, c.controlOnline
}

// SynchronizationReport adds the last sanitized failure to the live state so
// the local interface can explain why the server is unavailable.
func (c *Client) SynchronizationReport() (checked, online bool, detail string) {
	c.controlMu.RLock()
	defer c.controlMu.RUnlock()
	return c.controlChecked, c.controlOnline, c.synchronizationDetail
}

func (c *Client) setRemoteControl(online, paused, blocked bool, revision int64) {
	c.controlMu.Lock()
	c.controlChecked = true
	c.controlOnline, c.controlPaused, c.controlBlocked = online, paused, blocked
	c.controlRevision = revision
	c.controlMu.Unlock()
}

// SetStatusReporter registers a process-local observer for synchronization
// state transitions. Reports contain sanitized errors and never credentials.
func (c *Client) SetStatusReporter(reporter func(error)) {
	c.statusReporter = reporter
}

func (c *Client) reportStatus(err error) {
	c.controlMu.Lock()
	if err == nil {
		c.synchronizationDetail = ""
	} else {
		c.synchronizationDetail = err.Error()
	}
	c.controlMu.Unlock()
	if c.statusReporter != nil {
		c.statusReporter(err)
	}
}

func New(store *storage.Store, httpClient *http.Client, config Config) (*Client, error) {
	if store == nil || httpClient == nil {
		return nil, errors.New("store and HTTP client are required")
	}
	if config.ServerURL == "" || config.DeviceID == "" || config.DeviceToken == "" || !protocol.ValidInstallationID(config.InstallationID) ||
		config.AttemptTimeout <= 0 {
		return nil, errors.New("complete synchronization configuration is required")
	}
	if config.HeartbeatInterval < MinimumHeartbeatInterval || config.HeartbeatInterval > MaximumHeartbeatInterval {
		return nil, errors.New("heartbeat fallback must be between 1 second and 10 minutes")
	}
	config.ServerURL = strings.TrimRight(config.ServerURL, "/")
	return &Client{
		store: store, http: httpClient, config: config, now: time.Now,
		wait: waitForHeartbeat, jitter: jitteredHeartbeatDelay,
	}, nil
}

// SetGraphicalSession reports the established graphical session observed by
// logind. Heartbeat takes a consistent snapshot without depending on desktop
// environment variables.
func (c *Client) SetGraphicalSession(active bool, sessionID string, locked bool) {
	if !active {
		sessionID = ""
		locked = false
	}
	c.graphicalSessionMu.Lock()
	c.graphicalSessionActive = active
	c.graphicalSessionID = sessionID
	c.graphicalSessionLocked = locked
	c.graphicalSessionMu.Unlock()
}

func (c *Client) graphicalSession() (bool, string, bool) {
	c.graphicalSessionMu.RLock()
	defer c.graphicalSessionMu.RUnlock()
	return c.graphicalSessionActive, c.graphicalSessionID, c.graphicalSessionLocked
}

// Heartbeat performs one cycle. Durable events are removed only when their
// identifiers are explicitly acknowledged by the server.
func (c *Client) Heartbeat(ctx context.Context, now time.Time) (result protocol.HeartbeatResponse, err error) {
	stage := "local_state"
	defer func() {
		if err != nil {
			c.setRemoteControl(false, false, false, 0)
			var classified *heartbeatError
			if !errors.As(err, &classified) {
				err = &heartbeatError{stage: stage, err: err}
			}
		}
	}()
	if now.IsZero() {
		return protocol.HeartbeatResponse{}, errors.New("heartbeat time is required")
	}
	revision := int64(0)
	policy, err := c.store.LoadPolicy(ctx)
	if err == nil {
		revision = policy.Revision
	} else if !errors.Is(err, storage.ErrNoPolicy) {
		return protocol.HeartbeatResponse{}, err
	}
	localDate := now.Format("2006-01-02")
	graphicalSessionActive, graphicalSessionID, graphicalSessionLocked := c.graphicalSession()
	c.controlMu.RLock()
	controlRevision := c.controlRevision
	c.controlMu.RUnlock()
	confirmedState, hasConfirmedState := c.store.CurrentConfirmedSessionState()
	usage, err := c.store.LoadDailyUsage(ctx, localDate)
	if err != nil {
		return protocol.HeartbeatResponse{}, err
	}
	pending, err := c.store.PendingEvents(ctx, 100)
	if err != nil {
		return protocol.HeartbeatResponse{}, err
	}
	commandAcks, err := c.store.AppliedCommandIDs(ctx, 100)
	if err != nil {
		return protocol.HeartbeatResponse{}, err
	}
	request := protocol.HeartbeatRequest{
		PolicyRevision: revision, LocalDate: localDate, SecondsUsed: usage.SecondsUsed,
		GraphicalSessionActive: graphicalSessionActive,
		GraphicalSessionID:     graphicalSessionID,
		GraphicalSessionLocked: graphicalSessionLocked,
		ControlRevision:        controlRevision,
		CommandAcks:            commandAcks,
	}
	if hasConfirmedState {
		request.SessionStateRevision = confirmedState.Revision
	}
	request.RequestSessionState = graphicalSessionActive && (!hasConfirmedState ||
		confirmedState.SessionID != graphicalSessionID || confirmedState.LocalDate != localDate ||
		confirmedState.Revision < revision)
	for _, event := range pending {
		if !json.Valid([]byte(event.PayloadJSON)) {
			_ = c.store.IncrementEventRetries(ctx, []string{event.UUID})
			return protocol.HeartbeatResponse{}, fmt.Errorf("pending event %s contains invalid JSON", event.UUID)
		}
		request.Events = append(request.Events, protocol.PendingEvent{
			UUID: event.UUID, Kind: event.Kind, Payload: json.RawMessage(event.PayloadJSON), CreatedAt: event.CreatedAt,
		})
	}
	stage = "request"
	body, err := json.Marshal(request)
	if err != nil {
		return protocol.HeartbeatResponse{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.ServerURL+protocol.HeartbeatPath, bytes.NewReader(body))
	if err != nil {
		return protocol.HeartbeatResponse{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+c.config.DeviceToken)
	httpRequest.Header.Set("X-Tempo-Device-ID", c.config.DeviceID)
	httpRequest.Header.Set(protocol.InstallationIDHeader, c.config.InstallationID)
	httpRequest.Header.Set(protocol.VersionHeader, protocol.CurrentProtocolVersion)
	httpRequest.Header.Set(protocol.CapabilitiesHeader,
		protocol.NextHeartbeatCapability+", "+protocol.CommandAckReceiptCapability+", "+protocol.InstallationIdentityCapability)
	stage = "transport"
	response, err := c.http.Do(httpRequest)
	if err != nil {
		c.http.CloseIdleConnections()
		c.recordRetries(ctx, pending)
		return protocol.HeartbeatResponse{}, fmt.Errorf("send heartbeat: %s", redactSensitiveText(err.Error(), c.config.DeviceToken))
	}
	defer response.Body.Close()
	stage = "response"
	if response.StatusCode != http.StatusOK {
		failure := decodeHeartbeatError(response.StatusCode, response.Header.Get("Retry-After"), response.Body)
		c.recordRetries(ctx, pending)
		return protocol.HeartbeatResponse{}, failure
	}
	result, err = decodeHeartbeatResponse(response.Body)
	if err != nil {
		c.recordRetries(ctx, pending)
		return protocol.HeartbeatResponse{}, err
	}
	stage = "apply"
	c.setRemoteControl(true, result.Control.MonitoringPaused, result.Control.ManualBlock, result.Control.Revision)
	for _, eventID := range result.AcknowledgedEvents {
		if err := c.store.AcknowledgeEvent(ctx, eventID); err != nil {
			return protocol.HeartbeatResponse{}, err
		}
	}
	if err := c.store.ForgetAppliedCommandIDs(ctx, result.AcknowledgedCommands); err != nil {
		return protocol.HeartbeatResponse{}, fmt.Errorf("forget acknowledged commands: %w", err)
	}
	if result.Policy != nil {
		if err := c.store.ReplacePolicy(ctx, fromProtocolPolicy(*result.Policy)); err != nil {
			return protocol.HeartbeatResponse{}, fmt.Errorf("apply policy revision %d: %w", result.Policy.Revision, err)
		}
	}
	if request.RequestSessionState && result.SessionState == nil {
		return protocol.HeartbeatResponse{}, errors.New("server did not return the requested session state")
	}
	if result.Policy != nil && graphicalSessionActive &&
		result.Policy.Revision > request.SessionStateRevision && result.SessionState == nil {
		return protocol.HeartbeatResponse{}, errors.New("server changed policy without confirming the active session balance")
	}
	if result.SessionState != nil {
		if !graphicalSessionActive || result.SessionState.SessionID != graphicalSessionID ||
			result.SessionState.LocalDate != localDate || result.SessionState.Revision < 0 ||
			result.SessionState.RemainingSeconds < 0 || result.SessionState.UsageSeconds < 0 ||
			result.SessionState.ConfirmedAt.IsZero() {
			return protocol.HeartbeatResponse{}, errors.New("server returned an invalid session state")
		}
		if result.Policy != nil && result.SessionState.Revision < result.Policy.Revision {
			return protocol.HeartbeatResponse{}, errors.New("server returned a session state older than its policy")
		}
		if err := c.store.SaveConfirmedSessionState(ctx, storage.ConfirmedSessionState{
			Revision: result.SessionState.Revision, SessionID: result.SessionState.SessionID,
			LocalDate:        result.SessionState.LocalDate,
			RemainingSeconds: result.SessionState.RemainingSeconds,
			UsageSeconds:     result.SessionState.UsageSeconds,
			ConfirmedAt:      result.SessionState.ConfirmedAt,
		}); err != nil {
			return protocol.HeartbeatResponse{}, fmt.Errorf("store confirmed session state: %w", err)
		}
	}
	// Install the authoritative state before handling its commands. Bonus
	// commands become acknowledgeable now; control commands are only staged and
	// the daemon acknowledges them after observing the requested graphical
	// effect.
	for _, command := range result.Commands {
		if err := c.applyCommand(ctx, command, result.Control.Revision, request.LocalDate, now); err != nil {
			return protocol.HeartbeatResponse{}, fmt.Errorf("apply command %s: %w", command.ID, err)
		}
	}
	return result, nil
}

func decodeHeartbeatError(status int, retryAfterHeader string, responseBody io.Reader) error {
	var response protocol.ErrorResponse
	decoder := json.NewDecoder(io.LimitReader(responseBody, 4096))
	_ = decoder.Decode(&response)
	if status == http.StatusTooManyRequests {
		return &remoteHeartbeatError{
			message: "heartbeat rate limited by the server", retryKind: retryRateLimited,
			retryAfter: boundedRetryAfter(retryAfterHeader),
		}
	}
	if response.Code == "revision_ahead" {
		return &remoteHeartbeatError{
			message: fmt.Sprintf(
				"heartbeat rejected: local revision %d is newer than server revision %d; local state belongs to another enrollment or the server was restored from an older backup",
				response.ClientRevision, response.ServerRevision,
			), retryKind: retryPermanent,
		}
	}
	switch response.Code {
	case "invalid_device_credentials":
		return &remoteHeartbeatError{message: "device credentials were rejected; reconfigure the Compasso agent", retryKind: retryPermanent}
	case "family_suspended":
		return &remoteHeartbeatError{message: "the Compasso family is suspended; contact support", retryKind: retryPermanent}
	case "installation_conflict":
		return &remoteHeartbeatError{message: "another Compasso agent installation is linked to this device; reconfigure it or rotate the device token", retryKind: retryPermanent}
	case "agent_upgrade_required":
		return &remoteHeartbeatError{message: "heartbeat rejected; update the Compasso agent", retryKind: retryPermanent}
	}
	if status >= 500 {
		return &remoteHeartbeatError{message: fmt.Sprintf("heartbeat returned HTTP %d", status), retryKind: retryTransient}
	}
	if status == http.StatusConflict {
		return &remoteHeartbeatError{
			message:   "heartbeat rejected because local synchronization state is newer than the device on the server; local state may belong to another enrollment or the server may have been restored from an older backup",
			retryKind: retryIncompatible,
		}
	}
	if status >= 400 && status < 500 {
		return &remoteHeartbeatError{message: fmt.Sprintf("heartbeat returned incompatible HTTP %d", status), retryKind: retryIncompatible}
	}
	return fmt.Errorf("heartbeat returned HTTP %d", status)
}

func boundedRetryAfter(value string) time.Duration {
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 5 * time.Second
	}
	delay := time.Duration(seconds) * time.Second
	if delay < 5*time.Second {
		return 5 * time.Second
	}
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func decodeHeartbeatResponse(responseBody io.Reader) (protocol.HeartbeatResponse, error) {
	var heartbeatResponse protocol.HeartbeatResponse
	decoder := json.NewDecoder(io.LimitReader(responseBody, 512<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&heartbeatResponse); err != nil {
		return protocol.HeartbeatResponse{}, fmt.Errorf("decode heartbeat response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return protocol.HeartbeatResponse{}, errors.New("decode heartbeat response: trailing JSON data")
	}
	return heartbeatResponse, nil
}

func redactSensitiveText(message, secret string) string {
	if secret == "" {
		return message
	}
	return strings.ReplaceAll(message, secret, "[REDACTED]")
}

func (c *Client) applyCommand(ctx context.Context, command protocol.Command, controlRevision int64, localDate string, now time.Time) error {
	if command.ID == "" || command.CreatedAt.IsZero() {
		return errors.New("invalid command envelope")
	}
	switch command.Kind {
	case "pause_monitoring", "resume_monitoring", "block_now", "clear_manual_block":
		return c.store.StageControlEffect(ctx, command.ID, controlRevision, command.Kind, now)
	case "add_bonus":
		var payload protocol.CreditIncrementPayload
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			return errors.New("invalid bonus command payload")
		}
		if payload.Origin != "web" || payload.Seconds <= 0 {
			return errors.New("invalid bonus command payload")
		}
		if payload.UUID == "" {
			payload.UUID = command.ID
		}
		if payload.CreatedAt.IsZero() {
			payload.CreatedAt = command.CreatedAt
		}
		return c.store.ApplyRemoteBonus(ctx, command.ID, storage.Bonus{
			UUID: payload.UUID, LocalDate: localDate, Seconds: payload.Seconds,
			Origin: "web", CreatedAt: payload.CreatedAt,
		}, now)
	default:
		return fmt.Errorf("unsupported command kind %q", command.Kind)
	}
}

func (c *Client) recordRetries(ctx context.Context, events []storage.PendingEvent) {
	if len(events) == 0 {
		return
	}
	ids := make([]string, len(events))
	for index, event := range events {
		ids[index] = event.UUID
	}
	_ = c.store.IncrementEventRetries(ctx, ids)
}

func (c *Client) Run(ctx context.Context, logger *log.Logger) error {
	if logger == nil {
		logger = log.Default()
	}
	delay := time.Duration(0)
	normalInterval := c.config.HeartbeatInterval
	initialBackoff := initialHeartbeatBackoff(normalInterval)
	backoff := initialBackoff
	online := false
	first := true
	fastFollowUp := false
	attempts := 0
	var offlineSince time.Time
	var lastFailureLog time.Time
	for {
		if delay > 0 {
			if !c.wait(ctx, delay) {
				return nil
			}
		}
		attemptStarted := time.Now()
		attemptContext, cancelAttempt := context.WithTimeout(ctx, c.config.AttemptTimeout)
		result, err := c.Heartbeat(attemptContext, c.now())
		cancelAttempt()
		attemptFinished := time.Now()
		attemptDuration := attemptFinished.Sub(attemptStarted)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			attempts++
			if online || first {
				offlineSince = attemptFinished
				lastFailureLog = attemptFinished
				logger.Printf("synchronization offline stage=%s attempt=%d duration=%s: %v",
					heartbeatErrorStage(err), attempts, attemptDuration.Round(time.Millisecond), err)
				c.reportStatus(err)
			} else if attemptFinished.Sub(lastFailureLog) >= repeatedFailureLogInterval {
				lastFailureLog = attemptFinished
				logger.Printf("synchronization still offline stage=%s attempts=%d offline_for=%s last_attempt=%s: %v",
					heartbeatErrorStage(err), attempts, attemptFinished.Sub(offlineSince).Round(time.Second),
					attemptDuration.Round(time.Millisecond), err)
			}
			online, first = false, false
			fastFollowUp = false
			var remoteFailure *remoteHeartbeatError
			if errors.As(err, &remoteFailure) {
				switch remoteFailure.retryKind {
				case retryRateLimited:
					delay = c.jitter(remoteFailure.retryAfter)
				case retryPermanent:
					delay = c.jitter(permanentFailureDelay)
				case retryIncompatible:
					delay = c.jitter(incompatibleFailureDelay)
				default:
					delay = c.jitter(backoff)
				}
			} else {
				// Keep transient retry cadence measured from the start of the failed
				// attempt. A timeout must not add another full backoff.
				delay = c.jitter(backoff) - attemptDuration
				if delay < 0 {
					delay = 0
				}
			}
			if remoteFailure == nil || remoteFailure.retryKind == retryTransient {
				backoff *= 2
				if backoff > maximumTransientBackoff {
					backoff = maximumTransientBackoff
				}
			}
			continue
		}
		if !online {
			if attempts == 0 {
				logger.Printf("synchronization online")
			} else {
				logger.Printf("synchronization online attempts=%d offline_for=%s",
					attempts, attemptFinished.Sub(offlineSince).Round(time.Second))
			}
			c.reportStatus(nil)
		}
		online, first = true, false
		attempts = 0
		offlineSince = time.Time{}
		lastFailureLog = time.Time{}
		normalInterval = nextHeartbeatInterval(result.NextHeartbeatSeconds, c.config.HeartbeatInterval)
		initialBackoff = initialHeartbeatBackoff(normalInterval)
		backoff = initialBackoff
		if len(result.Commands) > 0 && !fastFollowUp {
			// Commands are durably recorded during Heartbeat. Report their IDs in
			// an immediate follow-up instead of waiting a full normal interval.
			// At most one accelerated request is allowed in sequence so a server
			// that repeats a command cannot cause a busy synchronization loop.
			delay = 0
			fastFollowUp = true
		} else {
			delay = c.jitter(normalInterval)
			fastFollowUp = false
		}
	}
}

func jitteredHeartbeatDelay(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	maximumJitter := base / 10
	if maximumJitter <= 0 {
		return base
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return base
	}
	additional := time.Duration(binary.LittleEndian.Uint64(random[:]) % uint64(maximumJitter+1))
	return base + additional
}

func waitForHeartbeat(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextHeartbeatInterval(serverSeconds int64, fallback time.Duration) time.Duration {
	if serverSeconds < int64(MinimumHeartbeatInterval/time.Second) ||
		serverSeconds > int64(MaximumHeartbeatInterval/time.Second) {
		return fallback
	}
	return time.Duration(serverSeconds) * time.Second
}

func initialHeartbeatBackoff(normalInterval time.Duration) time.Duration {
	if normalInterval < time.Second {
		return normalInterval
	}
	return time.Second
}

func fromProtocolPolicy(policy protocol.Policy) storage.PolicySnapshot {
	converted := storage.PolicySnapshot{
		Revision: policy.Revision, MonitoringPaused: policy.MonitoringPaused,
		ManualBlock: policy.ManualBlock, WarningMinutes: policy.WarningMinutes,
		LocalPasswordVerifier: policy.LocalPasswordVerifier, UpdatedAt: policy.UpdatedAt,
	}
	for day, seconds := range policy.WeeklyQuotaSeconds {
		converted.WeeklyQuota[day] = time.Duration(seconds) * time.Second
	}
	for _, routine := range policy.Routines {
		converted.Routines = append(converted.Routines, storage.StoredRoutine{
			ID: routine.ID, Name: routine.Name, Days: routine.Days,
			Start: time.Duration(routine.StartSecond) * time.Second,
			End:   time.Duration(routine.EndSecond) * time.Second, Enabled: routine.Enabled,
		})
	}
	return converted
}
