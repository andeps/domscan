// Package config loads and validates application runtime configuration.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config contains every deployment-level setting used by the application.
type Config struct {
	Address                                         string
	GinMode                                         string
	RDAPBootstrapURL                                string
	RDAPFallbackURLs                                []string
	RDAPProviderConcurrency                         int
	RDAPTimeout                                     time.Duration
	ReadHeaderTimeout                               time.Duration
	IdleTimeout                                     time.Duration
	ShutdownTimeout                                 time.Duration
	RequestTimeout                                  time.Duration
	RedisEnabled                                    bool
	RedisURL                                        string
	RedisKeyPrefix                                  string
	RedisDialTimeout                                time.Duration
	CacheTTL                                        time.Duration
	CacheUnknownTTL                                 time.Duration
	EmailEnabled                                    bool
	SMTPHost, SMTPUsername, SMTPPassword, EmailFrom string
	SMTPPort                                        int
	EmailTo                                         []string
	EmailTimeout                                    time.Duration
	EmailBatchSize                                  int
}

// Load reads a dotenv file when present, then overlays process environment
// variables. Process environment variables always have higher priority.
func Load(path string) (Config, error) {
	fileValues, err := readDotEnv(path)
	if err != nil {
		return Config{}, err
	}
	value := func(key, fallback string) string {
		if current, exists := os.LookupEnv(key); exists {
			return current
		}
		if current, exists := fileValues[key]; exists {
			return current
		}
		return fallback
	}

	cfg := Config{
		Address:          value("DOMAIN_TOOL_ADDR", ":8080"),
		GinMode:          value("DOMAIN_GIN_MODE", "release"),
		RDAPBootstrapURL: value("DOMAIN_RDAP_BOOTSTRAP_URL", "https://data.iana.org/rdap/dns.json"),
		RedisURL:         value("DOMAIN_REDIS_URL", "redis://localhost:6379/0"),
		RedisKeyPrefix:   value("DOMAIN_REDIS_KEY_PREFIX", "domscan:availability:"),
		SMTPHost:         value("DOMAIN_SMTP_HOST", ""), SMTPUsername: value("DOMAIN_SMTP_USERNAME", ""), SMTPPassword: value("DOMAIN_SMTP_PASSWORD", ""), EmailFrom: value("DOMAIN_SMTP_FROM", ""),
	}
	emailEnabled, parseEmailErr := strconv.ParseBool(value("DOMAIN_EMAIL_ENABLED", "false"))
	if parseEmailErr != nil {
		return Config{}, fmt.Errorf("DOMAIN_EMAIL_ENABLED 必须是 true 或 false")
	}
	cfg.EmailEnabled = emailEnabled
	cfg.SMTPPort, parseEmailErr = strconv.Atoi(value("DOMAIN_SMTP_PORT", "587"))
	if parseEmailErr != nil || cfg.SMTPPort < 1 || cfg.SMTPPort > 65535 {
		return Config{}, fmt.Errorf("DOMAIN_SMTP_PORT 必须是 1 到 65535")
	}
	for _, item := range strings.Split(value("DOMAIN_EMAIL_TO", ""), ",") {
		if item = strings.TrimSpace(item); item != "" {
			cfg.EmailTo = append(cfg.EmailTo, item)
		}
	}
	if cfg.EmailEnabled && (cfg.SMTPHost == "" || cfg.EmailFrom == "" || len(cfg.EmailTo) == 0) {
		return Config{}, fmt.Errorf("启用邮箱通知时必须配置 SMTP 主机、发件人和收件人")
	}
	cfg.EmailBatchSize, parseEmailErr = strconv.Atoi(value("DOMAIN_EMAIL_BATCH_SIZE", "10"))
	if parseEmailErr != nil || cfg.EmailBatchSize < 1 || cfg.EmailBatchSize > 1000 {
		return Config{}, fmt.Errorf("DOMAIN_EMAIL_BATCH_SIZE 必须在 1 到 1000 之间")
	}
	if strings.TrimSpace(cfg.Address) == "" {
		return Config{}, fmt.Errorf("DOMAIN_TOOL_ADDR 不能为空")
	}
	if cfg.GinMode != "debug" && cfg.GinMode != "release" && cfg.GinMode != "test" {
		return Config{}, fmt.Errorf("DOMAIN_GIN_MODE 必须是 debug、release 或 test")
	}
	redisEnabled, err := strconv.ParseBool(value("DOMAIN_REDIS_ENABLED", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("DOMAIN_REDIS_ENABLED 必须是 true 或 false")
	}
	cfg.RedisEnabled = redisEnabled
	if cfg.RedisEnabled && strings.TrimSpace(cfg.RedisURL) == "" {
		return Config{}, fmt.Errorf("启用 Redis 时 DOMAIN_REDIS_URL 不能为空")
	}
	if strings.TrimSpace(cfg.RedisKeyPrefix) == "" {
		return Config{}, fmt.Errorf("DOMAIN_REDIS_KEY_PREFIX 不能为空")
	}
	if !validHTTPURL(cfg.RDAPBootstrapURL) {
		return Config{}, fmt.Errorf("DOMAIN_RDAP_BOOTSTRAP_URL 必须是有效的 HTTP(S) 地址")
	}
	for _, rawURL := range strings.Split(value("DOMAIN_RDAP_FALLBACK_URLS", "https://rdap-bootstrap.arin.net/bootstrap/domain/,https://rdap.org/domain/"), ",") {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}
		if !validHTTPURL(rawURL) {
			return Config{}, fmt.Errorf("DOMAIN_RDAP_FALLBACK_URLS 包含无效地址 %q", rawURL)
		}
		cfg.RDAPFallbackURLs = append(cfg.RDAPFallbackURLs, rawURL)
	}
	if len(cfg.RDAPFallbackURLs) == 0 {
		return Config{}, fmt.Errorf("DOMAIN_RDAP_FALLBACK_URLS 至少需要一个地址")
	}
	providerConcurrency, err := strconv.Atoi(value("DOMAIN_RDAP_PROVIDER_CONCURRENCY", "3"))
	if err != nil || providerConcurrency < 1 || providerConcurrency > 20 {
		return Config{}, fmt.Errorf("DOMAIN_RDAP_PROVIDER_CONCURRENCY 必须在 1 到 20 之间")
	}
	cfg.RDAPProviderConcurrency = providerConcurrency

	durations := []struct {
		key      string
		fallback string
		target   *time.Duration
	}{
		{"DOMAIN_RDAP_TIMEOUT", "12s", &cfg.RDAPTimeout},
		{"DOMAIN_HTTP_READ_HEADER_TIMEOUT", "5s", &cfg.ReadHeaderTimeout},
		{"DOMAIN_HTTP_IDLE_TIMEOUT", "60s", &cfg.IdleTimeout},
		{"DOMAIN_HTTP_SHUTDOWN_TIMEOUT", "5s", &cfg.ShutdownTimeout},
		{"DOMAIN_HTTP_REQUEST_TIMEOUT", "15m", &cfg.RequestTimeout},
		{"DOMAIN_REDIS_DIAL_TIMEOUT", "2s", &cfg.RedisDialTimeout},
		{"DOMAIN_CACHE_TTL", "24h", &cfg.CacheTTL},
		{"DOMAIN_CACHE_UNKNOWN_TTL", "5m", &cfg.CacheUnknownTTL},
		{"DOMAIN_EMAIL_TIMEOUT", "15s", &cfg.EmailTimeout},
	}
	for _, item := range durations {
		duration, parseErr := time.ParseDuration(value(item.key, item.fallback))
		if parseErr != nil || duration <= 0 {
			return Config{}, fmt.Errorf("%s 必须是大于零的 Go duration，例如 12s 或 5m", item.key)
		}
		*item.target = duration
	}
	return cfg, nil
}

func validHTTPURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func readDotEnv(path string) (map[string]string, error) {
	values := make(map[string]string)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取配置文件 %s：%w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, rawValue, found := strings.Cut(line, "=")
		key, rawValue = strings.TrimSpace(key), strings.TrimSpace(rawValue)
		if !found || !validKey(key) {
			return nil, fmt.Errorf("%s 第 %d 行格式无效", path, lineNumber)
		}
		parsedValue, parseErr := unquote(rawValue)
		if parseErr != nil {
			return nil, fmt.Errorf("%s 第 %d 行：%w", path, lineNumber, parseErr)
		}
		values[key] = parsedValue
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取配置文件 %s：%w", path, err)
	}
	return values, nil
}

func validKey(key string) bool {
	if key == "" {
		return false
	}
	for i, char := range key {
		if (char < 'A' || char > 'Z') && char != '_' && !(i > 0 && char >= '0' && char <= '9') {
			return false
		}
	}
	return true
}

func unquote(value string) (string, error) {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1], nil
	}
	if strings.HasPrefix(value, `"`) {
		result, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("引号字符串无效")
		}
		return result, nil
	}
	return value, nil
}
