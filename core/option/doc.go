// Package option provides cached key-value configuration for GoPress.
//
// Options are persisted in the database and autoloaded into memory during
// engine bootstrap. The store is used for site settings, active theme state,
// plugin settings, and theme-specific configuration. Translatable option
// metadata is kept in a separate registry so multilingual plugins can expose
// admin translation workflows without coupling to individual themes.
package option
