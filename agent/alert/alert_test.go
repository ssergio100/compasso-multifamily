package alert

import (
	"testing"
	"time"

	"github.com/ssergio100/compasso/agent/policy"
)

func TestTrackerReturnsCrossedRoutineThresholds(t *testing.T) {
	blockAt := time.Date(2026, 8, 8, 22, 0, 0, 0, time.Local)
	decision := policy.Decision{
		Allowed: true, Remaining: time.Hour,
		NextBlockAt: blockAt, NextBlockReason: policy.ReasonRoutine,
	}
	cycle := Cycle{PolicyRevision: 1, SessionID: "session-3", LocalDate: "2026-08-08"}
	var tracker Tracker
	if alerts, err := tracker.Due(decision, 10, cycle, blockAt.Add(-10*time.Minute-time.Second), true); err != nil || len(alerts) != 0 {
		t.Fatalf("initial alerts=%+v err=%v", alerts, err)
	}
	alerts, err := tracker.Due(decision, 10, cycle, blockAt.Add(-time.Minute), true)
	if err != nil || len(alerts) != 1 {
		t.Fatalf("routine alerts=%+v err=%v", alerts, err)
	}
	if alerts[0].Kind != AlertOneMinute {
		t.Fatalf("most urgent alert was not selected: %+v", alerts)
	}
	if repeated, err := tracker.Due(decision, 10, cycle, blockAt.Add(-time.Minute+time.Second), true); err != nil || len(repeated) != 0 {
		t.Fatalf("consumed thresholds were repeated: %+v err=%v", repeated, err)
	}
}

func TestTrackerDoesNotRepeatThresholdWhenPredictionMoves(t *testing.T) {
	cycle := Cycle{PolicyRevision: 1, SessionID: "session-3", LocalDate: "2026-08-08"}
	var tracker Tracker
	firstBlock := time.Date(2026, 8, 8, 14, 2, 0, 0, time.Local)
	decision := policy.Decision{
		Allowed: true, Remaining: 61 * time.Second,
		NextBlockAt: firstBlock, NextBlockReason: policy.ReasonQuota,
	}
	if alerts, err := tracker.Due(decision, 1, cycle, firstBlock.Add(-time.Minute-time.Second), true); err != nil || len(alerts) != 0 {
		t.Fatalf("initial alerts=%+v err=%v", alerts, err)
	}
	decision.Remaining = time.Minute
	alerts, err := tracker.Due(decision, 1, cycle, firstBlock.Add(-time.Minute), true)
	if err != nil || len(alerts) != 1 || alerts[0].Kind != AlertOneMinute {
		t.Fatalf("first threshold alerts=%+v err=%v", alerts, err)
	}
	decision.Remaining = 59 * time.Second
	decision.NextBlockAt = firstBlock.Add(time.Second)
	alerts, err = tracker.Due(decision, 1, cycle, firstBlock.Add(-time.Minute+time.Second), true)
	if err != nil || len(alerts) != 0 {
		t.Fatalf("moving prediction repeated threshold: %+v err=%v", alerts, err)
	}
}

func TestTrackerConsumesLockedAlertsWithoutBacklog(t *testing.T) {
	blockAt := time.Date(2026, 8, 8, 22, 0, 0, 0, time.Local)
	decision := policy.Decision{
		Allowed: true, Remaining: 11 * time.Minute,
		NextBlockAt: blockAt, NextBlockReason: policy.ReasonQuota,
	}
	cycle := Cycle{PolicyRevision: 1, SessionID: "session-3", LocalDate: "2026-08-08"}
	var tracker Tracker
	_, _ = tracker.Due(decision, 10, cycle, blockAt.Add(-10*time.Minute-time.Second), true)
	decision.Remaining = 10 * time.Minute
	if alerts, err := tracker.Due(decision, 10, cycle, blockAt.Add(-10*time.Minute), false); err != nil || len(alerts) != 0 {
		t.Fatalf("locked alerts=%+v err=%v", alerts, err)
	}
	decision.Remaining = 10*time.Minute - time.Second
	if alerts, err := tracker.Due(decision, 10, cycle, blockAt.Add(-10*time.Minute+time.Second), true); err != nil || len(alerts) != 0 {
		t.Fatalf("unlock replayed alert=%+v err=%v", alerts, err)
	}
	decision.Remaining = 5 * time.Minute
	alerts, err := tracker.Due(decision, 10, cycle, blockAt.Add(-5*time.Minute), true)
	if err != nil || len(alerts) != 1 || alerts[0].Kind != AlertFiveMinute {
		t.Fatalf("future alert after unlock=%+v err=%v", alerts, err)
	}
}

func TestTrackerStartsNewCycleAfterBalanceIncrease(t *testing.T) {
	cycle := Cycle{PolicyRevision: 1, SessionID: "session-3", LocalDate: "2026-08-08"}
	var tracker Tracker
	start := time.Date(2026, 8, 8, 14, 0, 0, 0, time.Local)
	decision := policy.Decision{
		Allowed: true, Remaining: 61 * time.Second,
		NextBlockAt: start.Add(61 * time.Second), NextBlockReason: policy.ReasonQuota,
	}
	_, _ = tracker.Due(decision, 1, cycle, start, true)
	decision.Remaining = time.Minute
	decision.NextBlockAt = start.Add(61 * time.Second)
	alerts, _ := tracker.Due(decision, 1, cycle, start.Add(time.Second), true)
	if len(alerts) != 1 {
		t.Fatalf("first cycle alerts=%+v", alerts)
	}
	decision.Remaining = 2 * time.Minute
	decision.NextBlockAt = start.Add(2*time.Minute + 2*time.Second)
	alerts, _ = tracker.Due(decision, 1, cycle, start.Add(2*time.Second), true)
	if len(alerts) != 0 {
		t.Fatalf("balance increase emitted immediate alert=%+v", alerts)
	}
	decision.Remaining = time.Minute
	alerts, _ = tracker.Due(decision, 1, cycle, start.Add(62*time.Second), true)
	if len(alerts) != 1 || alerts[0].Kind != AlertOneMinute {
		t.Fatalf("new balance cycle alerts=%+v", alerts)
	}
}

func TestTrackerDoesNotRearmAfterSmallBalanceCorrection(t *testing.T) {
	cycle := Cycle{PolicyRevision: 1, SessionID: "session-3", LocalDate: "2026-08-08"}
	var tracker Tracker
	start := time.Date(2026, 8, 8, 14, 0, 0, 0, time.Local)
	decision := policy.Decision{
		Allowed: true, Remaining: 61 * time.Second,
		NextBlockAt: start.Add(61 * time.Second), NextBlockReason: policy.ReasonQuota,
	}
	_, _ = tracker.Due(decision, 1, cycle, start, true)
	decision.Remaining = time.Minute
	alerts, _ := tracker.Due(decision, 1, cycle, start.Add(time.Second), true)
	if len(alerts) != 1 {
		t.Fatalf("initial alert=%+v", alerts)
	}
	decision.Remaining += time.Second
	decision.NextBlockAt = decision.NextBlockAt.Add(2 * time.Second)
	if alerts, err := tracker.Due(decision, 1, cycle, start.Add(2*time.Second), true); err != nil || len(alerts) != 0 {
		t.Fatalf("small correction rearmed alert: %+v err=%v", alerts, err)
	}
}

func TestTrackerRejectsInvalidInputs(t *testing.T) {
	decision := policy.Decision{Allowed: true, NextBlockAt: time.Now().Add(time.Hour)}
	var tracker Tracker
	if _, err := tracker.Due(decision, -1, Cycle{}, time.Now(), true); err == nil {
		t.Fatal("negative warning unexpectedly accepted")
	}
	if _, err := tracker.Due(decision, 10, Cycle{}, time.Time{}, true); err == nil {
		t.Fatal("zero current time unexpectedly accepted")
	}
}

func TestTenMinuteWarningDoesNotFireWithThirtyOneMinutesRemaining(t *testing.T) {
	start := time.Date(2026, 8, 13, 23, 0, 0, 0, time.Local)
	blockAt := start.Add(31 * time.Minute)
	cycle := Cycle{PolicyRevision: 2, SessionID: "session", LocalDate: "2026-08-13"}
	decision := policy.Decision{
		Allowed: true, Remaining: 31 * time.Minute,
		NextBlockAt: blockAt, NextBlockReason: policy.ReasonQuota,
	}
	var tracker Tracker
	if alerts, err := tracker.Due(decision, 10, cycle, start, true); err != nil || len(alerts) != 0 {
		t.Fatalf("initial alerts=%+v err=%v", alerts, err)
	}
	decision.Remaining = 11 * time.Minute
	if alerts, err := tracker.Due(decision, 10, cycle, start.Add(20*time.Minute), true); err != nil || len(alerts) != 0 {
		t.Fatalf("early alerts=%+v err=%v", alerts, err)
	}
	decision.Remaining = 10 * time.Minute
	alerts, err := tracker.Due(decision, 10, cycle, start.Add(21*time.Minute), true)
	if err != nil || len(alerts) != 1 || alerts[0].Kind != AlertPrimary {
		t.Fatalf("ten-minute alerts=%+v err=%v", alerts, err)
	}
}

func TestPlannedAlertsExplainEveryScheduledBlockReason(t *testing.T) {
	blockAt := time.Date(2026, 8, 28, 22, 0, 0, 0, time.Local)
	tests := []struct {
		name   string
		reason policy.Reason
		want   map[string][2]string
	}{
		{
			name:   "routine",
			reason: policy.ReasonRoutine,
			want: map[string][2]string{
				AlertPrimary:    {"Rotina programada em 10 minutos", "Uma rotina programada começará em 10 minutos. O computador será bloqueado."},
				AlertFiveMinute: {"Rotina programada em 5 minutos", "Uma rotina programada começará em 5 minutos. O computador será bloqueado."},
				AlertOneMinute:  {"Rotina programada em 1 minuto", "Uma rotina programada começará em 1 minuto. O computador será bloqueado."},
			},
		},
		{
			name:   "quota",
			reason: policy.ReasonQuota,
			want: map[string][2]string{
				AlertPrimary:    {"O tempo de hoje termina em 10 minutos", "O tempo disponível de hoje terminará em 10 minutos. O computador será bloqueado."},
				AlertFiveMinute: {"O tempo de hoje termina em 5 minutos", "O tempo disponível de hoje terminará em 5 minutos. O computador será bloqueado."},
				AlertOneMinute:  {"O tempo de hoje termina em 1 minuto", "O tempo disponível de hoje terminará em 1 minuto. O computador será bloqueado."},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			alerts := plannedAlerts(policy.Decision{
				Allowed: true, NextBlockAt: blockAt, NextBlockReason: test.reason,
			}, 10)
			if len(alerts) != len(test.want) {
				t.Fatalf("alerts=%+v", alerts)
			}
			for _, alert := range alerts {
				want, ok := test.want[alert.Kind]
				if !ok || alert.Title != want[0] || alert.Body != want[1] {
					t.Errorf("alert=%+v want=%+v", alert, want)
				}
			}
		})
	}
}

func TestManualBlockCopyNeverExposesTechnicalReason(t *testing.T) {
	if title := titleForReason(policy.ReasonManualBlock, 1); title != "Bloqueio solicitado pelo responsável" {
		t.Fatalf("title=%q", title)
	}
	if body := bodyForReason(policy.ReasonManualBlock, 1); body != "O responsável solicitou o bloqueio deste computador." {
		t.Fatalf("body=%q", body)
	}
}
