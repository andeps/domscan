package httpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"domscan/availability"
	"domscan/user"

	"github.com/gin-gonic/gin"
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

type delayedStreamingChecker struct {
	slowDone chan struct{}
}

type flushOnlyResponseWriter struct {
	header  http.Header
	output  *io.PipeWriter
	pending bytes.Buffer
	mu      sync.Mutex
}

func (w *flushOnlyResponseWriter) Header() http.Header { return w.header }

func (w *flushOnlyResponseWriter) WriteHeader(_ int) {}

func (w *flushOnlyResponseWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pending.Write(payload)
}

func (w *flushOnlyResponseWriter) Flush() {
	w.mu.Lock()
	payload := append([]byte(nil), w.pending.Bytes()...)
	w.pending.Reset()
	w.mu.Unlock()
	if len(payload) > 0 {
		_, _ = w.output.Write(payload)
	}
}

func (c delayedStreamingChecker) Check(_ context.Context, domain string) availability.Result {
	if domain == "fast0.com" {
		time.Sleep(500 * time.Millisecond)
		close(c.slowDone)
	}
	return availability.Result{Domain: domain, Status: availability.StatusAvailable}
}

type notificationCall struct {
	recipient string
	results   []availability.Result
}

type recordingNotifier struct {
	calls chan notificationCall
}

func (n *recordingNotifier) Notify(_ context.Context, recipient string, results []availability.Result) error {
	n.calls <- notificationCall{recipient: recipient, results: append([]availability.Result(nil), results...)}
	return nil
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

func TestSearchFlushesEachResultBeforeAllChecksFinish(t *testing.T) {
	slowDone := make(chan struct{})
	body := `{"options":{"keywords":["fast"],"tlds":["com"],"minLength":4,"maxLength":5,"digitMode":"allow","limit":2},"concurrency":2}`
	reader, writer := io.Pipe()
	responseWriter := &flushOnlyResponseWriter{header: make(http.Header), output: writer}
	req := httptest.NewRequest(http.MethodPost, "/api/search", strings.NewReader(body))
	handler := New(delayedStreamingChecker{slowDone: slowDone}, Options{GinMode: "test", RequestTimeout: time.Minute})
	go func() {
		handler.ServeHTTP(responseWriter, req)
		_ = writer.Close()
	}()

	scanner := bufio.NewScanner(reader)
	if !scanner.Scan() {
		t.Fatalf("first streamed line missing: %v", scanner.Err())
	}
	if responseWriter.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("X-Accel-Buffering = %q", responseWriter.Header().Get("X-Accel-Buffering"))
	}
	var first availability.Result
	if err := json.Unmarshal(scanner.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.Domain != "fast.com" {
		t.Fatalf("first streamed result = %+v", first)
	}
	select {
	case <-slowDone:
		t.Fatal("first result was buffered until all checks completed")
	default:
	}

	if !scanner.Scan() {
		t.Fatalf("second streamed line missing: %v", scanner.Err())
	}
	if !scanner.Scan() {
		t.Fatalf("summary line missing: %v", scanner.Err())
	}
}

func TestSearchNotifiesLoggedInUser(t *testing.T) {
	notifier := &recordingNotifier{calls: make(chan notificationCall, 1)}
	app := &server{checker: fakeChecker{}, notifier: notifier, notifyBatch: 10}
	router := gin.New()
	router.POST("/api/search", func(c *gin.Context) {
		c.Set(user.EmailContextKey, "member@example.com")
		c.Next()
	}, app.handleSearch)

	body := `{"options":{"keywords":["free"],"tlds":["com"],"minLength":4,"maxLength":4,"digitMode":"forbid","limit":1},"concurrency":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/search", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}

	select {
	case call := <-notifier.calls:
		if call.recipient != "member@example.com" {
			t.Fatalf("recipient = %q", call.recipient)
		}
		if len(call.results) != 1 || call.results[0].Domain != "free.com" {
			t.Fatalf("unexpected notification results: %+v", call.results)
		}
	case <-time.After(time.Second):
		t.Fatal("notification was not queued")
	}
}

func TestSearchDoesNotNotifyWithoutLoggedInUser(t *testing.T) {
	notifier := &recordingNotifier{calls: make(chan notificationCall, 1)}
	body := `{"options":{"keywords":["free"],"tlds":["com"],"minLength":4,"maxLength":4,"digitMode":"forbid","limit":1},"concurrency":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/search", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	New(fakeChecker{}, Options{GinMode: "test", RequestTimeout: time.Minute, Notifier: notifier}).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"notificationQueued":1`) {
		t.Fatalf("anonymous search queued notification: %s", recorder.Body.String())
	}
	select {
	case call := <-notifier.calls:
		t.Fatalf("anonymous notification: %+v", call)
	default:
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

func TestPartialOptionsKeepDefaultRequestTimeout(t *testing.T) {
	body := `{"domains":["free.com"],"concurrency":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/check", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	New(fakeChecker{}, Options{GinMode: "test"}).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"domain":"free.com"`) {
		t.Fatalf("partial options canceled request: status=%d body=%s", recorder.Code, recorder.Body.String())
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

func TestRootDoesNotServeFrontend(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	New(fakeChecker{}).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("unexpected content type: %s", contentType)
	}
}
