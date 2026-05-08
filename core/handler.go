package core

import (
	"net/http"
	"sync"
)

// HandlerSwitcher is a thread-safe HTTP handler that can be swapped at runtime.
// It is used by the server entry point to switch between installer and application mode,
// and to update the handler after theme hot-switch triggers a router rebuild.
type HandlerSwitcher struct {
	mu      sync.RWMutex
	handler http.Handler
}

// Set replaces the active handler. Safe for concurrent use.
func (h *HandlerSwitcher) Set(next http.Handler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handler = next
}

// ServeHTTP dispatches the request to the current handler.
func (h *HandlerSwitcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	handler := h.handler
	h.mu.RUnlock()

	if handler == nil {
		http.Error(w, "handler not ready", http.StatusServiceUnavailable)
		return
	}

	handler.ServeHTTP(w, r)
}
