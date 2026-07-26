# Theme Dependencies and Versioning

Some themes only work correctly when certain plugins are active (for example a
theme that relies on multilang for language-prefixed routing). GoPress lets a
theme **declare** the plugins it needs in `theme.toml`; core resolves and
enforces them.

> This is a **module-level dependency**: the theme names a plugin by slug ("I
> need this whole plugin module"), but its runtime code still only benefits
> through core's generic extension points (hooks/filters/funcmap) — it never
> imports, calls, or references the plugin internals. Direction is **theme →
> plugin** only; plugin→plugin dependencies are not supported.

## Declaration

Add a `[requires]` block to `theme.toml`:

```toml
[requires]
core = ">=0.7.0"                      # optional: required GoPress core version (semver constraint)
plugins = [
  { slug = "multi-language", version = ">=2.0.0" },
  { slug = "seo-extras",     version = "^1.0.0", optional = false },
]
```

- `slug` — the required plugin's **runtime identity** (its `Name()`, also declared
  in the plugin's `plugin.toml` `[plugin].slug`). Note the slug can differ from the
  plugin **directory** name (dir `multilang` → slug `multi-language`).
- `version` — optional semver constraint (`>=2.0.0`, `^1.0`, `~1.2`, `>=1 <2`, …).
  Empty means any version.
- `optional` — when `true`, an unmet dependency is a warning, not a hard block.
- `core` — optional constraint on the core version.

## Mandatory versioning

Theme and plugin versions must be valid semver and come from a **single source**:

- Theme version comes from `theme.toml [theme].version` (the theme embeds it via
  `//go:embed theme.toml`; `BaseTheme` parses it — do not hardcode it in Go).
- Plugin version comes from `plugin.toml [plugin].version` (the plugin embeds it
  via `//go:embed plugin.toml`).

Both toml files are **baked into the binary** via `//go:embed` and read the same
way, so if you change code you must bump the matching toml version — otherwise the
binary reports a stale version.

Core validates versions at **build time** (`gopress gen/build`) and at **boot**.

## The four dependency states

| State | Meaning | Handling |
|---|---|---|
| **Satisfied** | plugin present, active, version in range | allowed |
| **Inactive** | present and in range but not activated | **auto-activatable** (offered on theme switch) |
| **Version mismatch** | active but version fails the constraint | **blocks** (shows required vs installed) |
| **Missing** | not compiled into this build | **hard block** (see "compile-time reality") |

## Compile-time reality (the key WordPress difference)

GoPress plugins are **compiled into the binary** — there is no runtime install. A
**missing** dependency cannot be fixed at runtime; you must add the plugin to the
build and re-run `gopress build`. The admin never offers "install"; it only
auto-activates plugins that are compiled in but inactive.

## When it is enforced, and how it shows

1. **Build time** (`gopress gen/build`): scans each theme's `[requires]` and warns
   about missing plugins, invalid semver, and unmet version constraints.
2. **Boot**: resolves the **active** theme's dependencies — inactive required
   plugins are **auto-activated**; missing/incompatible ones let the site boot but
   raise a **warning banner** in the admin.
3. **Theme switch**: the target theme is pre-checked.
   - **Inactive** deps: the switch confirm dialog lists the plugins that will be
     enabled; on confirm the switch is transactional (activate plugins first, roll
     back on failure).
   - **Missing/version mismatch**: the card shows a red "missing dependency" badge,
     the switch button is disabled, and the reason is shown on hover.
4. **Plugin deactivate**: if the active theme depends on a plugin, its deactivate
   button is disabled with "required by the active theme; switch themes first".

## For plugin authors: the `plugin.toml` slug

To be depended on, a plugin's `plugin.toml` must declare `slug`, and it must equal
the plugin's `Name()`:

```toml
[plugin]
slug = "multi-language"   # runtime identity; themes depend on this; must equal Name()
name = "Multi-Language"   # human-readable display name
version = "2.0.0"
```
