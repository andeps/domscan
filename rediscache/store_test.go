package rediscache

import (
	"testing"
	"time"
)

func TestNewRejectsUnsupportedURL(t *testing.T) {
	if _, err := New("http://localhost:6379", "domain:", time.Second); err == nil {
		t.Fatal("expected unsupported Redis URL error")
	}
}

func TestKeyNormalizesDomain(t *testing.T) {
	store, err := New("redis://localhost:6379/0", "domscan:", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := store.key("Example.COM"); got != "domscan:example.com" {
		t.Fatalf("key = %q", got)
	}
}
