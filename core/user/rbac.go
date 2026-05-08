package user

import "sync"

// Permission represents an action on a resource.
type Permission struct {
	Resource string
	Action   string
}

// RoleDef defines role capabilities.
type RoleDef struct {
	Name        string
	DisplayName string
	Level       int
	Caps        map[string]bool // "resource.action" => true
}

// RBAC manages role-based access control.
type RBAC struct {
	mu    sync.RWMutex
	roles map[string]*RoleDef
}

// NewRBAC creates a new RBAC manager with default WordPress-style roles.
func NewRBAC() *RBAC {
	r := &RBAC{
		roles: make(map[string]*RoleDef),
	}
	r.registerDefaults()
	return r
}

func (r *RBAC) registerDefaults() {
	r.RegisterRole(RoleSuperAdmin, "超级管理员", 100, map[string]bool{
		"*.*": true,
	})
	r.RegisterRole(RoleEditor, "编辑", 50, map[string]bool{
		"content.create": true, "content.read": true, "content.update": true, "content.delete": true,
		"taxonomy.create": true, "taxonomy.read": true, "taxonomy.update": true, "taxonomy.delete": true,
		"media.create": true, "media.read": true, "media.update": true, "media.delete": true,
		"menu.read": true, "menu.update": true,
		"user.read": true,
		"comment.moderate": true,
		"dashboard.read":   true,
	})
	r.RegisterRole(RoleAuthor, "作者", 30, map[string]bool{
		"content.create": true, "content.read": true, "content.update_own": true, "content.delete_own": true,
		"taxonomy.read": true,
		"media.create":  true, "media.read": true, "media.update_own": true,
		"dashboard.read": true,
	})
	r.RegisterRole(RoleContributor, "投稿者", 20, map[string]bool{
		"content.create": true, "content.read": true, "content.update_own": true,
		"taxonomy.read":  true,
		"dashboard.read": true,
	})
	r.RegisterRole(RoleSubscriber, "订阅者", 10, map[string]bool{
		"dashboard.read": true,
		"profile.update": true,
	})
}

// RegisterRole adds or replaces a role definition.
func (r *RBAC) RegisterRole(name, displayName string, level int, caps map[string]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roles[name] = &RoleDef{
		Name:        name,
		DisplayName: displayName,
		Level:       level,
		Caps:        caps,
	}
}

// Can checks whether a role has a specific capability.
func (r *RBAC) Can(role, resource, action string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.roles[role]
	if !ok {
		return false
	}
	// Super wildcard
	if def.Caps["*.*"] {
		return true
	}
	cap := resource + "." + action
	return def.Caps[cap]
}

// GetRole returns a role definition.
func (r *RBAC) GetRole(name string) *RoleDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.roles[name]
}

// AllRoles returns all registered role definitions.
func (r *RBAC) AllRoles() []*RoleDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*RoleDef
	for _, def := range r.roles {
		out = append(out, def)
	}
	return out
}

// RoleLevel returns the level of a role (0 if not found).
func (r *RBAC) RoleLevel(role string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if def, ok := r.roles[role]; ok {
		return def.Level
	}
	return 0
}
