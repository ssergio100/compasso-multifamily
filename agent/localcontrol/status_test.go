package localcontrol

import (
	"strings"
	"testing"
)

type fakeSynchronization struct {
	checked bool
	online  bool
	detail  string
}

func (f fakeSynchronization) SynchronizationStatus() (bool, bool) {
	return f.checked, f.online
}

func (f fakeSynchronization) SynchronizationReport() (bool, bool, string) {
	return f.checked, f.online, f.detail
}

func TestSynchronizationReportReturnsStableLocalStates(t *testing.T) {
	state, detail := SynchronizationReport(fakeSynchronization{})
	if state != "checking" || !strings.Contains(detail, "primeira resposta") {
		t.Fatalf("checking report state=%q detail=%q", state, detail)
	}
	state, detail = SynchronizationReport(fakeSynchronization{checked: true, online: true})
	if state != "online" || detail != "" {
		t.Fatalf("online report state=%q detail=%q", state, detail)
	}
	state, detail = SynchronizationReport(fakeSynchronization{checked: true, detail: "heartbeat returned HTTP 401 secret=hidden"})
	if state != "offline" || !strings.Contains(detail, "identificação") || strings.Contains(detail, "hidden") {
		t.Fatalf("offline report state=%q detail=%q", state, detail)
	}
}

func TestHumanSynchronizationDetailCoversActionableFailures(t *testing.T) {
	tests := []struct {
		detail string
		want   string
	}{
		{"heartbeat returned HTTP 400", "erro 400"},
		{"heartbeat returned HTTP 426", "atualizado"},
		{"heartbeat returned HTTP 502", "temporariamente"},
		{"dial tcp: lookup api.test: no such host", "localizar"},
		{"context deadline exceeded", "demorou"},
		{"unexpected local error", "configurações"},
	}
	for _, test := range tests {
		if got := HumanSynchronizationDetail(test.detail); !strings.Contains(got, test.want) {
			t.Errorf("detail %q mapped to %q, want %q", test.detail, got, test.want)
		}
	}
}
