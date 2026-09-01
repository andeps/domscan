package availability

import (
	"context"
	"errors"
	"testing"
	"time"
)

type countingChecker struct{ calls int }

func (c *countingChecker) Check(_ context.Context, domain string) Result {
	c.calls++
	return Result{Domain: domain, Status: StatusAvailable, Message: "fresh"}
}

type memoryCache struct {
	result Result
	found  bool
	getErr error
	setTTL time.Duration
	sets   int
}

func (c *memoryCache) Get(_ context.Context, _ string) (Result, bool, error) {
	return c.result, c.found, c.getErr
}

func (c *memoryCache) Set(_ context.Context, _ string, result Result, ttl time.Duration) error {
	c.result, c.found, c.setTTL = result, true, ttl
	c.sets++
	return nil
}

func TestCachedCheckerSkipsWorkWhenRequestAlreadyCanceled(t *testing.T) {
	next := &countingChecker{}
	cache := &memoryCache{}
	checker := NewCachedChecker(next, cache, time.Hour, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := checker.Check(ctx, "canceled.example")
	if result.Status != StatusUnknown || result.Message != "查询已取消" {
		t.Fatalf("unexpected canceled result: %+v", result)
	}
	if next.calls != 0 || cache.sets != 0 {
		t.Fatalf("canceled request performed work: calls=%d sets=%d", next.calls, cache.sets)
	}
}

func TestCachedCheckerDoesNotWriteAfterCheckIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	next := CheckerFunc(func(_ context.Context, domain string) Result {
		cancel()
		return Result{Domain: domain, Status: StatusUnknown, Message: "查询已取消"}
	})
	cache := &memoryCache{}
	checker := NewCachedChecker(next, cache, time.Hour, time.Minute)

	checker.Check(ctx, "canceled.example")
	if cache.sets != 0 {
		t.Fatalf("cache writes after cancellation = %d, want 0", cache.sets)
	}
}

func TestCachedCheckerReturnsCacheHitWithoutScanning(t *testing.T) {
	next := &countingChecker{}
	cache := &memoryCache{result: Result{Domain: "example.com", Status: StatusRegistered}, found: true}
	checker := NewCachedChecker(next, cache, time.Hour, time.Minute)

	result := checker.Check(context.Background(), "example.com")
	if !result.Cached || result.Status != StatusRegistered {
		t.Fatalf("unexpected cached result: %+v", result)
	}
	if next.calls != 0 {
		t.Fatalf("underlying checker called %d times, want 0", next.calls)
	}
}

func TestCachedCheckerStoresFreshResult(t *testing.T) {
	next := &countingChecker{}
	cache := &memoryCache{}
	checker := NewCachedChecker(next, cache, 24*time.Hour, 5*time.Minute)

	result := checker.Check(context.Background(), "free.example")
	if result.Cached || next.calls != 1 || !cache.found || cache.setTTL != 24*time.Hour {
		t.Fatalf("unexpected fresh result/cache: result=%+v calls=%d cache=%+v", result, next.calls, cache)
	}
}

func TestCachedCheckerFallsBackWhenCacheFails(t *testing.T) {
	next := &countingChecker{}
	cache := &memoryCache{getErr: errors.New("redis unavailable")}
	checker := NewCachedChecker(next, cache, time.Hour, time.Minute)

	result := checker.Check(context.Background(), "free.example")
	if result.Status != StatusAvailable || next.calls != 1 {
		t.Fatalf("unexpected fallback result: %+v, calls=%d", result, next.calls)
	}
}

func TestCachedCheckerUsesShortTTLForUnknownResult(t *testing.T) {
	next := CheckerFunc(func(_ context.Context, domain string) Result {
		return Result{Domain: domain, Status: StatusUnknown}
	})
	cache := &memoryCache{}
	checker := NewCachedChecker(next, cache, 24*time.Hour, 5*time.Minute)

	checker.Check(context.Background(), "unknown.example")
	if cache.setTTL != 5*time.Minute {
		t.Fatalf("unknown TTL = %s, want 5m", cache.setTTL)
	}
}

type CheckerFunc func(context.Context, string) Result

func (fn CheckerFunc) Check(ctx context.Context, domain string) Result {
	return fn(ctx, domain)
}
