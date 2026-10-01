package windowsservice

import "testing"

func TestConfigurationValidationAndPublicView(t *testing.T) {
	configuration := Configuration{
		ServerURL: "https://example.test", DeviceID: "device", DeviceToken: "secret",
		ControlledUserSID: "S-1-5-21-1-2-3-1001",
	}
	if err := configuration.Validate(); err != nil {
		t.Fatalf("valid configuration rejected: %v", err)
	}
	public := configuration.Public()
	if !public.HasDeviceToken || public.ServerURL != configuration.ServerURL || public.DeviceID != configuration.DeviceID ||
		public.ControlledUserSID != configuration.ControlledUserSID {
		t.Fatalf("unexpected public configuration: %+v", public)
	}
}

func TestConfigurationRejectsUnsafeServerAndMissingFields(t *testing.T) {
	tests := []Configuration{
		{ServerURL: "http://example.test", DeviceID: "device", DeviceToken: "secret", ControlledUserSID: "S-1-5-21-1"},
		{ServerURL: "https://example.test/path", DeviceID: "device", DeviceToken: "secret", ControlledUserSID: "S-1-5-21-1"},
		{ServerURL: "https://example.test", DeviceID: "", DeviceToken: "secret", ControlledUserSID: "S-1-5-21-1"},
		{ServerURL: "https://example.test", DeviceID: "device", DeviceToken: "secret", ControlledUserSID: "not-a-sid"},
	}
	for index, configuration := range tests {
		if err := configuration.Validate(); err == nil {
			t.Fatalf("unsafe configuration %d accepted", index)
		}
	}
}
