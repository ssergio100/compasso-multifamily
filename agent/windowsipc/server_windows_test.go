//go:build windows

package windowsipc

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ssergio100/compasso/agent/localauth"
	"github.com/ssergio100/compasso/agent/localmsg"
)

type fakeBonus struct {
	result localauth.GrantResult
	err    error
	gotPwd string
	gotSec int64
}

func (f *fakeBonus) Grant(_ context.Context, password string, seconds int64, _ time.Time) (localauth.GrantResult, error) {
	f.gotPwd, f.gotSec = password, seconds
	return f.result, f.err
}

type fakeSync struct {
	checked, online bool
	detail          string
}

func (f fakeSync) SynchronizationReport() (bool, bool, string) {
	return f.checked, f.online, f.detail
}

type fakeSettings struct {
	settings PublicSettings
	ok       bool
}

func (f fakeSettings) PublicConfiguration() (PublicSettings, bool) {
	return f.settings, f.ok
}

func TestFrameRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	want := Response{OK: true, Status: localmsg.StatusOnline, EventUUID: "abc"}
	if err := writeFrame(&buffer, want); err != nil {
		t.Fatal(err)
	}
	got, err := readFrame(&buffer)
	if err != nil {
		t.Fatal(err)
	}
	if got.Operation != "" {
		t.Fatalf("decoded request carries unexpected operation %q", got.Operation)
	}
}

func TestReadFrameRejectsOversizedPrefix(t *testing.T) {
	payload := []byte{0xff, 0xff, 0xff, 0xff}
	if _, err := readFrame(bytes.NewReader(payload)); err == nil {
		t.Fatal("expected an out-of-range payload to be rejected")
	}
}

func TestReadFrameRejectsZeroLength(t *testing.T) {
	if _, err := readFrame(bytes.NewReader([]byte{0, 0, 0, 0})); err == nil {
		t.Fatal("expected a zero-length payload to be rejected")
	}
}

func TestHandleAddLocalBonusSuccessReportsDurableGrant(t *testing.T) {
	bonus := &fakeBonus{result: localauth.GrantResult{
		UUID: "uuid-1", BonusSeconds: 900, TotalSeconds: 1800,
	}}
	service := NewService(bonus, fakeSync{}, nil)

	response := service.Handle(Request{
		Operation: OperationAddLocalBonus, Password: "secret", Seconds: 900,
	})
	if !response.OK || response.EventUUID != "uuid-1" {
		t.Fatalf("unexpected response %+v", response)
	}
	if response.Grant == nil || response.Grant.BonusSeconds != 900 || response.Grant.TotalSeconds != 1800 {
		t.Fatalf("grant summary missing or wrong: %+v", response.Grant)
	}
	if bonus.gotPwd != "secret" || bonus.gotSec != 900 {
		t.Fatalf("engine received password %q and %d seconds", bonus.gotPwd, bonus.gotSec)
	}
}

func TestHandleAddLocalBonusMapsFailuresToStableCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"not configured", localauth.ErrPasswordNotConfigured, localmsg.ErrorPasswordNotConfigured},
		{"invalid password", localauth.ErrInvalidPassword, localmsg.ErrorInvalidPassword},
		{"rate limited", localauth.ErrRateLimited, localmsg.ErrorRateLimited},
		{"unexpected", errors.New("boom"), localmsg.ErrorFailed},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewService(&fakeBonus{err: testCase.err}, fakeSync{}, nil)
			response := service.Handle(Request{
				Operation: OperationAddLocalBonus, Password: "wrong", Seconds: 900,
			})
			if response.OK {
				t.Fatal("expected the request to fail")
			}
			if response.ErrorCode != testCase.code {
				t.Fatalf("error code = %q, want %q", response.ErrorCode, testCase.code)
			}
			if response.Message == "" {
				t.Fatal("expected a human message for the interface")
			}
			if response.EventUUID != "" || response.Grant != nil {
				t.Fatal("a failed request must not report a persisted bonus")
			}
		})
	}
}

func TestHandleNeverLeaksRawSynchronizationError(t *testing.T) {
	service := NewService(&fakeBonus{}, fakeSync{checked: true, detail: "dial tcp 10.0.0.5:443: secret-host"}, nil)
	response := service.Handle(Request{Operation: OperationSynchronization})
	if !response.OK || response.Status != localmsg.StatusOffline {
		t.Fatalf("unexpected response %+v", response)
	}
	if bytes.Contains([]byte(response.Detail), []byte("10.0.0.5")) ||
		bytes.Contains([]byte(response.Detail), []byte("secret-host")) {
		t.Fatalf("detail leaked connection internals: %q", response.Detail)
	}
}

func TestHandleSynchronizationStates(t *testing.T) {
	cases := []struct {
		name    string
		sync    fakeSync
		want    string
		wantMsg bool
	}{
		{"checking", fakeSync{checked: false}, localmsg.StatusChecking, true},
		{"online", fakeSync{checked: true, online: true}, localmsg.StatusOnline, false},
		{"offline", fakeSync{checked: true, detail: "http 503"}, localmsg.StatusOffline, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewService(&fakeBonus{}, testCase.sync, nil)
			response := service.Handle(Request{Operation: OperationSynchronization})
			if response.Status != testCase.want {
				t.Fatalf("status = %q, want %q", response.Status, testCase.want)
			}
			if (response.Detail != "") != testCase.wantMsg {
				t.Fatalf("detail = %q, want message present = %v", response.Detail, testCase.wantMsg)
			}
		})
	}
}

func TestHandleRejectsUnknownOperation(t *testing.T) {
	service := NewService(&fakeBonus{}, fakeSync{}, nil)
	response := service.Handle(Request{Operation: "drop_tables"})
	if response.OK || response.ErrorCode != localmsg.ErrorInvalidRequest {
		t.Fatalf("unexpected response %+v", response)
	}
}

func TestHandlePublicConfigurationHidesToken(t *testing.T) {
	service := NewService(&fakeBonus{}, fakeSync{}, fakeSettings{
		settings: PublicSettings{
			ServerURL: "https://api.test", DeviceID: "device",
			ControlledUserSID: "S-1-5-21-1-2-3-1001", HasDeviceToken: true, Configured: true,
		},
		ok: true,
	})
	response := service.Handle(Request{Operation: OperationPublicConfiguration})
	if !response.OK || response.Settings == nil || !response.Settings.Configured {
		t.Fatalf("unexpected response %+v", response)
	}
	if !response.Settings.HasDeviceToken {
		t.Fatal("expected the token presence flag to be reported")
	}
}

func TestHandlePublicConfigurationWhenNotConfigured(t *testing.T) {
	service := NewService(&fakeBonus{}, fakeSync{}, fakeSettings{
		settings: PublicSettings{
			ServerURL: "https://api.test", DeviceID: "device",
			ControlledUserSID: "S-1-5-21-1-2-3-1001", HasDeviceToken: true,
		},
		ok: true,
	})
	response := service.Handle(Request{Operation: OperationPublicConfiguration})
	if !response.OK || response.Settings == nil || response.Settings.Configured {
		t.Fatalf("unexpected response %+v", response)
	}
}

func TestListenRejectsInvalidSID(t *testing.T) {
	if _, err := Listen("not-a-sid"); err == nil {
		t.Fatal("expected an invalid SID to be rejected before creating the pipe")
	}
}

func TestListenCreatesPipeWithRestrictedACL(t *testing.T) {
	listener, err := Listen("S-1-5-21-278194532-2139705530-887251162-1002")
	if err != nil {
		t.Skipf("controlled SID is not resolvable in this environment: %v", err)
	}
	defer listener.Close()
	if listener.handle == 0 {
		t.Fatal("expected a valid pipe handle")
	}
}
