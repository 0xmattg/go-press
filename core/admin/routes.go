package admin

import (
	"go-press/core/content"
	"go-press/core/user"

	"github.com/gin-gonic/gin"
)

// contentTypeMiddleware sets the content_type key in the gin context.
func contentTypeMiddleware(typeName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("content_type", typeName)
		c.Next()
	}
}

// taxonomyTypeMiddleware sets the taxonomy_type key in the gin context.
func taxonomyTypeMiddleware(taxType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("taxonomy_type", taxType)
		c.Next()
	}
}

// SetupRoutes registers all admin routes on the given Gin engine.
// Content type and taxonomy routes are generated dynamically from the registry.
func SetupRoutes(r *gin.Engine, h *Handler, auth *user.Auth, registry *content.Registry) {
	// Public routes (no auth)
	r.GET("/admin/login", h.LoginPage)
	r.POST("/admin/login", h.LoginSubmit)

	// Protected routes
	admin := r.Group("/admin")
	admin.Use(AuthMiddleware(auth))
	{
		admin.GET("/", h.Dashboard)
		admin.GET("/logout", h.Logout)

		// ---- Dynamic content type routes ----
		for _, typeDef := range registry.AllTypes() {
			slug := AdminSlug(typeDef.Name)
			typeName := typeDef.Name

			group := admin.Group("/" + slug)
			group.Use(contentTypeMiddleware(typeName))

			// All content types get list + delete
			group.GET("", h.ContentList)
			group.POST("/:id/delete", h.ContentDelete)

			if typeDef.HasArchive {
				// Full CRUD for archive-enabled content types
				group.GET("/new", h.ContentNew)
				group.POST("/new", h.ContentCreate)
				group.GET("/:id/edit", h.ContentEdit)
				group.POST("/:id/edit", h.ContentUpdate)
				// Drag-and-drop reorder (returns JSON)
				group.POST("/reorder", h.ContentReorder)
			} else {
				// Read-only detail for non-archive types (e.g. contact_message)
				group.GET("/:id", h.ContentDetail)
			}
		}

		// ---- Dynamic taxonomy routes ----
		for _, taxDef := range registry.AllTaxonomies() {
			slug := AdminSlug(taxDef.Name)
			taxType := taxDef.Name

			group := admin.Group("/" + slug)
			group.Use(taxonomyTypeMiddleware(taxType))

			group.GET("", h.TaxonomyList)
			group.POST("", h.TaxonomyCreate)
			group.POST("/:id/edit", h.TaxonomyUpdate)
			group.POST("/:id/delete", h.TaxonomyDelete)
		}

		// ---- System routes (not content-type driven) ----
		admin.GET("/settings", h.SettingList)
		admin.POST("/settings", h.SettingUpdate)
		admin.POST("/sitemap/generate", h.SitemapGenerate)

		admin.GET("/media", h.MediaList)
		admin.GET("/media/json", h.MediaJSON)
		admin.POST("/media/upload", h.MediaUpload)
		admin.POST("/media/upload-json", h.MediaUploadJSON)
		admin.POST("/media/regenerate", h.MediaRegenerateVariants)
		admin.POST("/media/:id/meta", h.MediaUpdateMeta)
		admin.POST("/media/:id/delete", h.MediaDelete)

		admin.GET("/users", h.UserList)
		admin.GET("/users/new", h.UserNew)
		admin.POST("/users/new", h.UserCreate)
		admin.GET("/users/:id/edit", h.UserEdit)
		admin.POST("/users/:id/edit", h.UserUpdate)
		admin.POST("/users/:id/delete", h.UserDelete)

		admin.GET("/themes", h.ThemeList)
		admin.POST("/themes/switch", h.ThemeSwitch)
		admin.POST("/themes/demo-import", h.ThemeDemoImport)
		admin.GET("/themes/:slug/settings", h.ThemeSettings)
		admin.POST("/themes/:slug/settings", h.ThemeSettingsSave)

		admin.GET("/plugins", h.PluginList)
		admin.POST("/plugins/activate", h.PluginActivate)
		admin.POST("/plugins/deactivate", h.PluginDeactivate)
		admin.GET("/plugins/:slug/settings", h.PluginSettings)
		admin.POST("/plugins/:slug/settings", h.PluginSettingsSave)

		admin.GET("/cache", h.CacheStatus)
		admin.POST("/cache/flush", h.CacheFlush)

		admin.GET("/redirects", h.RedirectList)
		admin.POST("/redirects", h.RedirectAdd)
		admin.POST("/redirects/delete", h.RedirectRemove)

		admin.GET("/menus", h.MenuList)
		admin.POST("/menus", h.MenuCreate)
		admin.GET("/menus/:id/edit", h.MenuEdit)
		admin.POST("/menus/:id/edit", h.MenuUpdate)
		admin.POST("/menus/:id/delete", h.MenuDelete)
		admin.POST("/menus/:id/items", h.MenuSaveItems)
	}
}
