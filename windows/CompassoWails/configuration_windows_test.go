//go:build windows

package main

import "testing"

func TestValidateConfigurationForm(t *testing.T) {
	valid := configurationRequest{
		ServerURL: "https://example.test", DeviceID: "device", DeviceToken: "token",
		ControlledUser: "child", ControlledUserSID: "S-1-5-21-1-2-3-1001", Confirmed: true,
	}
	if message := validateConfigurationForm(valid); message != "" {
		t.Fatalf("valid configuration rejected: %s", message)
	}

	tests := []struct {
		name   string
		change func(*configurationRequest)
	}{
		{"confirmation", func(request *configurationRequest) { request.Confirmed = false }},
		{"account", func(request *configurationRequest) { request.ControlledUserSID = "" }},
		{"remote HTTP", func(request *configurationRequest) { request.ServerURL = "http://example.test" }},
		{"path", func(request *configurationRequest) { request.ServerURL = "https://example.test/api" }},
		{"credentials", func(request *configurationRequest) { request.ServerURL = "https://user@example.test" }},
		{"device", func(request *configurationRequest) { request.DeviceID = "" }},
		{"token", func(request *configurationRequest) { request.DeviceToken = "" }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := valid
			testCase.change(&request)
			if message := validateConfigurationForm(request); message == "" {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}

func TestValidateConfigurationFormAllowsLoopbackHTTP(t *testing.T) {
	for _, serverURL := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		request := configurationRequest{
			ServerURL: serverURL, DeviceID: "device", DeviceToken: "token",
			ControlledUser: "child", ControlledUserSID: "S-1-5-21-1-2-3-1001", Confirmed: true,
		}
		if message := validateConfigurationForm(request); message != "" {
			t.Fatalf("loopback URL %q rejected: %s", serverURL, message)
		}
	}
}

func TestExecutableFromServiceCommand(t *testing.T) {
	tests := map[string]string{
		`"C:\Program Files\Compasso\Agent\CompassoAgent.exe" service`: `C:\Program Files\Compasso\Agent\CompassoAgent.exe`,
		`C:\Compasso\CompassoAgent.exe service`:                       `C:\Compasso\CompassoAgent.exe`,
		`C:\Compasso\CompassoAgent.exe`:                               `C:\Compasso\CompassoAgent.exe`,
	}
	for command, wanted := range tests {
		got, err := executableFromServiceCommand(command)
		if err != nil || got != wanted {
			t.Fatalf("command %q: executable=%q error=%v, want %q", command, got, err, wanted)
		}
	}
}

func TestEligibleWindowsAccountsAndConfigurationBridge(t *testing.T) {
	accounts, err := eligibleWindowsAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) == 0 {
		t.Fatal("expected at least one eligible local account")
	}
	for _, account := range accounts {
		if account.Name == "" || account.SID == "" {
			t.Fatalf("incomplete account: %+v", account)
		}
	}
	bridge, nonce, err := newConfigurationBridge()
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	if len(nonce) != 32 {
		t.Fatalf("nonce length=%d", len(nonce))
	}
}
