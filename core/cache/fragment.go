package cache

import (
	"encoding/json"
	"time"
)

const fragmentCachePrefix = "frag:"

// Fragment provides helpers for caching partial HTML or data fragments.
// Themes and handlers can use this to cache expensive sidebar widgets,
// navigation menus, or other reusable page sections.
type Fragment struct {
	mgr *Manager
}

// NewFragment creates a fragment cache helper backed by the given Manager.
func NewFragment(mgr *Manager) *Fragment {
	return &Fragment{mgr: mgr}
}

// GetHTML retrieves a cached HTML fragment by key.
func (f *Fragment) GetHTML(key string) (string, bool) {
	data, ok := f.mgr.Get(fragmentCachePrefix + key)
	if !ok {
		return "", false
	}
	return string(data), true
}

// SetHTML caches an HTML fragment.
func (f *Fragment) SetHTML(key, html string, ttl time.Duration) {
	f.mgr.Set(fragmentCachePrefix+key, []byte(html), ttl)
}

// GetJSON retrieves and unmarshals a cached JSON fragment into dest.
func (f *Fragment) GetJSON(key string, dest interface{}) bool {
	data, ok := f.mgr.Get(fragmentCachePrefix + key)
	if !ok {
		return false
	}
	return json.Unmarshal(data, dest) == nil
}

// SetJSON marshals value to JSON and caches it.
func (f *Fragment) SetJSON(key string, value interface{}, ttl time.Duration) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	f.mgr.Set(fragmentCachePrefix+key, data, ttl)
}

// Delete removes a specific fragment.
func (f *Fragment) Delete(key string) {
	f.mgr.Delete(fragmentCachePrefix + key)
}

// DeleteByPrefix removes all fragments whose key starts with the given prefix.
func (f *Fragment) DeleteByPrefix(prefix string) {
	f.mgr.DeleteByPrefix(fragmentCachePrefix + prefix)
}

// Flush removes all fragment cache entries.
func (f *Fragment) Flush() {
	f.mgr.DeleteByPrefix(fragmentCachePrefix)
}
