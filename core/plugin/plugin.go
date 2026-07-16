package plugin

import "go-press/core/user"

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

// SettingsProvider is an optional interface that plugins can implement
// to supply a custom admin settings page for plugin-specific configuration.
type SettingsProvider interface {
	// SettingsTemplatePath returns the absolute path to the plugin's admin
	// settings template file. Return "" if no settings page is available.
	SettingsTemplatePath() string
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
