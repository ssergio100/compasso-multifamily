package localmsg

import (
	"strings"
	"testing"
)

func TestSynchronizationDetail(t *testing.T) {
	cases := []struct{ detail, want string }{
		{"http 400", "versões incompatíveis"},
		{"http 401", "identificação deste computador"},
		{"http 403", "identificação deste computador"},
		{"local revision mismatch", "não corresponde ao cadastro"},
		{"http 409", "não corresponde ao cadastro"},
		{"update the compasso agent", "precisa ser atualizado"},
		{"http 426", "precisa ser atualizado"},
		{"http 502", "temporariamente indisponível"},
		{"http 503", "temporariamente indisponível"},
		{"http 504", "temporariamente indisponível"},
		{"dial tcp: lookup apifamily.smresume.com", "localizar o servidor"},
		{"server misbehaving", "localizar o servidor"},
		{"context deadline exceeded", "demorou demais"},
		{"i/o timeout", "demorou demais"},
		{"something entirely new", "A sincronização falhou"},
		{"", "A sincronização falhou"},
	}
	for _, testCase := range cases {
		got := SynchronizationDetail(testCase.detail)
		if !strings.Contains(got, testCase.want) {
			t.Errorf("SynchronizationDetail(%q) = %q, want it to contain %q", testCase.detail, got, testCase.want)
		}
	}
}

func TestReport(t *testing.T) {
	status, detail := Report(StatusChecking, "irrelevant failure")
	if status != StatusChecking || detail != WaitingForFirstResponse {
		t.Fatalf("Report(checking) = (%q, %q)", status, detail)
	}
	status, detail = Report(StatusOnline, "")
	if status != StatusOnline || detail != "" {
		t.Fatalf("Report(online) = (%q, %q)", status, detail)
	}
	status, detail = Report(StatusOffline, "http 503")
	if status != StatusOffline || !strings.Contains(detail, "temporariamente indisponível") {
		t.Fatalf("Report(offline) = (%q, %q)", status, detail)
	}
}

func TestBonusError(t *testing.T) {
	if got := BonusError(ErrorInvalidPassword); got != "Senha incorreta." {
		t.Fatalf("BonusError(invalid_password) = %q", got)
	}
	if got := BonusError("anything else"); got != ForBonusFailure {
		t.Fatalf("BonusError(unknown) = %q", got)
	}
}
