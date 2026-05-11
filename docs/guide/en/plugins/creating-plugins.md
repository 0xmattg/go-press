# Creating Plugins

This guide shows the basic structure of a GoPress plugin.

## Minimal Plugin

```go
// plugins/my-plugin/plugin.go
package myplugin

import (
    "go-press/core"
    corePlugin "go-press/core/plugin"
)

func init() {
    core.RegisterPlugin("my-plugin", func(engine *core.Engine) corePlugin.Plugin {
        return New(engine)
    })
}

type Plugin struct {
    engine *core.Engine
    hooks  []core.HookHandle
}

func New(engine *core.Engine) *Plugin {
    return &Plugin{engine: engine}
}

func (p *Plugin) Name() string        { return "my-plugin" }
func (p *Plugin) Version() string     { return "1.0.0" }
func (p *Plugin) Description() string { return "Example plugin" }

func (p *Plugin) Activate() error {
    handle := p.engine.Hooks.AddFilter("theme.footer.end", p.renderFooter, 10)
    p.hooks = append(p.hooks, handle)
    return nil
}

func (p *Plugin) Deactivate() error {
    for _, handle := range p.hooks {
        p.engine.Hooks.RemoveFilter(handle)
    }
    p.hooks = nil
    return nil
}
```

No manual `cmd/server/main.go` edit is required. Drop the folder into `plugins/`, make sure it has both `plugin.toml` and at least one non-test `.go` file at its root, then re-run `gopress serve`. The autoload package is regenerated and the new plugin's `init()` registers itself with `core.RegisterPlugin` at startup. See [Getting Started > Installation](../getting-started/installation.md) for details.

## Plugin Metadata

Every plugin must ship a `plugin.toml` at its root — it both serves as the auto-detection marker (the `gopress` CLI ignores a `plugins/<name>/` directory without it) and provides metadata for the admin UI and future plugin registry features. Minimum schema:

```toml
[plugin]
name = "My Plugin"
version = "1.0.0"
description = "Short summary of what the plugin does."
author = "Me"
```

Reserved keys may grow over time (for example dependency declarations or compatibility ranges); keep your file forward-compatible by sticking to the `[plugin]` table for now.

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

## Frontend Output

Use standard theme hook slots:

- `theme.head.end`
- `theme.body.open`
- `theme.footer.end`
- `header.nav.after`

The plugin output must match the semantic location. For example, `header.nav.after` should normally output navigation list items, not a floating widget.

## Deactivation Checklist

- Remove every action/filter handle.
- Stop middleware or route behavior from affecting requests.
- Leave stored data in place unless the user explicitly uninstalls the plugin.
- Clear relevant cache paths after activation state changes.

