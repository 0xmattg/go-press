package gopressmcp

import "github.com/0xmattg/go-press/core"

func init() {
	core.RegisterPlugin(PluginName, func(engine *core.Engine) {
		engine.LoadPlugin(New())
	})
}
