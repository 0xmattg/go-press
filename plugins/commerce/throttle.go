package commerce

import (
	"sync"
	"time"
)

// attemptThrottle caps how many times a client key (IP) may hit a sensitive
// endpoint within a sliding window. It is in-memory and best-effort: it blunts
// brute-force enumeration of the /order-tracking lookup, complementing — not
// replacing — the order-number + email two-factor check itself.
type attemptThrottle struct {
	mu     sync.Mutex
	hits   map[string]*attemptWindow
	max    int
	window time.Duration
	now    func() time.Time // injectable for tests
}

type attemptWindow struct {
	count       int
	windowStart time.Time
}

// newAttemptThrottle permits up to max attempts per key within window.
func newAttemptThrottle(max int, window time.Duration) *attemptThrottle {
	return &attemptThrottle{
		hits:   make(map[string]*attemptWindow),
		max:    max,
		window: window,
		now:    time.Now,
	}
}

// allow records one attempt for key and reports whether it is within budget.
// The first attempt in a fresh window is allowed; the (max+1)-th is blocked
// until the window rolls over. A nil throttle or empty key fails open.
func (t *attemptThrottle) allow(key string) bool {
	if t == nil || key == "" {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	w := t.hits[key]
	if w == nil || now.Sub(w.windowStart) >= t.window {
		t.hits[key] = &attemptWindow{count: 1, windowStart: now}
		t.pruneLocked(now)
		return true
	}
	w.count++
	return w.count <= t.max
}

// pruneLocked drops expired windows so the map cannot grow unbounded.
func (t *attemptThrottle) pruneLocked(now time.Time) {
	for k, w := range t.hits {
		if now.Sub(w.windowStart) >= t.window {
			delete(t.hits, k)
		}
	}
}
