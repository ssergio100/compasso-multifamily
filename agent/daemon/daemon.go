// Package daemon coordinates policy evaluation, accounting and enforcement.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/ssergio100/compasso/agent/alert"
	"github.com/ssergio100/compasso/agent/policy"
	"github.com/ssergio100/compasso/agent/session"
	"github.com/ssergio100/compasso/agent/storage"
)

const (
	localDateLayout          = "2006-01-02"
	sessionLockRetryInterval = 30 * time.Second
)

// Status describes one completed daemon cycle.
type Status struct {
	Decision                policy.Decision
	GraphicalSession        bool
	AwaitingSynchronization bool
	UsageSeconds            int64
	DueAlerts               []alert.Alert
}

// SynchronizationSource receives session presence for the next heartbeat.
type SynchronizationSource interface {
	SetGraphicalSession(active bool, sessionID string, locked bool)
	RemoteControl() (online, paused, blocked bool, revision int64)
}

// Daemon owns the runtime state that is intentionally not persisted between
// individual polling cycles. Durable policy and usage remain in Store.
type Daemon struct {
	store           *storage.Store
	sessions        session.Manager
	controlledUser  string
	checkpointEvery time.Duration

	tracker               *storage.UsageTracker
	trackerDate           string
	lastAt                time.Time
	lastShouldCount       bool
	lastHadActiveSession  bool
	lastCountUntil        time.Time
	fraction              time.Duration
	lockAttempts          map[string]time.Time
	synchronizationSource SynchronizationSource
	alertNotifier         alert.Notifier
	alertTracker          alert.Tracker
}

func (d *Daemon) SetAlertNotifier(notifier alert.Notifier) {
	d.alertNotifier = notifier
}

// SetSynchronizationSource enables server-confirmed balance authorization and
// graphical-session reporting.
func (d *Daemon) SetSynchronizationSource(source SynchronizationSource) {
	d.synchronizationSource = source
}

// New creates a daemon controller. Call Step periodically or Run for the
// production loop.
func New(store *storage.Store, sessions session.Manager, controlledUser string, checkpointEvery time.Duration) (*Daemon, error) {
	if store == nil || sessions == nil {
		return nil, errors.New("store and session manager are required")
	}
	if controlledUser == "" {
		return nil, errors.New("controlled user cannot be empty")
	}
	if checkpointEvery <= 0 {
		return nil, errors.New("checkpoint interval must be positive")
	}
	return &Daemon{
		store: store, sessions: sessions, controlledUser: controlledUser,
		checkpointEvery: checkpointEvery, lockAttempts: make(map[string]time.Time),
	}, nil
}

// Step performs one complete observation, accounting and enforcement cycle.
// now should contain Go's monotonic component in production.
func (d *Daemon) Step(ctx context.Context, now time.Time) (Status, error) {
	if now.IsZero() {
		return Status{}, errors.New("step time must be set")
	}
	if !d.lastAt.IsZero() && now.Before(d.lastAt) {
		return Status{}, errors.New("step time moved backwards")
	}

	snapshot, err := d.store.CurrentPolicy()
	if err != nil {
		return Status{}, err
	}
	remoteOnline := false
	remoteControlRevision := int64(0)
	if d.synchronizationSource != nil {
		remoteOnline, snapshot.MonitoringPaused, snapshot.ManualBlock, remoteControlRevision = d.synchronizationSource.RemoteControl()
		if !remoteOnline {
			snapshot.MonitoringPaused, snapshot.ManualBlock = false, false
		}
	}
	allSessions, err := d.sessions.Sessions(ctx, d.controlledUser)
	if err != nil {
		return Status{}, err
	}
	graphical := graphicalSessions(allSessions)
	activeGraphicalSessionID := establishedGraphicalSessionID(graphical)
	activeGraphicalSession := activeGraphicalSessionID != ""
	lockedSessions := make(map[string]bool)
	activeSessions := make(map[string]bool)
	for _, current := range graphical {
		if current.State != "active" {
			continue
		}
		key := current.BalanceAuthorizationID()
		activeSessions[key] = true
		lockedSessions[key], err = d.sessions.IsLocked(ctx, current)
		if err != nil {
			return Status{}, err
		}
	}
	for key := range d.lockAttempts {
		if !activeSessions[key] {
			delete(d.lockAttempts, key)
		}
	}
	activeGraphicalSessionLocked := false
	for _, current := range graphical {
		if current.State == "active" {
			activeGraphicalSessionLocked = lockedSessions[current.BalanceAuthorizationID()]
			break
		}
	}
	if d.synchronizationSource != nil {
		d.synchronizationSource.SetGraphicalSession(activeGraphicalSession, activeGraphicalSessionID, activeGraphicalSessionLocked)
	}
	pendingControl, hasPendingControl, err := d.store.PendingControlEffect(ctx)
	if err != nil {
		return Status{}, err
	}
	controlEffectCurrent := remoteOnline && hasPendingControl && pendingControl.Revision == remoteControlRevision
	if controlEffectCurrent {
		effectConfirmed := false
		switch pendingControl.Kind {
		case "block_now":
			effectConfirmed = activeGraphicalSessionID != "" && activeGraphicalSessionLocked
		case "clear_manual_block", "pause_monitoring":
			effectConfirmed = activeGraphicalSessionID == "" || !activeGraphicalSessionLocked
		case "resume_monitoring":
			effectConfirmed = true
		}
		if effectConfirmed {
			if err := d.store.CompleteControlEffect(ctx, pendingControl.CommandID, now); err != nil {
				return Status{}, err
			}
			controlEffectCurrent = false
		}
	}
	localDate := now.Format(localDateLayout)
	if d.tracker == nil {
		if err := d.prepareTracker(ctx, localDate, now); err != nil {
			return Status{}, err
		}
	} else if d.trackerDate != localDate {
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		if d.lastShouldCount && d.lastHadActiveSession {
			beforeMidnight := cappedElapsed(d.lastAt, midnight, d.lastCountUntil)
			if err := d.addElapsed(ctx, beforeMidnight, midnight); err != nil {
				return Status{}, fmt.Errorf("account usage before local midnight: %w", err)
			}
		}
		if err := d.prepareTracker(ctx, localDate, now); err != nil {
			return Status{}, err
		}
		newDayDecision, err := d.evaluate(ctx, snapshot, midnight, remoteOnline)
		if err != nil {
			return Status{}, err
		}
		if newDayDecision.ShouldCount && activeGraphicalSession {
			afterMidnight := cappedElapsed(midnight, now, newDayDecision.NextBlockAt)
			if err := d.addElapsed(ctx, afterMidnight, now); err != nil {
				return Status{}, fmt.Errorf("account usage after local midnight: %w", err)
			}
		}
	} else if !d.lastAt.IsZero() && d.lastShouldCount && d.lastHadActiveSession {
		elapsed := cappedElapsed(d.lastAt, now, d.lastCountUntil)
		if err := d.addElapsed(ctx, elapsed, now); err != nil {
			return Status{}, fmt.Errorf("account allowed usage: %w", err)
		}
	}

	confirmedState, hasConfirmedState := d.store.CurrentConfirmedSessionState()
	if hasConfirmedState && confirmedState.LocalDate == localDate && d.tracker.Seconds() < confirmedState.UsageSeconds {
		if err := d.tracker.EnsureAtLeast(ctx, confirmedState.UsageSeconds, now); err != nil {
			return Status{}, fmt.Errorf("reconcile server-confirmed usage: %w", err)
		}
	}
	awaitingSynchronization := remoteOnline && !snapshot.MonitoringPaused && !snapshot.ManualBlock && activeGraphicalSessionID != "" &&
		(!hasConfirmedState || confirmedState.SessionID != activeGraphicalSessionID ||
			confirmedState.LocalDate != localDate || confirmedState.Revision < snapshot.Revision)
	var decision policy.Decision
	if awaitingSynchronization {
		decision = policy.Decision{
			Allowed: true, Reason: policy.ReasonAwaitingSynchronization,
		}
	} else {
		decision, err = d.evaluate(ctx, snapshot, now, remoteOnline)
		if err != nil {
			return Status{}, err
		}
	}
	if !decision.Allowed && d.lastShouldCount {
		if err := d.tracker.Flush(ctx, now); err != nil {
			return Status{}, fmt.Errorf("persist usage before blocking login: %w", err)
		}
	}
	status := Status{
		Decision: decision, GraphicalSession: activeGraphicalSession,
		AwaitingSynchronization: awaitingSynchronization,
		UsageSeconds:            d.tracker.Seconds(),
	}
	status.DueAlerts, err = d.alertTracker.Due(decision, snapshot.WarningMinutes, alert.Cycle{
		PolicyRevision: snapshot.Revision,
		SessionID:      activeGraphicalSessionID,
		LocalDate:      localDate,
	}, now, activeGraphicalSessionID != "" && !activeGraphicalSessionLocked && !awaitingSynchronization)
	if err != nil {
		return Status{}, fmt.Errorf("calculate due alerts: %w", err)
	}
	// Advance runtime accounting state before enforcement. A transient lock
	// failure must not cause the same elapsed interval to be counted twice.
	d.lastAt = now
	d.lastShouldCount = decision.ShouldCount
	d.lastHadActiveSession = activeGraphicalSession
	d.lastCountUntil = decision.NextBlockAt
	if awaitingSynchronization {
		return status, nil
	}

	if decision.Allowed {
		unlockRequested := controlEffectCurrent &&
			(pendingControl.Kind == "clear_manual_block" || pendingControl.Kind == "pause_monitoring")
		if unlockRequested {
			for _, current := range graphical {
				if current.State != "active" || !lockedSessions[current.BalanceAuthorizationID()] {
					continue
				}
				if err := d.sessions.Unlock(ctx, current); err != nil {
					return status, err
				}
			}
		}
		for key := range d.lockAttempts {
			delete(d.lockAttempts, key)
		}
	} else {
		for _, current := range graphical {
			if current.State != "active" {
				continue
			}
			key := current.BalanceAuthorizationID()
			attemptedAt, attempted := d.lockAttempts[key]
			if attempted && now.Sub(attemptedAt) < sessionLockRetryInterval {
				continue
			}
			d.lockAttempts[key] = now
			if err := d.sessions.Lock(ctx, current); err != nil {
				return status, err
			}
		}
	}
	return status, nil
}

// Flush persists the final in-memory counter during an orderly shutdown.
func (d *Daemon) Flush(ctx context.Context, now time.Time) error {
	if d.tracker == nil {
		return nil
	}
	return d.tracker.Flush(ctx, now)
}

// Run executes Step immediately and then at every tick until cancellation.
// A missing initial policy is non-fatal: the daemon stays alive so a future
// synchronization phase can install one.
func (d *Daemon) Run(ctx context.Context, tick time.Duration, logger *log.Logger) error {
	if tick <= 0 {
		return errors.New("tick interval must be positive")
	}
	if logger == nil {
		logger = log.Default()
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	lastSummary := ""
	lastError := ""
	runStep := func(now time.Time) {
		status, err := d.Step(ctx, now)
		if err != nil {
			if err.Error() != lastError {
				logger.Printf("agent cycle failed: %v", err)
				lastError = err.Error()
			}
			return
		}
		if lastError != "" {
			logger.Printf("agent cycle recovered")
			lastError = ""
		}
		for _, dueAlert := range status.DueAlerts {
			if d.alertNotifier == nil {
				logger.Printf("desktop alert unavailable kind=%s", dueAlert.Kind)
				continue
			}
			if err := d.alertNotifier.Notify(ctx, dueAlert); err != nil {
				logger.Printf("desktop alert failed kind=%s: %v", dueAlert.Kind, err)
			}
		}
		summary := fmt.Sprintf("%s:%t:%t", status.Decision.Reason, status.GraphicalSession,
			status.AwaitingSynchronization)
		if summary != lastSummary {
			logger.Printf("decision=%s session=%t awaiting_synchronization=%t usage_seconds=%d remaining_seconds=%d",
				status.Decision.Reason, status.GraphicalSession, status.AwaitingSynchronization,
				status.UsageSeconds, int64(status.Decision.Remaining/time.Second))
			lastSummary = summary
		}
	}
	runStep(time.Now())
	for {
		select {
		case <-ctx.Done():
			flushContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := d.Flush(flushContext, time.Now())
			cancel()
			return err
		case now := <-ticker.C:
			runStep(now)
		}
	}
}

func (d *Daemon) prepareTracker(ctx context.Context, localDate string, now time.Time) error {
	if d.trackerDate == localDate {
		return nil
	}
	if d.tracker != nil {
		if err := d.tracker.Flush(ctx, now); err != nil {
			return fmt.Errorf("flush previous local date: %w", err)
		}
	}
	tracker, err := storage.NewUsageTracker(ctx, d.store, localDate, d.checkpointEvery)
	if err != nil {
		return fmt.Errorf("load usage for %s: %w", localDate, err)
	}
	d.tracker = tracker
	d.trackerDate = localDate
	d.fraction = 0
	return nil
}

func (d *Daemon) addElapsed(ctx context.Context, elapsed time.Duration, now time.Time) error {
	if elapsed <= 0 {
		return nil
	}
	total := d.fraction + elapsed
	whole := total.Truncate(time.Second)
	d.fraction = total - whole
	if whole == 0 {
		return nil
	}
	return d.tracker.Add(ctx, whole, now)
}

func (d *Daemon) evaluate(ctx context.Context, snapshot storage.PolicySnapshot, now time.Time, serverAuthority bool) (policy.Decision, error) {
	monitoring := policy.MonitoringActive
	if snapshot.MonitoringPaused {
		monitoring = policy.MonitoringPaused
	}
	input := policy.Input{
		Now: now, Monitoring: monitoring, ManualBlock: snapshot.ManualBlock,
	}
	if !serverAuthority {
		bonus, err := d.store.TotalBonusSeconds(ctx, d.trackerDate)
		if err != nil {
			return policy.Decision{}, fmt.Errorf("load daily bonus: %w", err)
		}
		input.Quota = policy.WeeklyQuota(snapshot.WeeklyQuota)
		input.Consumed = time.Duration(d.tracker.Seconds()) * time.Second
		input.Bonus = time.Duration(bonus) * time.Second
	} else {
		confirmedState, available := d.store.CurrentConfirmedSessionState()
		remainingSeconds := int64(0)
		if available && confirmedState.LocalDate == d.trackerDate {
			usageSinceConfirmation := d.tracker.Seconds() - confirmedState.UsageSeconds
			if usageSinceConfirmation < 0 {
				usageSinceConfirmation = 0
			}
			remainingSeconds = confirmedState.RemainingSeconds - usageSinceConfirmation
			if remainingSeconds < 0 {
				remainingSeconds = 0
			}
		}
		input.Quota[now.Weekday()] = time.Duration(remainingSeconds) * time.Second
	}
	for _, routine := range snapshot.Routines {
		if routine.Enabled {
			input.Routines = append(input.Routines, policy.Routine{
				Name: routine.Name, Days: routine.Days, Start: routine.Start, End: routine.End,
			})
		}
	}
	decision, err := policy.Evaluate(input)
	if err != nil {
		return policy.Decision{}, fmt.Errorf("evaluate policy: %w", err)
	}
	return decision, nil
}

func establishedGraphicalSessionID(sessions []session.Session) string {
	for _, current := range sessions {
		if current.State == "active" {
			return current.BalanceAuthorizationID()
		}
	}
	return ""
}

func graphicalSessions(sessions []session.Session) []session.Session {
	graphical := make([]session.Session, 0, len(sessions))
	for _, current := range sessions {
		if current.IsLocalGraphical() {
			graphical = append(graphical, current)
		}
	}
	return graphical
}

func cappedElapsed(start, end, countUntil time.Time) time.Duration {
	if !countUntil.IsZero() && countUntil.Before(end) {
		end = countUntil
	}
	if !end.After(start) {
		return 0
	}
	return end.Sub(start)
}
