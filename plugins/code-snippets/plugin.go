// Package codesnippets provides a WPCode-like plugin for GoPress.
//
// It lets admins inject arbitrary HTML/JS snippets into three theme slots —
// before </head>, immediately after <body>, and before </body> — to wire up
// third-party tooling such as Google Analytics, Google Tag Manager,
// Cloudflare verification meta tags, chat widgets, etc., without modifying
// theme files.
//
// Architecture: pure consumer of three theme template hooks declared in core
// (theme.head.end / theme.body.open / theme.footer.end). Themes that opt into
// plugin-friendly contracts call {{renderHook ...}} at those positions; this
// plugin registers a filter on each hook and appends the admin-configured
// snippet to whatever previous filters returned.
//
// Storage: three plain options rows keyed plugin_code-snippets_{slot}. No
// custom DB table — simple is enough for the GA/GTM use case. A future
// version can grow into a per-snippet table with conditions / priorities
// without touching this contract.
//
// Usage in main.go:
//
//	import _ "go-press/plugins/code-snippets"
package codesnippets

import (
	"html/template"
	"path/filepath"
	"strings"

	"go-press/core"
	"go-press/core/hook"
	"go-press/core/plugin"
	"go-press/pkg/logger"
)

const (
	PluginName = "code-snippets"

	// Option keys persisted to gp_options. Names map 1:1 to the textarea
	// field names rendered by templates/admin/settings.tmpl, so the framework
	// auto-save handler (matches plugin_<slug>_* prefix) writes the values
	// without any custom save plumbing.
	optHeadSnippet   = "plugin_code-snippets_head"
	optBodyOpenSnippet = "plugin_code-snippets_body_open"
	optFooterSnippet = "plugin_code-snippets_footer"
)

// Plugin implements plugin.Plugin and plugin.SettingsProvider.
type Plugin struct {
	engine *core.Engine

	// Hook handles for clean Deactivate; without these, leftover filters
	// would keep injecting snippets after the plugin is disabled.
	hookHandles []hook.Handle
}

// New constructs a fresh Plugin instance.
func New() *Plugin { return &Plugin{} }

// --- Plugin interface ---

func (p *Plugin) Name() string    { return PluginName }
func (p *Plugin) Version() string { return "1.0.0" }
func (p *Plugin) Description() string {
	return "WPCode 风格的代码片段注入：在 <head>、<body> 开头、</body> 前三个位置插入任意 HTML/JS（适合 Google Analytics、GTM、第三方追踪脚本等）"
}

// --- SettingsProvider interface ---

func (p *Plugin) SettingsTemplatePath() string {
	return filepath.Join("plugins", "code-snippets", "templates", "admin", "settings.tmpl")
}

// --- Lifecycle ---

func (p *Plugin) Activate(app plugin.App) {
	e, ok := app.(*core.Engine)
	if !ok {
		logger.Error("code-snippets: failed to cast app to *core.Engine")
		return
	}
	p.engine = e
	p.hookHandles = p.hookHandles[:0]

	p.hookHandles = append(p.hookHandles,
		e.Hooks.AddFilter(hook.ThemeHeadEnd, p.injectHead, 50))
	p.hookHandles = append(p.hookHandles,
		e.Hooks.AddFilter(hook.ThemeBodyOpen, p.injectBodyOpen, 50))
	p.hookHandles = append(p.hookHandles,
		e.Hooks.AddFilter(hook.ThemeFooterEnd, p.injectFooter, 50))

	logger.Info("code-snippets plugin activated")
}

func (p *Plugin) Deactivate(app plugin.App) {
	if p.engine == nil {
		return
	}
	for _, h := range p.hookHandles {
		p.engine.Hooks.RemoveFilter(h)
	}
	p.hookHandles = p.hookHandles[:0]
	logger.Info("code-snippets plugin deactivated")
}

// --- Filter handlers ---

func (p *Plugin) injectHead(value interface{}, args ...interface{}) interface{} {
	return p.appendSnippet(value, optHeadSnippet)
}

func (p *Plugin) injectBodyOpen(value interface{}, args ...interface{}) interface{} {
	return p.appendSnippet(value, optBodyOpenSnippet)
}

func (p *Plugin) injectFooter(value interface{}, args ...interface{}) interface{} {
	return p.appendSnippet(value, optFooterSnippet)
}

// appendSnippet reads the snippet for the given option key and appends it to
// the running template.HTML value passed through the filter chain. Returns
// the value untouched if the plugin was deactivated mid-flight, the option
// is empty, or the value type is unexpected.
func (p *Plugin) appendSnippet(value interface{}, optKey string) interface{} {
	if p.engine == nil || !p.engine.PluginManager.IsActive(PluginName) {
		return value
	}
	if p.engine.Options == nil {
		return value
	}
	snippet := strings.TrimSpace(p.engine.Options.Get(optKey))
	if snippet == "" {
		return value
	}

	existing := template.HTML("")
	switch v := value.(type) {
	case template.HTML:
		existing = v
	case string:
		existing = template.HTML(v)
	}
	// Snippet content is admin-controlled and intentionally treated as raw
	// HTML — that is the whole feature (allow arbitrary <script> tags).
	return existing + template.HTML("\n") + template.HTML(snippet) + template.HTML("\n")
}
