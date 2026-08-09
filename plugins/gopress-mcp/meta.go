package gopressmcp

import (
	_ "embed"

	"github.com/0xmattg/go-press/core/plugin"
)

//go:embed plugin.toml
var pluginTOML string

var pluginMeta = plugin.ParseMetaString(pluginTOML)
