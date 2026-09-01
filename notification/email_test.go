package notification

import "testing"

func TestValidRecipient(t *testing.T) {
	if got, err := validRecipient("member@example.com"); err != nil || got != "member@example.com" {
		t.Fatalf("validRecipient() = %q, %v", got, err)
	}
}

func TestValidRecipientRejectsHeaderInjection(t *testing.T) {
	if _, err := validRecipient("member@example.com\r\nBcc: attacker@example.com"); err == nil {
		t.Fatal("expected injected recipient to be rejected")
	}
}
