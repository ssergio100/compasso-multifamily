package session

import "testing"

func TestClassifyWindowsSession(t *testing.T) {
	tests := []struct {
		name     string
		state    uint32
		protocol uint16
		flag     int32
		eligible bool
		locked   bool
	}{
		{name: "local unlocked", state: windowsWTSActive, protocol: 0, flag: 1, eligible: true},
		{name: "local locked", state: windowsWTSActive, protocol: 0, flag: 0, eligible: true, locked: true},
		{name: "unknown flag fails closed", state: windowsWTSActive, protocol: 0, flag: -1, eligible: true, locked: true},
		{name: "disconnected is locked", state: windowsWTSDisconnected, protocol: 0, flag: 1, eligible: true, locked: true},
		{name: "rdp ignored", state: windowsWTSActive, protocol: 2, flag: 1},
		{name: "listener ignored", state: 6, protocol: 0, flag: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			eligible, locked := classifyWindowsSession(test.state, test.protocol, test.flag)
			if eligible != test.eligible || locked != test.locked {
				t.Fatalf("classification = %t/%t, want %t/%t", eligible, locked, test.eligible, test.locked)
			}
		})
	}
}
