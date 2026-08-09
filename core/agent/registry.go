package agent

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)

var (
	ErrToolAlreadyRegistered = errors.New("agent tool is already registered")
	ErrInvalidTool           = errors.New("invalid agent tool")
	ErrInvalidOwner          = errors.New("agent tool owner is required")
)

type registryEntry struct {
	owner      string
	tool       Tool
	generation uint64
}

// Registry is a concurrent, owner-aware catalog of protocol-neutral Tools.
type Registry struct {
	mu         sync.RWMutex
	tools      map[string]registryEntry
	revision   uint64
	generation atomic.Uint64
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]registryEntry)}
}

// Handle revokes exactly the registration that created it. A stale Handle can
// never remove a later Tool registered under the same name.
type Handle struct {
	registry   *Registry
	name       string
	owner      string
	generation uint64
	once       sync.Once
	revoked    bool
}

func (h *Handle) Revoke() bool {
	if h == nil || h.registry == nil {
		return false
	}
	h.once.Do(func() {
		h.revoked = h.registry.revoke(h.name, h.owner, h.generation)
	})
	return h.revoked
}

func (r *Registry) Register(owner string, tool Tool) (*Handle, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, ErrInvalidOwner
	}
	if err := validateTool(tool); err != nil {
		return nil, err
	}
	tool.InputSchema = append([]byte(nil), tool.InputSchema...)
	tool.OutputSchema = append([]byte(nil), tool.OutputSchema...)
	generation := r.generation.Add(1)

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[tool.Name]; exists {
		return nil, ErrToolAlreadyRegistered
	}
	r.tools[tool.Name] = registryEntry{owner: owner, tool: tool, generation: generation}
	r.revision++
	return &Handle{registry: r, name: tool.Name, owner: owner, generation: generation}, nil
}

func (r *Registry) get(name string) (RegisteredTool, bool) {
	if r == nil {
		return RegisteredTool{}, false
	}
	r.mu.RLock()
	entry, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return RegisteredTool{}, false
	}
	return executableRegisteredTool(entry), true
}

func (r *Registry) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	r.mu.RLock()
	revision := r.revision
	tools := make([]RegisteredTool, 0, len(r.tools))
	for _, entry := range r.tools {
		tools = append(tools, descriptorRegisteredTool(entry))
	}
	r.mu.RUnlock()
	sort.Slice(tools, func(i, j int) bool { return tools[i].Tool.Name < tools[j].Tool.Name })
	return Snapshot{Revision: revision, Tools: tools}
}

func (r *Registry) Revision() uint64 {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.revision
}

func (r *Registry) RevokeOwner(owner string) int {
	if r == nil || strings.TrimSpace(owner) == "" {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for name, entry := range r.tools {
		if entry.owner == owner {
			delete(r.tools, name)
			removed++
		}
	}
	if removed > 0 {
		r.revision++
	}
	return removed
}

func (r *Registry) revoke(name, owner string, generation uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tools[name]
	if !ok || entry.owner != owner || entry.generation != generation {
		return false
	}
	delete(r.tools, name)
	r.revision++
	return true
}

func executableRegisteredTool(entry registryEntry) RegisteredTool {
	tool := entry.tool
	tool.InputSchema = append([]byte(nil), tool.InputSchema...)
	tool.OutputSchema = append([]byte(nil), tool.OutputSchema...)
	return RegisteredTool{Owner: entry.owner, Tool: tool}
}

func descriptorRegisteredTool(entry registryEntry) RegisteredTool {
	registered := executableRegisteredTool(entry)
	registered.Tool.Handler = nil
	registered.Tool.ResolvePermission = nil
	return registered
}

func validateTool(tool Tool) error {
	if !toolNamePattern.MatchString(tool.Name) || strings.TrimSpace(tool.Title) == "" ||
		strings.TrimSpace(tool.Description) == "" || tool.Handler == nil {
		return ErrInvalidTool
	}
	if tool.Mutability != MutabilityRead && tool.Mutability != MutabilityWrite {
		return ErrInvalidTool
	}
	if tool.Risk.rank() > RiskCritical.rank() || tool.Risk.rank() < RiskRead.rank() {
		return ErrInvalidTool
	}
	if tool.Mutability == MutabilityRead && tool.Risk != RiskRead {
		return ErrInvalidTool
	}
	if tool.Mutability == MutabilityWrite && !tool.Idempotent {
		return ErrInvalidTool
	}
	if tool.RequiresConfirmation && tool.Mutability != MutabilityWrite {
		return ErrInvalidTool
	}
	if tool.Permission.Scope == "" || tool.Permission.Resource == "" || tool.Permission.Action == "" {
		return ErrInvalidTool
	}
	if err := ValidateSchemaDefinition(tool.InputSchema); err != nil {
		return err
	}
	if err := ValidateSchemaDefinition(tool.OutputSchema); err != nil {
		return err
	}
	return nil
}
