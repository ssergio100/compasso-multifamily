package accountmail

import "testing"

func TestNewSMTPValidatesSecurityRelevantConfiguration(t *testing.T) {
	tests := []struct {
		name, address, username, password, from string
		valid                                   bool
	}{
		{name: "valid authenticated", address: "smtp.example:587", username: "user", password: "secret", from: "contas@example.com", valid: true},
		{name: "valid relay", address: "smtp.example:25", from: "contas@example.com", valid: true},
		{name: "missing port", address: "smtp.example", from: "contas@example.com"},
		{name: "display sender", address: "smtp.example:587", from: "Compasso <contas@example.com>"},
		{name: "header injection", address: "smtp.example:587", from: "contas@example.com\r\nBcc: attacker@example.com"},
		{name: "partial authentication", address: "smtp.example:587", username: "user", from: "contas@example.com"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mailer, err := NewSMTP(test.address, test.username, test.password, test.from)
			if test.valid && (err != nil || !mailer.Available()) {
				t.Fatalf("valid SMTP rejected: mailer=%v err=%v", mailer, err)
			}
			if !test.valid && err == nil {
				t.Fatal("invalid SMTP configuration accepted")
			}
		})
	}
}
