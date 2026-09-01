package availability

import (
	"context"
	"log/slog"
	"time"
)

// Cache stores availability results by domain name.
type Cache interface {
	Get(context.Context, string) (Result, bool, error)
	Set(context.Context, string, Result, time.Duration) error
}

// CachedChecker decorates another Checker with a read-through cache.
type CachedChecker struct {
	next       Checker
	cache      Cache
	resultTTL  time.Duration
	unknownTTL time.Duration
}

// NewCachedChecker creates a read-through caching checker.
func NewCachedChecker(next Checker, cache Cache, resultTTL, unknownTTL time.Duration) *CachedChecker {
	return &CachedChecker{next: next, cache: cache, resultTTL: resultTTL, unknownTTL: unknownTTL}
}

func (c *CachedChecker) Check(ctx context.Context, domain string) Result {
	started := time.Now()
	if ctx.Err() != nil {
		return Result{Domain: domain, Status: StatusUnknown, Message: "查询已取消"}
	}
	if cached, found, err := c.cache.Get(ctx, domain); err == nil && found {
		cached.Cached = true
		cached.DurationMS = time.Since(started).Milliseconds()
		return cached
	} else if err != nil {
		if ctx.Err() != nil {
			return Result{Domain: domain, Status: StatusUnknown, Message: "查询已取消"}
		}
		slog.Warn("读取域名缓存失败，回退到 RDAP", "domain", domain, "error", err)
	}

	result := c.next.Check(ctx, domain)
	if ctx.Err() != nil {
		return result
	}
	ttl := c.resultTTL
	if result.Status == StatusUnknown {
		ttl = c.unknownTTL
	}
	if err := c.cache.Set(ctx, domain, result, ttl); err != nil && ctx.Err() == nil {
		slog.Warn("写入域名缓存失败", "domain", domain, "error", err)
	}
	return result
}
