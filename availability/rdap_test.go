package availability

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestRDAPCheckerStatusMapping(t *testing.T) {
	tests := []struct {
		code int
		want DomainStatus
	}{
		{http.StatusOK, StatusRegistered},
		{http.StatusNotFound, StatusAvailable},
		{http.StatusTooManyRequests, StatusUnknown},
		{http.StatusInternalServerError, StatusUnknown},
	}
	for _, test := range tests {
		checker := NewRDAPChecker("https://rdap.test/domain/", time.Second)
		checker.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/domain/example.com" {
				t.Errorf("unexpected path %s", request.URL.Path)
			}
			return &http.Response{StatusCode: test.code, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
		})
		result := checker.Check(context.Background(), "example.com")
		if result.Status != test.want {
			t.Errorf("HTTP %d: got %s, want %s", test.code, result.Status, test.want)
		}
	}
}

func TestRDAPCheckerParsesExpiration(t *testing.T) {
	checker := NewRDAPChecker("https://rdap.test/domain/", time.Second)
	checker.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := `{"events":[{"eventAction":"registration","eventDate":"2020-01-01T00:00:00Z"},{"eventAction":"expiration","eventDate":"2030-01-01T00:00:00Z"}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	result := checker.Check(context.Background(), "example.com")
	if result.ExpirationTime != "2030-01-01T00:00:00Z" {
		t.Fatalf("expiration = %q", result.ExpirationTime)
	}
}
