package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"domscan/availability"
)

type fakeChecker struct{}

func (fakeChecker) Check(_ context.Context, domain string) availability.Result {
	status := availability.StatusAvailable
	if strings.HasPrefix(domain, "taken") {
		status = availability.StatusRegistered
	}
	return availability.Result{Domain: domain, Status: status}
}

type cacheAwareFakeChecker struct{}

func (cacheAwareFakeChecker) Check(_ context.Context, domain string) availability.Result {
	if domain == "cached.com" {
		return availability.Result{Domain: domain, Status: availability.StatusAvailable, Cached: true}
	}
	return availability.Result{Domain: domain, Status: availability.StatusAvailable}
}

func TestGenerateEndpoint(t *testing.T) {
	body := `{"keywords":["go"],"tlds":["com"],"minLength":2,"maxLength":4,"digitMode":"forbid","limit":10}`
	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	New(fakeChecker{}).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Domains []string `json:"domains"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Domains) != 1 || response.Domains[0] != "go.com" {
		t.Fatalf("unexpected response: %v", response.Domains)
	}
}

func TestCheckEndpointStreamsResults(t *testing.T) {
	body := []byte(`{"domains":["free.com","taken.com"],"concurrency":2}`)
	req := httptest.NewRequest(http.MethodPost, "/api/check", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	New(fakeChecker{}).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/x-ndjson") {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d NDJSON lines, want 2", len(lines))
	}
	statuses := map[availability.DomainStatus]bool{}
	for _, line := range lines {
		var r availability.Result
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		statuses[r.Status] = true
	}
	if !statuses[availability.StatusAvailable] || !statuses[availability.StatusRegistered] {
		t.Fatalf("unexpected statuses: %v", statuses)
	}
}

func TestSearchSkipsCachedCandidatesAndContinues(t *testing.T) {
	body := `{"options":{"keywords":["cached"],"tlds":["com"],"minLength":6,"maxLength":7,"digitMode":"allow","limit":1},"concurrency":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/search", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	New(cacheAwareFakeChecker{}).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d NDJSON lines, want result + summary: %s", len(lines), recorder.Body.String())
	}
	var result availability.Result
	if err := json.Unmarshal([]byte(lines[0]), &result); err != nil {
		t.Fatal(err)
	}
	if result.Domain != "cached0.com" || result.Cached {
		t.Fatalf("search did not continue past cache: %+v", result)
	}
	var summary searchSummary
	if err := json.Unmarshal([]byte(lines[1]), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Event != "summary" || summary.CachedSkipped != 1 || summary.Fresh != 1 {
		t.Fatalf("unexpected search summary: %+v", summary)
	}
}

func TestSearchAcceptsFuzzyOptions(t *testing.T) {
	body := `{"options":{"keywords":["ai"],"tlds":["com"],"minLength":6,"maxLength":6,"digitMode":"forbid","fuzzyMode":"suffix","limit":1},"concurrency":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/search", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	New(fakeChecker{}).ServeHTTP(recorder, req)
	lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
	var result availability.Result
	if err := json.Unmarshal([]byte(lines[0]), &result); err != nil {
		t.Fatal(err)
	}
	if result.Domain != "aiaaaa.com" {
		t.Fatalf("fuzzy search result = %s", result.Domain)
	}
}

func TestHealthIdentifiesGinAndSetsSecurityHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	recorder := httptest.NewRecorder()
	New(fakeChecker{}).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"framework":"gin"`) {
		t.Fatalf("health response does not identify Gin: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"cache":"disabled"`) {
		t.Fatalf("health response does not expose cache status: %s", recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("security header = %q, want nosniff", got)
	}
}

func TestHealthReportsRedisCache(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	recorder := httptest.NewRecorder()
	New(fakeChecker{}, Options{GinMode: "test", RequestTimeout: time.Minute, CacheEnabled: true}).ServeHTTP(recorder, req)
	if !strings.Contains(recorder.Body.String(), `"cache":"redis"`) {
		t.Fatalf("health response does not report Redis: %s", recorder.Body.String())
	}
}

func TestUnsupportedMethodReturnsJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/generate", nil)
	recorder := httptest.NewRecorder()
	New(fakeChecker{}).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("unexpected content type: %s", contentType)
	}
}
