// Package codesnippets provides an init-based auto-registration for the plugin.
//
// Import with blank identifier in main.go:
//
//	import _ "go-press/plugins/code-snippets"
package codesnippets

import "go-press/core"

func init() {
	core.RegisterPlugin(PluginName, func(engine *core.Engine) {
		engine.LoadPlugin(New())
	})
}
