package commerceusdt

import (
	_ "embed"

	"go-press/core/plugin"
)

//go:embed plugin.toml
var pluginTOML string

// pluginMeta is the single source of truth for this plugin's metadata (including
// default_inactive), parsed from plugin.toml at init.
var pluginMeta = plugin.ParseMetaString(pluginTOML)

// pluginSlug is the stable identity used for registration and option namespacing.
const pluginSlug = "commerce-usdt"

// gatewayID is the payment-method identifier stored on orders.
const gatewayID = "usdt"
