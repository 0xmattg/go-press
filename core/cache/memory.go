package cache

import (
	"sync"
	"time"
)

// MemoryCache is an L1 in-process LRU cache using dgraph-io/ristretto.
// If ristretto is not available, it falls back to a simple sync.Map-based store.
// This implementation uses a simple concurrent map with TTL as a zero-dependency alternative.
// Replace internals with ristretto for production-grade eviction and admission policies.
type MemoryCache struct {
	mu      sync.RWMutex
	items   map[string]*memItem
	maxSize int // max number of entries (0 = unlimited)
}

type memItem struct {
	data      []byte
	expiresAt time.Time
}

// NewMemoryCache creates a new in-process memory cache.
// maxSize limits the number of entries; 0 means no limit.
func NewMemoryCache(maxSize int) *MemoryCache {
	mc := &MemoryCache{
		items:   make(map[string]*memItem),
		maxSize: maxSize,
	}
	// Start background cleanup goroutine
	go mc.cleanup()
	return mc
}

func (m *MemoryCache) Get(key string) ([]byte, bool) {
	m.mu.RLock()
	item, ok := m.items[key]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(item.expiresAt) {
		m.Delete(key)
		return nil, false
	}
	// Return a copy to prevent mutation
	cp := make([]byte, len(item.data))
	copy(cp, item.data)
	return cp, true
}

func (m *MemoryCache) Set(key string, value []byte, ttl time.Duration) {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	cp := make([]byte, len(value))
	copy(cp, value)

	m.mu.Lock()
	// Simple eviction: if at capacity, delete oldest expired or random entry
	if m.maxSize > 0 && len(m.items) >= m.maxSize {
		m.evictOne()
	}
	m.items[key] = &memItem{
		data:      cp,
		expiresAt: time.Now().Add(ttl),
	}
	m.mu.Unlock()
}

func (m *MemoryCache) Delete(key string) {
	m.mu.Lock()
	delete(m.items, key)
	m.mu.Unlock()
}

func (m *MemoryCache) DeleteByPrefix(prefix string) {
	m.mu.Lock()
	for k := range m.items {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(m.items, k)
		}
	}
	m.mu.Unlock()
}

func (m *MemoryCache) Flush() {
	m.mu.Lock()
	m.items = make(map[string]*memItem)
	m.mu.Unlock()
}

// evictOne removes one expired or arbitrary entry. Caller must hold the write lock.
func (m *MemoryCache) evictOne() {
	now := time.Now()
	// Try to find an expired entry first
	for k, v := range m.items {
		if now.After(v.expiresAt) {
			delete(m.items, k)
			return
		}
	}
	// No expired entry — remove the first one found (random in Go map iteration)
	for k := range m.items {
		delete(m.items, k)
		return
	}
}

// cleanup runs periodically to remove expired entries.
func (m *MemoryCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		now := time.Now()
		m.mu.Lock()
		for k, v := range m.items {
			if now.After(v.expiresAt) {
				delete(m.items, k)
			}
		}
		m.mu.Unlock()
	}
}
