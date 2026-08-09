// Package seoextras provides Yoast-style per-content SEO override fields.
//
// Import with blank identifier in main.go:
//
//	import _ "github.com/0xmattg/go-press/plugins/seo-extras"
package seoextras

import "github.com/0xmattg/go-press/core"

func init() {
	core.RegisterPlugin(PluginName, func(engine *core.Engine) {
		engine.LoadPlugin(New())
	})
}
