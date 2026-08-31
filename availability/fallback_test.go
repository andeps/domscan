package availability

import (
	"context"
	"testing"
)

func TestFallbackCheckerUsesNextSourceAfterUnknown(t *testing.T) {
	firstCalls, secondCalls := 0, 0
	first := CheckerFunc(func(_ context.Context, domain string) Result {
		firstCalls++
		return Result{Domain: domain, Status: StatusUnknown, Provider: "first", Message: "rate limited"}
	})
	second := CheckerFunc(func(_ context.Context, domain string) Result {
		secondCalls++
		return Result{Domain: domain, Status: StatusAvailable, Provider: "second"}
	})

	result := NewFallbackChecker(first, second).Check(context.Background(), "free.example")
	if result.Status != StatusAvailable || result.Provider != "second" || firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("unexpected fallback result: %+v, calls=%d/%d", result, firstCalls, secondCalls)
	}
}

func TestFallbackCheckerStopsAfterDefinitiveResult(t *testing.T) {
	secondCalls := 0
	first := CheckerFunc(func(_ context.Context, domain string) Result {
		return Result{Domain: domain, Status: StatusRegistered, Provider: "first"}
	})
	second := CheckerFunc(func(_ context.Context, domain string) Result {
		secondCalls++
		return Result{Domain: domain, Status: StatusAvailable, Provider: "second"}
	})

	result := NewFallbackChecker(first, second).Check(context.Background(), "taken.example")
	if result.Status != StatusRegistered || secondCalls != 0 {
		t.Fatalf("unexpected result: %+v, second calls=%d", result, secondCalls)
	}
}
