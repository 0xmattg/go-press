// Package multilang provides an init-based auto-registration for the plugin.
//
// Import with blank identifier in main.go:
//
//	import _ "github.com/0xmattg/go-press/plugins/multilang"
package multilang

import (
	"github.com/0xmattg/go-press/core"
	"github.com/0xmattg/go-press/pkg/logger"
)

func init() {
	core.RegisterPlugin(PluginName, load)
}

func load(engine *core.Engine) {
	// LEGACY(multilang-slug): see legacy_slug.go. Must run before LoadPlugin,
	// which reads the persisted activation flag.
	if _, err := migrateLegacySlugOptions(engine.DB, engine.Options); err != nil {
		logger.Error("multilang: legacy option migration failed", "error", err)
	}
	engine.LoadPlugin(New())
}
