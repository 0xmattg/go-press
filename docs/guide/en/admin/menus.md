# Menu Management

GoPress includes a menu system with named locations, hierarchical items, language-aware assignment, and admin visual management.

The menu store loads persisted menus into memory, indexes them by ID and
location, and builds parent/child relationships for nested navigation. Admin
users can create, edit, delete, assign, and drag-sort menus and items.

## Concepts

| Concept | Description |
|---|---|
| Menu | A named collection of menu items. |
| Menu item | A link entry that can point to content, taxonomy, custom URL, or another target. |
| Location | A theme-registered slot such as `header` or `footer`. |
| Assignment | A mapping from location, and optionally language, to a menu. |

## Theme Locations

Themes register menu locations in `theme.toml`:

```toml
[[menu_locations]]
name = "header"
label = "Header Navigation"

[[menu_locations]]
name = "footer"
label = "Footer Navigation"
```

The admin displays the active theme's registered locations and lets users assign menus to them.

Manifest declarations are the preferred source because core can inspect them
before theme runtime setup. A theme may still call
`app.MenuStore().RegisterLocation(name, label)` for a genuinely dynamic or
legacy location, but it should not duplicate a manifest entry.

## Rendering

Themes call `menuByLocation` in templates. Core resolves the correct menu for the current location and, when multilingual support is active, the current language.

```gotemplate
{{with menuByLocation "header"}}
  {{range .Items}}
    <a href="{{.URL}}" class="{{if isMenuURLActive $.Ctx .URL}}active{{end}}">{{.Title}}</a>
  {{end}}
{{end}}
```

Active navigation state should be derived from the current request URL and the menu item URL. Do not hard-code content type names, menu labels, or theme-specific page identifiers in reusable theme templates.

## Multilingual Menus

The multilingual plugin can assign different menus per language and translate menu item labels. The theme still renders by location; the plugin changes resolution through the core menu hook.

Menu items can point to content records instead of hard-coded URLs. Core resolves those content links through the same rewrite registry used by `archiveURL` and `contentURL`, so a theme can change a content type's `rewrite_slug` without rewriting every menu item manually.

The generic `menu.location.resolve` filter runs after location lookup and before
the menu reaches the theme; `menu.deleted` lets extensions clean up related
records. The menu package itself contains no multilingual-specific behavior.

In the admin translation surface, each registered location can be assigned a
different menu for every enabled language. The theme continues to request only
the location name.
