package plugin

import (
	"github.com/0xmattg/go-press/core/agent"
	"github.com/0xmattg/go-press/core/user"

	"github.com/BurntSushi/toml"
)

// Meta is the [plugin] metadata parsed from a plugin.toml file. plugin.toml is
// the single source of truth for a plugin's version (consumed by theme
// dependency resolution); Name() remains the Go slug identity, distinct from the
// human-facing Meta.Name.
type Meta struct {
	// Slug is the plugin's stable machine identity — it must equal the plugin's
	// Name() and is what themes reference in [requires].plugins. Distinct from
	// the human-facing Name.
	Slug        string `toml:"slug"`
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
	Author      string `toml:"author"`
	// DefaultInactive marks an opt-in "module" that ships disabled by default
	// and is enabled explicitly by the operator (see DefaultInactiveProvider).
	DefaultInactive bool `toml:"default_inactive"`
}

// ParseMetaString parses the [plugin] table from embedded plugin.toml content.
// On error it returns a zero Meta so the plugin still loads; version validation
// at boot/build time surfaces a malformed or missing version separately.
func ParseMetaString(data string) Meta {
	var file struct {
		Plugin Meta `toml:"plugin"`
	}
	if _, err := toml.Decode(data, &file); err != nil {
		return Meta{}
	}
	return file.Plugin
}

// App is the runtime object passed to Plugin lifecycle methods.
//
// The concrete value is currently *core.Engine. It remains intentionally loose
// here to avoid an import cycle; plugins that need engine services should type
// assert the capabilities they use and fail gracefully if unavailable.
type App interface{}

// PublicAuthHost is the generic capability external identity plugins use to
// register login methods and complete verified sign-ins.
type PublicAuthHost interface {
	PublicAuthenticator() *user.PublicAuth
	PublicSiteURL() string
}

// AgentHost is the narrow, protocol-neutral capability available to business
// plugins that contribute Agent Tools. Registration returns revocable handles;
// plugins must revoke their own handles during Deactivate.
type AgentHost interface {
	AgentToolRegistry() *agent.Registry
	AgentExecutor() *agent.Executor
}

// Plugin is the lifecycle contract every GoPress plugin must implement.
//
// Plugins are registered at startup, then activated or deactivated by Engine
// according to persisted admin state. Activate should wire hooks, routes,
// middleware, repositories, or settings providers. Deactivate should undo
// runtime registrations, especially hook and sitemap transformer handles.
type Plugin interface {
	// Metadata
	Name() string
	Version() string
	Description() string

	// Lifecycle
	Activate(app App)
	Deactivate(app App)
}

// DefaultInactiveProvider is an optional interface a plugin implements to
// declare that it ships disabled by default — an opt-in "module" the operator
// enables explicitly (e.g. the commerce engine). On first run (no persisted
// activation state) Engine.LoadPlugin leaves such a plugin registered but
// inactive, and the admin surfaces it as a togglable module. Plugins without
// this interface default to active, preserving existing behavior.
type DefaultInactiveProvider interface {
	DefaultInactive() bool
}

// SettingsProvider is an optional interface that plugins can implement
// to supply a custom admin settings page for plugin-specific configuration.
type SettingsProvider interface {
	// SettingsTemplatePath returns the absolute path to the plugin's admin
	// settings template file. Return "" if no settings page is available.
	SettingsTemplatePath() string
}

// SettingsAuthorizationProvider lets a plugin declare the RBAC resource used
// by its generic settings page. Core supplies the actions ("read" for the GET
// page and "update" for the POST save handler) and falls back to the existing
// "plugin" resource when this optional interface is not implemented.
//
// Keeping this declarative avoids core branching on a concrete plugin slug
// while allowing domain modules to grant narrowly scoped settings access.
type SettingsAuthorizationProvider interface {
	SettingsPermissionResource() string
}

// SettingsDataProvider is an optional interface that plugins can implement
// to inject extra data into their settings page template.
type SettingsDataProvider interface {
	// SettingsData returns additional template data for the settings page.
	SettingsData() map[string]interface{}
}

// SettingsSaveProvider is an optional interface that plugins can implement
// to react when their settings are saved (e.g. sync data to plugin tables).
type SettingsSaveProvider interface {
	// OnSettingsSave is called after plugin settings are persisted to the options table.
	OnSettingsSave(settings map[string]string)
}

// SettingsValidateProvider lets a plugin reject invalid settings before Core
// persists any option. Validation must be side-effect free; OnSettingsSave is
// still the post-persistence notification hook.
type SettingsValidateProvider interface {
	ValidateSettings(settings map[string]string) error
}

// LogoProvider is an optional interface that plugins can implement to supply an
// inline SVG logo shown on the admin plugin card. Return "" for no logo. Core
// sanitizes the markup before rendering it into admin pages.
type LogoProvider interface {
	LogoSVG() string
}

// Slug returns the stable admin/settings identifier for a plugin.
//
// Plugin names are already expected to be URL-safe slugs such as
// "multi-language" or "seo-extras", so the current implementation returns
// Name unchanged.
func Slug(p Plugin) string {
	return p.Name()
}

// Manager tracks registered and active plugins for one Engine.
//
// It does not persist activation state; Engine stores that in options and calls
// Activate or Deactivate as needed. Manager only owns the in-memory lifecycle
// bookkeeping.
type Manager struct {
	registered []Plugin
	active     map[string]Plugin
}

// NewManager creates a new plugin Manager.
func NewManager() *Manager {
	return &Manager{
		active: make(map[string]Plugin),
	}
}

// Register adds a plugin to the registered list.
//
// Register does not activate the plugin. Engine.LoadPlugin handles activation
// after checking persisted plugin_active_* options.
func (m *Manager) Register(p Plugin) {
	m.registered = append(m.registered, p)
}

// Activate activates a registered plugin by name.
//
// It returns false when no registered plugin matches name. If Activate returns
// true, the plugin is stored in the active map immediately after its lifecycle
// method completes.
func (m *Manager) Activate(name string, app App) bool {
	for _, p := range m.registered {
		if p.Name() == name {
			p.Activate(app)
			m.active[name] = p
			return true
		}
	}
	return false
}

// Deactivate deactivates an active plugin.
func (m *Manager) Deactivate(name string, app App) bool {
	p, ok := m.active[name]
	if !ok {
		return false
	}
	p.Deactivate(app)
	delete(m.active, name)
	return true
}

// IsActive checks if a plugin is currently active.
func (m *Manager) IsActive(name string) bool {
	_, ok := m.active[name]
	return ok
}

// ActivePlugins returns all active plugins.
func (m *Manager) ActivePlugins() []Plugin {
	var out []Plugin
	for _, p := range m.active {
		out = append(out, p)
	}
	return out
}

// RegisteredPlugins returns all registered plugins.
func (m *Manager) RegisteredPlugins() []Plugin {
	return m.registered
}

// IsDefaultInactive reports whether the registered plugin with the given slug
// declares itself default-inactive (an opt-in module). False when the plugin is
// absent or does not implement DefaultInactiveProvider.
func (m *Manager) IsDefaultInactive(slug string) bool {
	for _, p := range m.registered {
		if Slug(p) == slug {
			if dp, ok := p.(DefaultInactiveProvider); ok {
				return dp.DefaultInactive()
			}
			return false
		}
	}
	return false
}

// FindBySlug returns the registered plugin whose identity (Name/Slug) matches,
// or false when no such plugin is compiled into this build.
func (m *Manager) FindBySlug(slug string) (Plugin, bool) {
	for _, p := range m.registered {
		if Slug(p) == slug {
			return p, true
		}
	}
	return nil, false
}
