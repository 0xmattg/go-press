package moderncompany

import "embed"

// themeTOML is this theme's theme.toml, embedded so metadata, dependencies, and
// content types are baked into the binary (matching how plugins embed
// plugin.toml) instead of being read from disk at runtime.
//
//go:embed theme.toml
var themeTOML string

// themeFiles holds the theme's templates, static files, locales and demo data,
// passed to BaseTheme.InitBaseFS so the theme runs without its source
// directory. Outside release mode core reads the directory on disk instead
// when it exists, so template and CSS edits show without rebuilding.
//
//go:embed templates static locales demo
var themeFiles embed.FS
