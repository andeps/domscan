package availability

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestIANACheckerRoutesDomainToAuthoritativeServer(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://rdap.registry.test/rdap/domain/example.com" {
			t.Fatalf("unexpected authoritative URL: %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
	})}
	document := bootstrapDocument{Services: [][][]string{{{"com"}, {"https://rdap.registry.test/rdap/"}}}}
	checker, err := newIANACheckerFromDocument(document, client, 2)
	if err != nil {
		t.Fatal(err)
	}

	result := checker.Check(context.Background(), "example.com")
	if result.Status != StatusAvailable || result.Provider != "rdap.registry.test" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
