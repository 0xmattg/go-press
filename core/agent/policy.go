package agent

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// ToolProfile is the site-wide ceiling for Agent tool execution. Read-only is
// deliberately the zero-value/default profile; write tools additionally need
// an explicit per-tool grant.
type ToolProfile string

const (
	ProfileReadOnly  ToolProfile = "read_only"
	ProfileSafeWrite ToolProfile = "safe_write"

	OptionToolProfile       = "agent_tool_profile"
	OptionEnabledWriteTools = "agent_enabled_write_tools"
)

var ErrInvalidToolProfile = errors.New("invalid agent tool profile")

type PolicySnapshot struct {
	Profile           ToolProfile `json:"profile"`
	EnabledWriteTools []string    `json:"enabled_write_tools"`
	Revision          uint64      `json:"revision"`
}

// Policy is a protocol-neutral, concurrency-safe RiskPolicy. Protocol plugins
// may provide a settings UI, but Core remains the authority that enforces it.
type Policy struct {
	mu       sync.RWMutex
	profile  ToolProfile
	enabled  map[string]struct{}
	revision uint64
}

func NewPolicy() *Policy {
	return &Policy{profile: ProfileReadOnly, enabled: make(map[string]struct{}), revision: 1}
}

func (p *Policy) Configure(profile ToolProfile, enabled []string) error {
	if p == nil {
		return ErrInvalidToolProfile
	}
	if profile != ProfileReadOnly && profile != ProfileSafeWrite {
		return ErrInvalidToolProfile
	}
	next := make(map[string]struct{})
	if profile == ProfileSafeWrite {
		for _, name := range enabled {
			name = strings.TrimSpace(name)
			if !toolNamePattern.MatchString(name) {
				return ErrInvalidToolProfile
			}
			next[name] = struct{}{}
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.profile == profile && sameToolSet(p.enabled, next) {
		return nil
	}
	p.profile = profile
	p.enabled = next
	p.revision++
	return nil
}

func (p *Policy) Allow(_ Principal, tool Tool) bool {
	if tool.Mutability == MutabilityRead {
		return true
	}
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.profile != ProfileSafeWrite {
		return false
	}
	_, allowed := p.enabled[tool.Name]
	return allowed
}

func (p *Policy) Revision() uint64 {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.revision
}

func (p *Policy) Snapshot() PolicySnapshot {
	if p == nil {
		return PolicySnapshot{Profile: ProfileReadOnly}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	enabled := make([]string, 0, len(p.enabled))
	for name := range p.enabled {
		enabled = append(enabled, name)
	}
	sort.Strings(enabled)
	return PolicySnapshot{Profile: p.profile, EnabledWriteTools: enabled, Revision: p.revision}
}

func sameToolSet(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for name := range left {
		if _, ok := right[name]; !ok {
			return false
		}
	}
	return true
}
