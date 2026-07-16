package user

import (
	"errors"
	"net/url"
	"sort"
	"strings"
	"sync"
)

var ErrInvalidProvider = errors.New("invalid authentication provider")

// ProviderDescriptor is display metadata for a provider registered by an
// active plugin. BeginURL must be a same-site path handled by that plugin.
type ProviderDescriptor struct {
	ID       string
	Label    string
	BeginURL string
	IconURL  string
	Priority int
}

// ProviderRegistry is the core-owned discovery surface consumed by login UI.
type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]ProviderDescriptor
}

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{providers: make(map[string]ProviderDescriptor)}
}

func (r *ProviderRegistry) Register(provider ProviderDescriptor) error {
	provider.ID = strings.TrimSpace(provider.ID)
	provider.Label = strings.TrimSpace(provider.Label)
	provider.BeginURL = strings.TrimSpace(provider.BeginURL)
	if provider.ID == "" || provider.Label == "" || !isLocalProviderPath(provider.BeginURL) {
		return ErrInvalidProvider
	}
	r.mu.Lock()
	r.providers[provider.ID] = provider
	r.mu.Unlock()
	return nil
}

func (r *ProviderRegistry) Unregister(id string) {
	r.mu.Lock()
	delete(r.providers, strings.TrimSpace(id))
	r.mu.Unlock()
}

func (r *ProviderRegistry) Get(id string) (ProviderDescriptor, bool) {
	r.mu.RLock()
	provider, ok := r.providers[id]
	r.mu.RUnlock()
	return provider, ok
}

func (r *ProviderRegistry) All() []ProviderDescriptor {
	r.mu.RLock()
	providers := make([]ProviderDescriptor, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	r.mu.RUnlock()
	sort.Slice(providers, func(i, j int) bool {
		if providers[i].Priority != providers[j].Priority {
			return providers[i].Priority < providers[j].Priority
		}
		return providers[i].ID < providers[j].ID
	})
	return providers
}

func isLocalProviderPath(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "" && parsed.Host == "" && strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(parsed.Path, "//")
}
