package gopresslanding

import _ "embed"

// themeTOML is this theme's theme.toml, embedded so metadata, dependencies, and
// content types are baked into the binary (matching how plugins embed
// plugin.toml) instead of being read from disk at runtime.
//
//go:embed theme.toml
var themeTOML string
