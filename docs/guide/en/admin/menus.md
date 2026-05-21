# Menu Management

GoPress includes a menu system with named locations, hierarchical items, language-aware assignment, and admin visual management.

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
