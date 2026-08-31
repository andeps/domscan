package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\nDOMAIN_TOOL_ADDR=:9090\nDOMAIN_GIN_MODE=test\nDOMAIN_RDAP_BOOTSTRAP_URL='https://data.example/dns.json'\nDOMAIN_RDAP_FALLBACK_URLS=https://one.example/domain/, https://two.example/domain/\nDOMAIN_RDAP_PROVIDER_CONCURRENCY=2\nDOMAIN_RDAP_TIMEOUT=3s\nDOMAIN_HTTP_REQUEST_TIMEOUT=2m\nDOMAIN_REDIS_ENABLED=true\nDOMAIN_REDIS_URL=redis://cache:6379/1\nDOMAIN_CACHE_TTL=48h\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != ":9090" || cfg.GinMode != "test" || cfg.RDAPTimeout != 3*time.Second || cfg.RequestTimeout != 2*time.Minute || !cfg.RedisEnabled || cfg.CacheTTL != 48*time.Hour || cfg.RDAPProviderConcurrency != 2 || len(cfg.RDAPFallbackURLs) != 2 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestEnvironmentOverridesDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("DOMAIN_TOOL_ADDR=:9090\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOMAIN_TOOL_ADDR", ":7070")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != ":7070" {
		t.Fatalf("address = %q, want :7070", cfg.Address)
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("DOMAIN_RDAP_TIMEOUT=never\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid duration error")
	}
}

func TestLoadRejectsInvalidRedisFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("DOMAIN_REDIS_ENABLED=sometimes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid Redis flag error")
	}
}
