// Package theme defines the runtime contract between GoPress core and themes.
//
// Themes implement Theme and usually embed BaseTheme for routing, template
// hierarchy resolution, SEO injection, shared template functions, menu access,
// i18n helpers, and responsive image helpers. Theme configuration in theme.toml
// is loaded into the content registry by core so themes can declare content
// models without duplicating runtime registration code.
package theme
