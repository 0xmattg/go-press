package gopressanalytics

import (
	_ "embed"

	"github.com/0xmattg/go-press/core/plugin"
)

//go:embed plugin.toml
var pluginTOML string

// pluginMeta is the single source of truth for this plugin's metadata, parsed
// from plugin.toml at init. Version() returns pluginMeta.Version.
var pluginMeta = plugin.ParseMetaString(pluginTOML)
