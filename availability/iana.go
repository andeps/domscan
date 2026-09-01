package availability

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type IANAChecker struct {
	services map[string][]Checker
}

type bootstrapDocument struct {
	Services [][][]string `json:"services"`
}

func NewIANAChecker(ctx context.Context, bootstrapURL string, timeout time.Duration, concurrency int) (*IANAChecker, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bootstrapURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载 IANA RDAP 路由表：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("IANA RDAP 路由表返回 HTTP %d", resp.StatusCode)
	}
	var document bootstrapDocument
	if err := json.NewDecoder(resp.Body).Decode(&document); err != nil {
		return nil, fmt.Errorf("解析 IANA RDAP 路由表：%w", err)
	}
	return newIANACheckerFromDocument(document, client, concurrency)
}

func newIANACheckerFromDocument(document bootstrapDocument, client *http.Client, concurrency int) (*IANAChecker, error) {
	services := make(map[string][]Checker)
	checkersByBase := make(map[string]*RDAPChecker)
	if concurrency < 1 {
		concurrency = 1
	}
	for _, entry := range document.Services {
		if len(entry) != 2 {
			continue
		}
		for _, base := range entry[1] {
			if _, exists := checkersByBase[base]; !exists {
				checkersByBase[base] = &RDAPChecker{baseURL: strings.TrimRight(base, "/") + "/domain/", provider: providerName(base), client: client, semaphore: make(chan struct{}, concurrency)}
			}
		}
		for _, tld := range entry[0] {
			for _, base := range entry[1] {
				services[strings.ToLower(tld)] = append(services[strings.ToLower(tld)], checkersByBase[base])
			}
		}
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("IANA RDAP 路由表为空")
	}
	return &IANAChecker{services: services}, nil
}

func (c *IANAChecker) Check(ctx context.Context, domain string) Result {
	parts := strings.Split(domain, ".")
	tld := parts[len(parts)-1]
	checkers := c.services[strings.ToLower(tld)]
	if len(checkers) == 0 {
		return Result{Domain: domain, Status: StatusUnknown, Provider: "IANA bootstrap", Message: "没有该后缀的权威 RDAP 地址"}
	}
	return NewFallbackChecker(checkers...).Check(ctx, domain)
}
