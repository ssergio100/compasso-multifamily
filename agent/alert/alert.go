package alert

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ssergio100/compasso/agent/policy"
)

const (
	AlertPrimary    = "primary"
	AlertFiveMinute = "five_minute"
	AlertOneMinute  = "one_minute"
	minimumRearm    = 30 * time.Second
)

// Alert represents a planned notification event before a known block.
type Alert struct {
	At     time.Time
	Kind   string
	Reason policy.Reason
	Title  string
	Body   string
}

type Notifier interface {
	Notify(context.Context, Alert) error
}

// Cycle identifies one authorization period for alert deduplication.
type Cycle struct {
	PolicyRevision int64
	SessionID      string
	LocalDate      string
}

// Tracker emits each threshold once per authorization period. Its zero value
// is ready for use and deliberately emits nothing on the first observation.
type Tracker struct {
	initialized     bool
	active          bool
	cycle           Cycle
	reason          policy.Reason
	previousCycle   time.Time
	previousBlockAt time.Time
	emitted         map[string]bool
}

// Due returns thresholds crossed since the previous observation. When deliver
// is false, crossed thresholds are consumed without being returned so a locked
// desktop cannot accumulate obsolete notifications.
func (tracker *Tracker) Due(decision policy.Decision, warningMinutes int, cycle Cycle, now time.Time, deliver bool) ([]Alert, error) {
	if warningMinutes < 0 {
		return nil, fmt.Errorf("warning minutes cannot be negative")
	}
	if now.IsZero() || (tracker.initialized && now.Before(tracker.previousCycle)) {
		return nil, fmt.Errorf("alert cycle times are invalid")
	}
	if !tracker.initialized {
		tracker.initialized = true
		tracker.observe(decision, cycle, now)
		return nil, nil
	}

	hasFutureBlock := decision.Allowed && !decision.NextBlockAt.IsZero()
	blockWasMeaningfullyPostponed := !tracker.previousBlockAt.IsZero() &&
		decision.NextBlockAt.Sub(tracker.previousBlockAt) >= minimumRearm
	newCycle := !tracker.active || tracker.cycle != cycle || tracker.reason != decision.NextBlockReason ||
		blockWasMeaningfullyPostponed
	if !hasFutureBlock {
		tracker.active = false
		tracker.previousCycle = now
		tracker.previousBlockAt = time.Time{}
		return nil, nil
	}
	if newCycle {
		tracker.emitted = make(map[string]bool)
		tracker.observe(decision, cycle, now)
		return nil, nil
	}

	var mostUrgent *Alert
	for _, plannedAlert := range plannedAlerts(decision, warningMinutes) {
		crossed := plannedAlert.At.After(tracker.previousCycle) && !plannedAlert.At.After(now)
		if crossed && !tracker.emitted[plannedAlert.Kind] {
			tracker.emitted[plannedAlert.Kind] = true
			if !deliver {
				continue
			}
			current := plannedAlert
			mostUrgent = &current
		}
	}
	tracker.observe(decision, cycle, now)
	if mostUrgent == nil {
		return nil, nil
	}
	return []Alert{*mostUrgent}, nil
}

func (tracker *Tracker) observe(decision policy.Decision, cycle Cycle, now time.Time) {
	tracker.active = decision.Allowed && !decision.NextBlockAt.IsZero()
	tracker.cycle = cycle
	tracker.reason = decision.NextBlockReason
	tracker.previousCycle = now
	tracker.previousBlockAt = decision.NextBlockAt
	if tracker.emitted == nil {
		tracker.emitted = make(map[string]bool)
	}
}

func plannedAlerts(decision policy.Decision, warningMinutes int) []Alert {
	if decision.NextBlockAt.IsZero() || !decision.Allowed {
		return nil
	}
	thresholds := map[time.Time]string{}
	addThreshold := func(alertTime time.Time, alertKind string) {
		if alertTime.Before(decision.NextBlockAt) {
			thresholds[alertTime] = alertKind
		}
	}
	addThreshold(decision.NextBlockAt.Add(-time.Duration(warningMinutes)*time.Minute), AlertPrimary)
	addThreshold(decision.NextBlockAt.Add(-5*time.Minute), AlertFiveMinute)
	addThreshold(decision.NextBlockAt.Add(-time.Minute), AlertOneMinute)

	alertTimes := make([]time.Time, 0, len(thresholds))
	for alertTime := range thresholds {
		alertTimes = append(alertTimes, alertTime)
	}
	sort.Slice(alertTimes, func(first, second int) bool { return alertTimes[first].Before(alertTimes[second]) })
	plannedAlerts := make([]Alert, 0, len(alertTimes))
	for _, alertTime := range alertTimes {
		alertKind := thresholds[alertTime]
		remainingMinutes := int(decision.NextBlockAt.Sub(alertTime).Minutes())
		plannedAlerts = append(plannedAlerts, Alert{
			At: alertTime, Kind: alertKind, Reason: decision.NextBlockReason,
			Title: titleForReason(decision.NextBlockReason, remainingMinutes),
			Body:  bodyForReason(decision.NextBlockReason, remainingMinutes),
		})
	}
	return plannedAlerts
}

func titleForReason(reason policy.Reason, remainingMinutes int) string {
	remainingText := remainingTimeText(remainingMinutes)
	switch reason {
	case policy.ReasonRoutine:
		return fmt.Sprintf("Rotina programada em %s", remainingText)
	case policy.ReasonQuota:
		return fmt.Sprintf("O tempo de hoje termina em %s", remainingText)
	case policy.ReasonManualBlock:
		return "Bloqueio solicitado pelo responsável"
	default:
		return fmt.Sprintf("Bloqueio programado em %s", remainingText)
	}
}

func bodyForReason(reason policy.Reason, remainingMinutes int) string {
	remainingText := remainingTimeText(remainingMinutes)
	switch reason {
	case policy.ReasonRoutine:
		return fmt.Sprintf("Uma rotina programada começará em %s. O computador será bloqueado.", remainingText)
	case policy.ReasonQuota:
		return fmt.Sprintf("O tempo disponível de hoje terminará em %s. O computador será bloqueado.", remainingText)
	case policy.ReasonManualBlock:
		return "O responsável solicitou o bloqueio deste computador."
	default:
		return fmt.Sprintf("Este computador será bloqueado em %s.", remainingText)
	}
}

func remainingTimeText(minutes int) string {
	if minutes == 1 {
		return "1 minuto"
	}
	return fmt.Sprintf("%d minutos", minutes)
}
