package admin

import (
	"sync"
	"time"
)

// loginThrottle slows down online password brute-force / credential-stuffing
// against the admin console by capping failed login attempts per client key
// (IP) within a sliding window. It is intentionally in-memory and best-effort:
// it complements, not replaces, strong passwords and bcrypt hashing.
type loginThrottle struct {
	mu       sync.Mutex
	failures map[string]*loginFailureWindow
	max      int
	window   time.Duration
	now      func() time.Time
}

type loginFailureWindow struct {
	count       int
	windowStart time.Time
}

// newLoginThrottle allows up to max failures per window before blocking.
func newLoginThrottle(max int, window time.Duration) *loginThrottle {
	return &loginThrottle{
		failures: make(map[string]*loginFailureWindow),
		max:      max,
		window:   window,
		now:      time.Now,
	}
}

// blocked reports whether the key has exhausted its failure budget for the
// current window. Expired windows are cleared lazily.
func (t *loginThrottle) blocked(key string) bool {
	if t == nil || key == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	w := t.failures[key]
	if w == nil {
		return false
	}
	if t.now().Sub(w.windowStart) >= t.window {
		delete(t.failures, key)
		return false
	}
	return w.count >= t.max
}

// fail records one failed attempt, starting a fresh window when needed.
func (t *loginThrottle) fail(key string) {
	if t == nil || key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if len(t.failures) > 4096 {
		t.pruneLocked(now)
	}
	w := t.failures[key]
	if w == nil || now.Sub(w.windowStart) >= t.window {
		t.failures[key] = &loginFailureWindow{count: 1, windowStart: now}
		return
	}
	w.count++
}

// reset clears the failure counter after a successful login.
func (t *loginThrottle) reset(key string) {
	if t == nil || key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, key)
}

// pruneLocked drops expired windows. Caller must hold the mutex.
func (t *loginThrottle) pruneLocked(now time.Time) {
	for key, w := range t.failures {
		if now.Sub(w.windowStart) >= t.window {
			delete(t.failures, key)
		}
	}
}
