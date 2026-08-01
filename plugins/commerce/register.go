package commerce

import "go-press/core"

func init() {
	core.RegisterPlugin(pluginSlug, func(engine *core.Engine) {
		p := New()
		// Capture the engine up front so metadata like Description() can localize
		// to the admin language even while the module is inactive (commerce ships
		// default-inactive, so Activate — which also sets this — may never run).
		p.engine = engine
		engine.LoadPlugin(p)
	})
}
