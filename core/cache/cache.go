package cache

import "time"

// Cache is the common interface implemented by all cache backends.
//
// Values are byte slices so callers own serialization. Backends should treat
// ttl <= 0 as "use backend default or no expiration" only if their concrete
// implementation documents that behavior.
type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, value []byte, ttl time.Duration)
	Delete(key string)
	DeleteByPrefix(prefix string)
	Flush()
}

// Manager orchestrates GoPress's multi-level cache path.
//
// Reads check L1 first and L2 second. L2 hits are promoted back into L1 with a
// short TTL to reduce repeated distributed-cache reads. Either level may be a
// no-op cache, allowing the same code path in local development, production
// without Redis, and production with Redis.
type Manager struct {
	L1 Cache // process-local LRU (ristretto), may be noopCache
	L2 Cache // distributed cache (Redis), may be noopCache
}

// NewManager creates a cache manager. Nil levels are replaced with noopCache.
func NewManager(l1, l2 Cache) *Manager {
	if l1 == nil {
		l1 = &noopCache{}
	}
	if l2 == nil {
		l2 = &noopCache{}
	}
	return &Manager{L1: l1, L2: l2}
}

// NewNoopManager returns a manager where both levels are noop (no caching).
func NewNoopManager() *Manager {
	return &Manager{L1: &noopCache{}, L2: &noopCache{}}
}

// Get looks up key in L1 first, then L2.
//
// If L2 contains the value, Manager promotes it into L1 for five minutes before
// returning it. This keeps Redis-backed deployments fast while limiting stale
// in-process values after invalidation by prefix.
func (m *Manager) Get(key string) ([]byte, bool) {
	// Try L1
	if v, ok := m.L1.Get(key); ok {
		return v, true
	}
	// Try L2
	if v, ok := m.L2.Get(key); ok {
		// Promote to L1 with a shorter TTL
		m.L1.Set(key, v, 5*time.Minute)
		return v, true
	}
	return nil, false
}

// Set writes to both L1 and L2.
func (m *Manager) Set(key string, value []byte, ttl time.Duration) {
	m.L1.Set(key, value, ttl)
	m.L2.Set(key, value, ttl)
}

// Delete removes from both levels.
func (m *Manager) Delete(key string) {
	m.L1.Delete(key)
	m.L2.Delete(key)
}

// DeleteByPrefix removes all keys matching a prefix from both levels.
func (m *Manager) DeleteByPrefix(prefix string) {
	m.L1.DeleteByPrefix(prefix)
	m.L2.DeleteByPrefix(prefix)
}

// Flush clears all caches.
func (m *Manager) Flush() {
	m.L1.Flush()
	m.L2.Flush()
}

// IsNoop returns true if both levels are noop (no real caching).
func (m *Manager) IsNoop() bool {
	_, l1Noop := m.L1.(*noopCache)
	_, l2Noop := m.L2.(*noopCache)
	return l1Noop && l2Noop
}

// noopCache is a do-nothing cache used as a fallback when no backend is configured.
type noopCache struct{}

func (n *noopCache) Get(string) ([]byte, bool)         { return nil, false }
func (n *noopCache) Set(string, []byte, time.Duration) {}
func (n *noopCache) Delete(string)                     {}
func (n *noopCache) DeleteByPrefix(string)             {}
func (n *noopCache) Flush()                            {}
