package cache

import (
	"container/list"
	"strings"
	"sync"
	"time"
)

// MemoryCache is an in-process LRU cache with per-entry TTL.
//
// It keeps entries in a doubly linked list ordered by recency (front = most
// recently used) alongside a key→element index, giving O(1) Get, Set, Delete,
// and eviction. When the entry count exceeds maxSize the least-recently-used
// entry is evicted; maxSize == 0 disables the size bound. A background sweeper
// removes expired entries so idle keys do not linger.
type MemoryCache struct {
	mu      sync.Mutex // LRU updates the list on Get, so reads mutate state
	maxSize int
	ll      *list.List               // front = most recently used
	items   map[string]*list.Element // key -> element holding *memEntry
}

type memEntry struct {
	key       string
	data      []byte
	expiresAt time.Time
}

// NewMemoryCache creates a new in-process LRU cache.
// maxSize limits the number of entries; 0 means no limit.
func NewMemoryCache(maxSize int) *MemoryCache {
	mc := &MemoryCache{
		maxSize: maxSize,
		ll:      list.New(),
		items:   make(map[string]*list.Element),
	}
	// Start background cleanup goroutine
	go mc.cleanup()
	return mc
}

func (m *MemoryCache) Get(key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.items[key]
	if !ok {
		return nil, false
	}
	ent := el.Value.(*memEntry)
	if time.Now().After(ent.expiresAt) {
		m.removeElement(el)
		return nil, false
	}
	m.ll.MoveToFront(el)
	// Return a copy to prevent mutation
	cp := make([]byte, len(ent.data))
	copy(cp, ent.data)
	return cp, true
}

func (m *MemoryCache) Set(key string, value []byte, ttl time.Duration) {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	expiresAt := time.Now().Add(ttl)

	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[key]; ok {
		ent := el.Value.(*memEntry)
		ent.data = cp
		ent.expiresAt = expiresAt
		m.ll.MoveToFront(el)
		return
	}
	el := m.ll.PushFront(&memEntry{key: key, data: cp, expiresAt: expiresAt})
	m.items[key] = el
	if m.maxSize > 0 && m.ll.Len() > m.maxSize {
		m.evictLRU()
	}
}

func (m *MemoryCache) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.items[key]; ok {
		m.removeElement(el)
	}
}

func (m *MemoryCache) DeleteByPrefix(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, el := range m.items {
		if strings.HasPrefix(k, prefix) {
			m.removeElement(el)
		}
	}
}

func (m *MemoryCache) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ll.Init()
	m.items = make(map[string]*list.Element)
}

// evictLRU removes the least-recently-used entry (list back). Caller holds the lock.
func (m *MemoryCache) evictLRU() {
	if el := m.ll.Back(); el != nil {
		m.removeElement(el)
	}
}

// removeElement unlinks an element from both the recency list and the index.
// Caller holds the lock.
func (m *MemoryCache) removeElement(el *list.Element) {
	m.ll.Remove(el)
	delete(m.items, el.Value.(*memEntry).key)
}

// cleanup runs periodically to remove expired entries.
func (m *MemoryCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		now := time.Now()
		m.mu.Lock()
		for _, el := range m.items {
			if now.After(el.Value.(*memEntry).expiresAt) {
				m.removeElement(el)
			}
		}
		m.mu.Unlock()
	}
}
