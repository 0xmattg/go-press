// Package multilang provides an init-based auto-registration for the plugin.
//
// Import with blank identifier in main.go:
//
//	import _ "github.com/0xmattg/go-press/plugins/multilang"
package multilang

import "github.com/0xmattg/go-press/core"

func init() {
	core.RegisterPlugin(PluginName, func(engine *core.Engine) {
		engine.LoadPlugin(New())
	})
}
