// Package availability checks domain registration status through external services.
package availability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type DomainStatus string

const (
	StatusAvailable  DomainStatus = "available"
	StatusRegistered DomainStatus = "registered"
	StatusUnknown    DomainStatus = "unknown"
)

type Result struct {
	Domain         string       `json:"domain"`
	Status         DomainStatus `json:"status"`
	Message        string       `json:"message,omitempty"`
	DurationMS     int64        `json:"durationMs"`
	Cached         bool         `json:"cached,omitempty"`
	Provider       string       `json:"provider,omitempty"`
	ExpirationTime string       `json:"expirationTime,omitempty"`
}

type rdapPayload struct {
	Events []struct {
		Action string `json:"eventAction"`
		Date   string `json:"eventDate"`
	} `json:"events"`
}
type Checker interface {
	Check(context.Context, string) Result
}

// RDAPChecker implements Checker using the Registration Data Access Protocol.
type RDAPChecker struct {
	baseURL   string
	provider  string
	client    *http.Client
	semaphore chan struct{}
}

func NewRDAPChecker(baseURL string, timeout time.Duration) *RDAPChecker {
	return NewNamedRDAPChecker(providerName(baseURL), baseURL, timeout, 3)
}

func NewLimitedRDAPChecker(baseURL string, timeout time.Duration, concurrency int) *RDAPChecker {
	return NewNamedRDAPChecker(providerName(baseURL), baseURL, timeout, concurrency)
}

func NewNamedRDAPChecker(provider, baseURL string, timeout time.Duration, concurrency int) *RDAPChecker {
	if concurrency < 1 {
		concurrency = 1
	}
	return &RDAPChecker{baseURL: strings.TrimRight(baseURL, "/") + "/", provider: provider, semaphore: make(chan struct{}, concurrency), client: &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, MaxIdleConns: 50, MaxIdleConnsPerHost: 20, IdleConnTimeout: 60 * time.Second, ResponseHeaderTimeout: timeout},
	}}
}

func (c *RDAPChecker) Check(ctx context.Context, domain string) (result Result) {
	started := time.Now()
	result = Result{Domain: domain, Status: StatusUnknown, Provider: c.provider}
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	select {
	case c.semaphore <- struct{}{}:
		defer func() { <-c.semaphore }()
	case <-ctx.Done():
		result.Message = "查询已取消"
		return result
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+url.PathEscape(domain), nil)
	if err != nil {
		result.Message = err.Error()
		return result
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")
	req.Header.Set("User-Agent", "domscan/1.0")
	resp, err := c.client.Do(req)
	if err != nil {
		result.Message = friendlyNetworkError(err)
		return result
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		result.Status, result.Message = StatusRegistered, "RDAP 中存在注册记录"
		var payload rdapPayload
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err == nil {
			for _, event := range payload.Events {
				if strings.Contains(strings.ToLower(event.Action), "expiration") {
					result.ExpirationTime = event.Date
					break
				}
			}
		}
	case http.StatusNotFound:
		result.Status, result.Message = StatusAvailable, "RDAP 中未找到注册记录"
	case http.StatusTooManyRequests:
		result.Message = "查询过于频繁，请稍后重试"
	default:
		result.Message = fmt.Sprintf("RDAP 服务返回 HTTP %d", resp.StatusCode)
	}
	return result
}

func providerName(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return rawURL
}

func friendlyNetworkError(err error) string {
	if strings.Contains(err.Error(), "context deadline exceeded") || strings.Contains(err.Error(), "Client.Timeout") {
		return "查询超时"
	}
	return "无法连接 RDAP 服务"
}
