package session

import "testing"

func TestControlledActiveConsoleUsesSIDAndPhysicalConsole(t *testing.T) {
	const controlledSID = "S-1-5-21-1002"
	base := Snapshot{
		SessionID: 1, AccountSID: controlledSID, Console: true,
		Connection: ConnectionActive, Lock: LockUnknown,
	}
	if !base.IsControlledActiveConsole("s-1-5-21-1002") {
		t.Fatal("matching SID on the active physical console was not selected")
	}

	tests := []struct {
		name     string
		snapshot Snapshot
		sid      string
	}{
		{name: "different account", snapshot: base, sid: "S-1-5-21-2001"},
		{name: "missing configured SID", snapshot: base},
		{name: "remote session", snapshot: func() Snapshot { value := base; value.Console = false; return value }(), sid: controlledSID},
		{name: "disconnected console", snapshot: func() Snapshot { value := base; value.Connection = ConnectionDisconnected; return value }(), sid: controlledSID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.snapshot.IsControlledActiveConsole(test.sid) {
				t.Fatalf("unexpected controlled session: %+v", test.snapshot)
			}
		})
	}
}

func TestWTSValuesAreNormalizedWithoutInferringLockState(t *testing.T) {
	tests := []struct {
		raw  uint32
		want ConnectionState
	}{
		{raw: 0, want: ConnectionActive},
		{raw: 1, want: ConnectionConnected},
		{raw: 4, want: ConnectionDisconnected},
		{raw: 5, want: ConnectionOther},
		{raw: 99, want: ConnectionUnknown},
	}
	for _, test := range tests {
		if got := normalizeConnectionState(test.raw); got != test.want {
			t.Fatalf("normalizeConnectionState(%d)=%q, want %q", test.raw, got, test.want)
		}
	}
	if got := protocolName(0); got != "console" {
		t.Fatalf("console protocol=%q", got)
	}
	if got := protocolName(2); got != "rdp" {
		t.Fatalf("RDP protocol=%q", got)
	}

	lockTests := []struct {
		raw  uint32
		want LockState
	}{
		{raw: 0, want: LockLocked},
		{raw: 1, want: LockUnlocked},
		{raw: ^uint32(0), want: LockUnknown},
		{raw: 99, want: LockUnknown},
	}
	for _, test := range lockTests {
		if got := normalizeLockState(test.raw); got != test.want {
			t.Fatalf("normalizeLockState(%d)=%q, want %q", test.raw, got, test.want)
		}
	}
}
