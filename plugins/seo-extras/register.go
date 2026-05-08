// Package seoextras provides Yoast-style per-content SEO override fields.
//
// Import with blank identifier in main.go:
//
//	import _ "go-press/plugins/seo-extras"
package seoextras

import "go-press/core"

func init() {
	core.RegisterPlugin(PluginName, func(engine *core.Engine) {
		engine.LoadPlugin(New())
	})
}
