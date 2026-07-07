package content

import (
	"errors"
	"testing"
)

func TestEvaluateContactMessageRateLimitAllowsWithinLimits(t *testing.T) {
	decision := EvaluateContactMessageRateLimit(3, 5)
	if !decision.Allowed {
		t.Fatal("expected contact message to be allowed below both limits")
	}
}

func TestEvaluateContactMessageRateLimitRejectsTwelveHourLimit(t *testing.T) {
	decision := EvaluateContactMessageRateLimit(ContactMessageLimit12Hours, 5)
	if decision.Allowed {
		t.Fatal("expected contact message to be rejected at the 12 hour limit")
	}
}

func TestEvaluateContactMessageRateLimitRejectsDailyLimit(t *testing.T) {
	decision := EvaluateContactMessageRateLimit(1, ContactMessageLimit24Hours)
	if decision.Allowed {
		t.Fatal("expected contact message to be rejected at the 24 hour limit")
	}
}

func TestRateLimitedErrorIsStable(t *testing.T) {
	if !errors.Is(ErrContactMessageRateLimited, ErrContactMessageRateLimited) {
		t.Fatal("rate limit error should be usable with errors.Is")
	}
}
