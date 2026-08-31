package availability

import (
	"context"
	"strings"
	"time"
)

type FallbackChecker struct{ checkers []Checker }

func NewFallbackChecker(checkers ...Checker) *FallbackChecker {
	return &FallbackChecker{checkers: checkers}
}

func (c *FallbackChecker) Check(ctx context.Context, domain string) Result {
	started := time.Now()
	var messages []string
	result := Result{Domain: domain, Status: StatusUnknown}
	for _, checker := range c.checkers {
		candidate := checker.Check(ctx, domain)
		if candidate.Status == StatusAvailable || candidate.Status == StatusRegistered {
			candidate.DurationMS = time.Since(started).Milliseconds()
			return candidate
		}
		result = candidate
		if candidate.Message != "" {
			messages = append(messages, candidate.Provider+": "+candidate.Message)
		}
		if ctx.Err() != nil {
			break
		}
	}
	result.Domain = domain
	result.Status = StatusUnknown
	result.DurationMS = time.Since(started).Milliseconds()
	if len(messages) > 0 {
		result.Message = "所有检测源均未返回确定结果；" + strings.Join(messages, "；")
	}
	return result
}
