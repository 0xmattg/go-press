package commerce

import (
	"testing"
	"time"
)

func TestAttemptThrottle(t *testing.T) {
	now := time.Unix(0, 0)
	th := newAttemptThrottle(3, time.Minute)
	th.now = func() time.Time { return now }

	// First 3 attempts in the window are allowed; the 4th is blocked.
	for i := 1; i <= 3; i++ {
		if !th.allow("1.2.3.4") {
			t.Fatalf("attempt %d should be allowed", i)
		}
	}
	if th.allow("1.2.3.4") {
		t.Fatal("4th attempt should be blocked within the window")
	}

	// A different key has its own budget.
	if !th.allow("5.6.7.8") {
		t.Fatal("a different IP should be allowed independently")
	}

	// After the window rolls over, the budget resets.
	now = now.Add(time.Minute + time.Second)
	if !th.allow("1.2.3.4") {
		t.Fatal("attempt after window rollover should be allowed")
	}

	// Empty key fails open (never blocks).
	for i := 0; i < 10; i++ {
		if !th.allow("") {
			t.Fatal("empty key should always be allowed")
		}
	}
}
