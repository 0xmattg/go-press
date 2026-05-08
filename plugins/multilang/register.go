// Package multilang provides an init-based auto-registration for the plugin.
//
// Import with blank identifier in main.go:
//
//	import _ "go-press/plugins/multilang"
package multilang

import "go-press/core"

func init() {
	core.RegisterPlugin(PluginName, func(engine *core.Engine) {
		engine.LoadPlugin(New())
	})
}
