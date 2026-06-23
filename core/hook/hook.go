package hook

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
)

// AdminDashboardWidgets is the standard admin dashboard extension slot.
//
// Filters receive the accumulated template.HTML value and the dashboard
// template root as the first argument. Extensions should append their widget
// markup and return the same conceptual value type.
const AdminDashboardWidgets = "admin.dashboard.widgets"

// ActionFunc is a side-effect callback invoked by DoAction.
//
// Actions do not return values and are best suited for notifications such as
// "content saved", "menu deleted", or "engine initialized". If a caller needs
// callbacks to transform data, use a FilterFunc instead.
type ActionFunc func(ctx context.Context, args ...interface{})

// FilterFunc transforms a value as it moves through ApplyFilter.
//
// Each filter receives the output of the previous filter. Implementations
// should return a value of the same conceptual type that the hook contract
// documents; mismatched types are possible at compile time because filters are
// intentionally generic extension points.
type FilterFunc func(value interface{}, args ...interface{}) interface{}

// Handle is an opaque token returned from AddAction/AddFilter. Pass it to
// RemoveAction/RemoveFilter to unregister a callback — critical for plugin
// deactivation hygiene, since without it stale hooks would keep firing after
// a plugin is disabled. The zero Handle is safe to pass to Remove* (no-op).
type Handle struct {
	name string
	id   uint64
}

// IsZero reports whether h is the zero-value Handle (i.e. unset).
func (h Handle) IsZero() bool { return h.id == 0 && h.name == "" }

type actionEntry struct {
	fn       ActionFunc
	priority int
	id       uint64
}

type filterEntry struct {
	fn       FilterFunc
	priority int
	id       uint64
}

// Bus is the process-local action/filter event bus.
//
// Callbacks are ordered by ascending priority. The bus snapshots each callback
// slice before execution so callbacks can add or remove other callbacks without
// holding the lock during user/plugin code. Bus does not recover panics; plugin
// callbacks should fail explicitly and defensively.
type Bus struct {
	mu      sync.RWMutex
	actions map[string][]actionEntry
	filters map[string][]filterEntry
	nextID  uint64
}

// New creates an empty hook Bus.
func New() *Bus {
	return &Bus{
		actions: make(map[string][]actionEntry),
		filters: make(map[string][]filterEntry),
	}
}

// AddAction registers an action callback at the given priority (lower runs
// first). Returns a Handle that can be passed to RemoveAction to unregister.
// Callers that never need to unregister may ignore the return value.
func (b *Bus) AddAction(name string, fn ActionFunc, priority int) Handle {
	id := atomic.AddUint64(&b.nextID, 1)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.actions[name] = append(b.actions[name], actionEntry{fn: fn, priority: priority, id: id})
	sort.Slice(b.actions[name], func(i, j int) bool {
		return b.actions[name][i].priority < b.actions[name][j].priority
	})
	return Handle{name: name, id: id}
}

// RemoveAction unregisters an action previously added via AddAction.
// Passing a zero Handle is a no-op. Safe to call multiple times.
func (b *Bus) RemoveAction(h Handle) {
	if h.IsZero() {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	entries, ok := b.actions[h.name]
	if !ok {
		return
	}
	filtered := entries[:0]
	for _, e := range entries {
		if e.id != h.id {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		delete(b.actions, h.name)
	} else {
		b.actions[h.name] = filtered
	}
}

// DoAction executes all registered action callbacks for the named hook.
//
// Actions run synchronously in priority order. Long-running work should hand off
// to the worker pool rather than blocking the request goroutine that fired the
// hook.
func (b *Bus) DoAction(ctx context.Context, name string, args ...interface{}) {
	b.mu.RLock()
	entries := b.actions[name]
	b.mu.RUnlock()
	for _, e := range entries {
		e.fn(ctx, args...)
	}
}

// AddFilter registers a filter callback at the given priority. Returns a
// Handle that can be passed to RemoveFilter to unregister.
func (b *Bus) AddFilter(name string, fn FilterFunc, priority int) Handle {
	id := atomic.AddUint64(&b.nextID, 1)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.filters[name] = append(b.filters[name], filterEntry{fn: fn, priority: priority, id: id})
	sort.Slice(b.filters[name], func(i, j int) bool {
		return b.filters[name][i].priority < b.filters[name][j].priority
	})
	return Handle{name: name, id: id}
}

// RemoveFilter unregisters a filter previously added via AddFilter.
// Passing a zero Handle is a no-op. Safe to call multiple times.
func (b *Bus) RemoveFilter(h Handle) {
	if h.IsZero() {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	entries, ok := b.filters[h.name]
	if !ok {
		return
	}
	filtered := entries[:0]
	for _, e := range entries {
		if e.id != h.id {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		delete(b.filters, h.name)
	} else {
		b.filters[h.name] = filtered
	}
}

// ApplyFilter passes value through all registered filter callbacks sequentially.
//
// The returned value is the final result after every registered filter has run.
// If no filters are registered, the original value is returned unchanged.
func (b *Bus) ApplyFilter(name string, value interface{}, args ...interface{}) interface{} {
	b.mu.RLock()
	entries := b.filters[name]
	b.mu.RUnlock()
	for _, e := range entries {
		value = e.fn(value, args...)
	}
	return value
}
