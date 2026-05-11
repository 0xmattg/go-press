# Architecture Overview

GoPress is organized as a core engine plus registered themes and plugins. Themes and plugins do not call each other directly; both communicate with core through interfaces, registries, hooks, template helpers, settings providers, and request context.

## Startup Flow

```text
gopress serve
  -> scan themes/ and plugins/ for marker files (theme.toml / plugin.toml)
  -> regenerate internal/autoload/autoload_gen.go (blank imports)
  -> exec `go run ./cmd/server` (flags forwarded, signals forwarded)
cmd/server
  -> blank-import internal/autoload (triggers all theme/plugin init())
  -> config discovery
  -> database connection
  -> table prefix setup
  -> engine construction
  -> core migrations
  -> theme and plugin registration
  -> active theme setup
  -> admin, API, installer, and frontend routes
```

`gopress build` follows the same first three steps but ends with `go build -o build/gopress-server ./cmd/server` instead of `go run` — production binaries do not need the Go toolchain at runtime.

If the site is not installed yet, the handler switcher routes requests to the installer. After setup, the same process can switch to the live application without requiring a manual TOML edit.

## Engine Responsibilities

- Own database repositories, rewrite engine, SEO builder, hook bus, cache, media repository, workers, and admin services.
- Register core content types that must survive theme switching.
- Load theme-declared content types from `theme.toml`.
- Initialize active plugins and remove plugin hooks when they are disabled.
- Rebuild frontend routes after theme switching or relevant setting changes.

## Extension Model

The extension model is deliberately simple:

- Themes register themselves with `core.RegisterTheme`.
- Plugins register themselves with `core.RegisterPlugin`.
- Themes use `theme.App` to access engine services.
- Plugins subscribe to actions and filters exposed by core.
- Shared behavior is exposed by core-level helpers and hook names.

This keeps the architecture open to third-party packages without creating hidden dependencies between themes and plugins.

