# Hook System

GoPress provides a WordPress-style hook bus with actions and filters. It is the main extension mechanism for plugins and a useful customization point for themes.

## Actions

Actions are event notifications. They do not return a value.

```go
handle := engine.Hooks.AddAction("content.saved", func(args ...interface{}) {
    // react to content changes
}, 10)
```

## Filters

Filters receive a value, modify it, and return the updated value.

```go
handle := engine.Hooks.AddFilter("seo.content.meta", func(value interface{}, args ...interface{}) interface{} {
    return value
}, 10)
```

Priority controls execution order. Lower numbers run earlier.

## Removing Hooks

Plugins must keep returned handles and remove them during deactivation:

```go
engine.Hooks.RemoveFilter(handle)
```

This is required for plugin hot-disable behavior. A disabled plugin should not keep modifying frontend output, admin forms, SEO data, sitemap entries, or menus.

## Common Hook Areas

- Admin content form fields and save actions.
- SEO metadata transformation.
- Sitemap URL transformation.
- Frontend template slots.
- Menu location resolution.
- Content list tabs and admin UI extensions.

Hooks should carry stable, documented payloads. If a hook needs too many assumptions about a theme or plugin, it probably belongs behind a smaller core interface.

