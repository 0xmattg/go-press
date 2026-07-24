package cache

import (
	"strconv"
	"testing"
	"time"
)

func TestMemoryCacheGetSet(t *testing.T) {
	c := NewMemoryCache(0)
	c.Set("a", []byte("1"), time.Minute)
	if v, ok := c.Get("a"); !ok || string(v) != "1" {
		t.Fatalf("Get(a) = %q, %v", v, ok)
	}
	if _, ok := c.Get("missing"); ok {
		t.Fatal("missing key should be a miss")
	}
}

func TestMemoryCacheReturnsCopy(t *testing.T) {
	c := NewMemoryCache(0)
	c.Set("a", []byte("abc"), time.Minute)
	v, _ := c.Get("a")
	v[0] = 'X' // mutate the returned copy
	again, _ := c.Get("a")
	if string(again) != "abc" {
		t.Fatalf("stored value was mutated through returned slice: %q", again)
	}
}

func TestMemoryCacheTTLExpiry(t *testing.T) {
	c := NewMemoryCache(0)
	c.Set("a", []byte("1"), time.Nanosecond)
	time.Sleep(2 * time.Millisecond)
	if _, ok := c.Get("a"); ok {
		t.Fatal("expired entry should not be returned")
	}
}

func TestMemoryCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := NewMemoryCache(3)
	c.Set("a", []byte("1"), time.Minute)
	c.Set("b", []byte("2"), time.Minute)
	c.Set("c", []byte("3"), time.Minute)

	// Touch "a" so "b" becomes the least-recently-used entry.
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should be present")
	}

	// Inserting "d" exceeds capacity and must evict "b" (the LRU), not "a".
	c.Set("d", []byte("4"), time.Minute)

	if _, ok := c.Get("b"); ok {
		t.Fatal("b should have been evicted as least-recently-used")
	}
	for _, k := range []string{"a", "c", "d"} {
		if _, ok := c.Get(k); !ok {
			t.Fatalf("%q should still be cached", k)
		}
	}
}

func TestMemoryCacheRespectsCapacity(t *testing.T) {
	const max = 50
	c := NewMemoryCache(max)
	for i := 0; i < max*3; i++ {
		c.Set("k"+strconv.Itoa(i), []byte("v"), time.Minute)
	}
	if got := c.ll.Len(); got > max {
		t.Fatalf("cache holds %d entries, want <= %d", got, max)
	}
	if got := len(c.items); got > max {
		t.Fatalf("index holds %d entries, want <= %d", got, max)
	}
}

func TestMemoryCacheDeleteByPrefix(t *testing.T) {
	c := NewMemoryCache(0)
	c.Set("page:/a", []byte("1"), time.Minute)
	c.Set("page:/b", []byte("2"), time.Minute)
	c.Set("frag:/c", []byte("3"), time.Minute)
	c.DeleteByPrefix("page:")
	if _, ok := c.Get("page:/a"); ok {
		t.Fatal("page:/a should be gone")
	}
	if _, ok := c.Get("frag:/c"); !ok {
		t.Fatal("frag:/c should remain")
	}
}
