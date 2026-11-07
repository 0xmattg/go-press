package commerce

import (
	"embed"
	"io/fs"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/admin"
	"github.com/0xmattg/go-press/core/assets"
)

// commerceFiles holds the plugin's templates, locales and static files so it
// runs without its source directory. Core finds the settings page at
// templates/admin/settings.tmpl and the admin catalog under locales/admin.
//
//go:embed templates locales static
var commerceFiles embed.FS

// Assets implements plugin.AssetsProvider.
func (p *Plugin) Assets() fs.FS { return commerceFiles }

// files returns the plugin filesystem as core resolves it (the directory on
// disk outside release mode, see core.Engine.PluginFS), or the embedded files
// before Activate.
func (p *Plugin) files() fs.FS {
	if p != nil && p.engine != nil {
		if fsys := p.engine.PluginFS(p); fsys != nil {
			return fsys
		}
	}
	return commerceFiles
}

// renderStorefront renders one of the plugin's storefront fragments
// (templates/commerce/<fragment>.tmpl) inside the active theme's layout. A
// theme overrides it with templates/commerce/<fragment>.tmpl of its own.
func (p *Plugin) renderStorefront(c *gin.Context, fragment string, data gin.H) error {
	return p.engine.RenderNamespacedInActiveThemeFS(c, pluginSlug, fragment, assets.Sub(p.files(), "templates/commerce"), data)
}

// adminTemplate locates one of the plugin's admin page fragments.
func (p *Plugin) adminTemplate(fragment string) admin.TemplateRef {
	return admin.TemplateRef{FS: p.files(), Name: "templates/admin/" + fragment + ".tmpl"}
}
