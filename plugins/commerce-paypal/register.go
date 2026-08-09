package commercepaypal

import "github.com/0xmattg/go-press/core"

// init registers the plugin factory. Importing core here (only in register.go)
// keeps the commerce coupling elsewhere limited to the core/commerce contracts.
func init() {
	core.RegisterPlugin(pluginSlug, func(engine *core.Engine) {
		engine.LoadPlugin(New())
	})
}
