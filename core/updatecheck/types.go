// Package updatecheck provides the generic GoPress update-check protocol and
// target registry. Core, themes, and plugins can register independently
// versioned targets without depending on one another.
package updatecheck

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/0xmattg/go-press/pkg/semver"
)

const (
	ProtocolVersion = 1

	// CurrentPolicyVersion identifies the installer notice accepted by new
	// installations. It is sent for operational auditing, not authorization.
	CurrentPolicyVersion = "beta-1"

	DefaultCoreEndpoint = "https://gopress.xyz/api/official/v1/updates/check"
	OfficialReleasesURL = "https://github.com/0xmattg/go-press/releases"

	KeyInstallationID       = "system.update_installation_id"
	KeyPolicyVersion        = "system.update_policy_version"
	KeyPolicyAcceptedAt     = "system.update_policy_accepted_at"
	KeyPersistedCheckState  = "system.update_check_state"
	maxTargetParameters     = 16
	maxParameterKeyLength   = 64
	maxParameterValueLength = 256
)

type Kind string

const (
	KindCore   Kind = "core"
	KindTheme  Kind = "theme"
	KindPlugin Kind = "plugin"
)

// Target describes one independently versioned module checked against an
// update endpoint. Parameters are deliberately constrained string metadata so
// extension-specific protocols cannot turn the shared client into an
// unbounded arbitrary JSON transport.
type Target struct {
	Kind           Kind              `json:"kind"`
	Slug           string            `json:"slug"`
	CurrentVersion string            `json:"current_version"`
	Channel        string            `json:"channel,omitempty"`
	Endpoint       string            `json:"-"`
	Parameters     map[string]string `json:"parameters,omitempty"`
	// ReportInstallation opts this endpoint into receiving the official
	// installation envelope. Third-party theme/plugin targets default to false.
	ReportInstallation bool `json:"-"`
}

func (t Target) Key() string {
	return string(t.Kind) + ":" + t.Slug
}

type Instance struct {
	ID            string `json:"id"`
	PolicyVersion string `json:"policy_version"`
}

type Request struct {
	ProtocolVersion int       `json:"protocol_version"`
	Instance        *Instance `json:"instance,omitempty"`
	Targets         []Target  `json:"targets"`
}

type Update struct {
	Kind                    Kind   `json:"kind"`
	Slug                    string `json:"slug"`
	LatestVersion           string `json:"latest_version"`
	MinimumSupportedVersion string `json:"minimum_supported_version,omitempty"`
	Severity                string `json:"severity,omitempty"`
	ReleaseURL              string `json:"release_url,omitempty"`
	ReleasedAt              string `json:"released_at,omitempty"`
}

func (u Update) Key() string {
	return string(u.Kind) + ":" + u.Slug
}

type Response struct {
	ProtocolVersion int      `json:"protocol_version"`
	TTLSeconds      int      `json:"ttl_seconds"`
	Updates         []Update `json:"updates"`
}

// Status is a cached, read-only result suitable for admin UI consumers.
type Status struct {
	Kind                    Kind      `json:"kind"`
	Slug                    string    `json:"slug"`
	CurrentVersion          string    `json:"current_version"`
	LatestVersion           string    `json:"latest_version"`
	MinimumSupportedVersion string    `json:"minimum_supported_version,omitempty"`
	Severity                string    `json:"severity,omitempty"`
	ReleaseURL              string    `json:"release_url,omitempty"`
	ReleasedAt              string    `json:"released_at,omitempty"`
	CheckedAt               time.Time `json:"checked_at"`
	HasUpdate               bool      `json:"has_update"`
}

// Registry stores normalized update targets and is safe for extension
// registration while the scheduler is already running.
type Registry struct {
	mu      sync.RWMutex
	targets map[string]Target
}

func NewRegistry() *Registry {
	return &Registry{targets: make(map[string]Target)}
}

func (r *Registry) Register(target Target) error {
	if r == nil {
		return fmt.Errorf("update registry is nil")
	}
	normalized, err := normalizeTarget(target)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.targets[normalized.Key()] = normalized
	r.mu.Unlock()
	return nil
}

func (r *Registry) Unregister(kind Kind, slug string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.targets, string(kind)+":"+strings.TrimSpace(slug))
	r.mu.Unlock()
}

func (r *Registry) Get(kind Kind, slug string) (Target, bool) {
	if r == nil {
		return Target{}, false
	}
	r.mu.RLock()
	target, ok := r.targets[string(kind)+":"+strings.TrimSpace(slug)]
	r.mu.RUnlock()
	if !ok {
		return Target{}, false
	}
	target.Parameters = cloneParameters(target.Parameters)
	return target, true
}

func (r *Registry) All() []Target {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	out := make([]Target, 0, len(r.targets))
	for _, target := range r.targets {
		copyTarget := target
		copyTarget.Parameters = cloneParameters(target.Parameters)
		out = append(out, copyTarget)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

func normalizeTarget(target Target) (Target, error) {
	target.Slug = strings.TrimSpace(target.Slug)
	target.CurrentVersion = strings.TrimSpace(target.CurrentVersion)
	target.Channel = strings.TrimSpace(target.Channel)
	target.Endpoint = strings.TrimSpace(target.Endpoint)
	if target.Kind != KindCore && target.Kind != KindTheme && target.Kind != KindPlugin {
		return Target{}, fmt.Errorf("unsupported update target kind %q", target.Kind)
	}
	if target.Slug == "" || len(target.Slug) > 100 {
		return Target{}, fmt.Errorf("invalid update target slug")
	}
	if !semver.Valid(target.CurrentVersion) {
		return Target{}, fmt.Errorf("invalid current version %q", target.CurrentVersion)
	}
	endpoint, err := url.Parse(target.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" || endpoint.User != nil {
		return Target{}, fmt.Errorf("invalid update endpoint %q", target.Endpoint)
	}
	if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && isLoopbackHost(endpoint.Hostname())) {
		return Target{}, fmt.Errorf("update endpoint must use HTTPS")
	}
	if len(target.Parameters) > maxTargetParameters {
		return Target{}, fmt.Errorf("too many update target parameters")
	}
	params := make(map[string]string, len(target.Parameters))
	for key, value := range target.Parameters {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || len(key) > maxParameterKeyLength || len(value) > maxParameterValueLength {
			return Target{}, fmt.Errorf("invalid update target parameter")
		}
		params[key] = value
	}
	target.Parameters = params
	return target, nil
}

func cloneParameters(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
