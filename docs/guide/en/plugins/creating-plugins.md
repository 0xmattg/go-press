# Creating Plugins

This guide shows the basic structure of a GoPress plugin.

## Minimal Plugin

```go
// plugins/my-plugin/plugin.go
package myplugin

import (
    "go-press/core"
    "go-press/core/hook"
    "go-press/core/plugin"
)

type Plugin struct {
    engine *core.Engine
    hooks  []hook.Handle
}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string        { return "my-plugin" }
func (p *Plugin) Version() string     { return pluginMeta.Version }
func (p *Plugin) Description() string { return "Example plugin" }

func (p *Plugin) Activate(app plugin.App) {
    engine, ok := app.(*core.Engine)
    if !ok {
        return
    }
    p.engine = engine
    handle := engine.Hooks.AddFilter("theme.footer.end", p.renderFooter, 10)
    p.hooks = append(p.hooks, handle)
}

func (p *Plugin) Deactivate(_ plugin.App) {
    for _, handle := range p.hooks {
        p.engine.Hooks.RemoveFilter(handle)
    }
    p.hooks = nil
}
```

```go
// plugins/my-plugin/register.go
package myplugin

import "go-press/core"

func init() {
    core.RegisterPlugin("my-plugin", func(engine *core.Engine) {
        engine.LoadPlugin(New())
    })
}
```

No manual `cmd/server/main.go` edit is required. Drop the folder into `plugins/`, make sure it has both `plugin.toml` and at least one non-test `.go` file at its root, then re-run `gopress serve`. The autoload package is regenerated and the new plugin's `init()` registers itself with `core.RegisterPlugin` at startup. See [Getting Started > Installation](../getting-started/installation.md) for details.

## Plugin Metadata

Every plugin must ship a `plugin.toml` at its root — it both serves as the auto-detection marker (the `gopress` CLI ignores a `plugins/<name>/` directory without it) and provides metadata for the admin UI and future plugin registry features. Minimum schema:

```toml
[plugin]
slug = "my-plugin"
name = "My Plugin"
version = "1.0.0"
description = "Short summary of what the plugin does."
author = "Me"
```

`slug` is the runtime identity returned by `Name()` and used by theme dependency
declarations. `version` must be valid semver and is the single source of truth:
embed and parse `plugin.toml`, then return the parsed value from `Version()`
instead of writing the version again in Go.

```go
import (
    _ "embed"

    "go-press/core/plugin"
)

//go:embed plugin.toml
var pluginTOML string

var pluginMeta = plugin.ParseMetaString(pluginTOML)
```

## Plugin Data

For plugin-owned tables, use the database prefix helpers:

```go
table := dbprefix.PluginTable("my-plugin", "items")
core.RegisterPluginTable("my-plugin", "items")
```

This keeps plugin tables isolated from core and theme tables, and allows admin database tooling to identify table ownership.

## Settings Pages

Plugins that need admin configuration should implement the settings provider interfaces used by the admin plugin page. The plugin owns the UI, data loading, and save handling, while the admin owns routing, permissions, layout, and language context.

Keep settings templates translated through locale files instead of hard-coded strings.

Use `SettingsValidateProvider` for validation that must run against the complete submitted option set before Core persists anything. It must be side-effect free; use `SettingsSaveProvider` only for post-persistence work:

```go
func (p *MyPlugin) ValidateSettings(settings map[string]string) error {
    // Reject malformed, unsafe, or internally inconsistent values.
    return nil
}

func (p *MyPlugin) OnSettingsSave(settings map[string]string) {
    // Optional post-save synchronization.
}
```

The admin route performs the plugin settings resource's `update` RBAC check before validation or persistence. A plugin with a narrower settings capability should also implement `SettingsAuthorizationProvider` rather than registering a weaker custom route.

## Admin Card Logo

Implement the optional `LogoProvider` to show an icon on the admin **Plugins** card. Embed a square `static/logo.svg` (`viewBox="0 0 48 48"`) and return it:

```go
//go:embed static/logo.svg
var logoSVG string

func (p *Plugin) LogoSVG() string { return logoSVG }
```

Core runs `content.SanitizeSVG` (stripping `<script>`, `on*` handlers, `javascript:` URIs) before inlining, so third-party plugin logos are safe; return `""` for no icon.

## Frontend Output

Use standard theme hook slots:

- `theme.head.end`
- `theme.body.open`
- `theme.footer.end`
- `header.nav.after`

The plugin output must match the semantic location. For example, `header.nav.after` should normally output navigation list items, not a floating widget.

## Request-level Content and Taxonomy Filtering

Extensions that implement language, visibility, tenant, or preview rules should
register a request-local scope through `content.AddContentScope` in
`middleware.early`. Core repositories, BaseTheme, custom `PageService` clones,
and scoped admin lists can then consume the condition without plugin-specific
code. Extensions that own variant-specific Category/Tag identities should also
register `taxonomy.Scope` and route taxonomy writes through
`taxonomy.CommandService`; do not add language or tenant branches to core. See
the [Content and Taxonomy Scope APIs](../architecture/content-scope.md).

## Common Hooks

| Hook | Type | Purpose |
|---|---|---|
| `engine.init` | action | Run after bootstrap. |
| `middleware.early` | action | Register middleware before page cache. |
| `routes.register` | action | Register routes before the frontend catch-all. |
| `admin.content_form.fields` | filter | Add content-editor fields. |
| `admin.content.saved` | action | Persist extension-owned values after content save. |
| `admin.dashboard.widgets` | filter | Add a trusted, permission-aware dashboard widget. |
| `seo.content.meta` | filter | Transform per-content SEO metadata. |
| `theme.head.end`, `theme.body.open`, `theme.footer.end` | filter | Contribute frontend markup at semantic slots. |
| `header.nav.after` | filter | Add a primary-navigation item. |

## Identity Provider Plugins

An external identity plugin must integrate through core's `plugin.PublicAuthHost` contract. The plugin owns the provider protocol, callback verification, credentials, and provider-specific settings; core alone owns GoPress users, linked identities, sessions, registration policy, and account-linking policy.

After verifying the provider response, pass a normalized `user.VerifiedIdentity` to core. Do not create users or sessions directly from the plugin, and do not expose provider SDK types to themes. Login entry points are published to themes through the provider registry and the generic `loginProviders` template helper.

The complete contract, Google OIDC and MetaMask SIWE examples, provider icons, and route/RBAC rules are documented in [Public Accounts and External Identity](../architecture/public-authentication.md).

## Deactivation Checklist

- Remove every action/filter handle.
- Stop middleware or route behavior from affecting requests.
- Leave stored data in place unless the user explicitly uninstalls the plugin.
- Clear relevant cache paths after activation state changes.

Core rebuilds the Gin router after plugin activation or deactivation, so active
`middleware.early` and `routes.register` hooks are applied immediately. Plugins
with background workers should still keep an atomic runtime guard for requests
already executing on the previous router.
