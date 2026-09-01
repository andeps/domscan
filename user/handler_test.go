package user

import "testing"

func TestValidEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
		valid bool
	}{
		{name: "normal", email: "member@example.com", valid: true},
		{name: "trimmed and normalized", email: " MEMBER@EXAMPLE.COM ", valid: true},
		{name: "missing domain", email: "member@", valid: false},
		{name: "display name", email: "Member <member@example.com>", valid: false},
		{name: "header injection", email: "member@example.com\r\nBcc: attacker@example.com", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validEmail(tt.email); got != tt.valid {
				t.Fatalf("validEmail(%q) = %v, want %v", tt.email, got, tt.valid)
			}
		})
	}
}
