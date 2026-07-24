package admin

import (
	"testing"
	"time"
)

func TestLoginThrottleBlocksAfterMaxFailures(t *testing.T) {
	now := time.Now()
	tr := newLoginThrottle(3, 5*time.Minute)
	tr.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if tr.blocked("1.2.3.4") {
			t.Fatalf("blocked too early on attempt %d", i)
		}
		tr.fail("1.2.3.4")
	}
	if !tr.blocked("1.2.3.4") {
		t.Fatal("expected key to be blocked after reaching the failure budget")
	}
	// A different IP is unaffected.
	if tr.blocked("5.6.7.8") {
		t.Fatal("unrelated key must not be blocked")
	}
}

func TestLoginThrottleResetOnSuccess(t *testing.T) {
	tr := newLoginThrottle(2, 5*time.Minute)
	tr.fail("1.2.3.4")
	tr.fail("1.2.3.4")
	if !tr.blocked("1.2.3.4") {
		t.Fatal("should be blocked")
	}
	tr.reset("1.2.3.4")
	if tr.blocked("1.2.3.4") {
		t.Fatal("reset should clear the failure counter")
	}
}

func TestLoginThrottleWindowExpiry(t *testing.T) {
	now := time.Now()
	tr := newLoginThrottle(1, 1*time.Minute)
	tr.now = func() time.Time { return now }
	tr.fail("1.2.3.4")
	if !tr.blocked("1.2.3.4") {
		t.Fatal("should be blocked within window")
	}
	now = now.Add(2 * time.Minute)
	if tr.blocked("1.2.3.4") {
		t.Fatal("window should have expired")
	}
}

func TestLoginThrottleNilSafe(t *testing.T) {
	var tr *loginThrottle
	if tr.blocked("x") {
		t.Fatal("nil throttle must not block")
	}
	tr.fail("x") // must not panic
	tr.reset("x")
}
