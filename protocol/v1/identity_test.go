package v1

import "testing"

func TestValidInstallationIDRequiresCanonicalUUIDV4(t *testing.T) {
	for value, valid := range map[string]bool{
		"79e78a4f-713b-4e19-a756-b60a0505248a": true,
		"79e78a4f-713b-3e19-a756-b60a0505248a": false,
		"79e78a4f-713b-4e19-7756-b60a0505248a": false,
		"79E78A4F-713B-4E19-A756-B60A0505248A": false,
		"not-a-uuid":                           false,
	} {
		if ValidInstallationID(value) != valid {
			t.Fatalf("ValidInstallationID(%q)=%t, want %t", value, !valid, valid)
		}
	}
}
