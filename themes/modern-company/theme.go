package moderncompany

import (
	"html/template"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go-press/core"
	coreTheme "go-press/core/theme"
)

func init() {
	core.RegisterTheme("modern-company", func(engine *core.Engine, themeDir string) coreTheme.Theme {
		return New(engine, themeDir)
	})
}

// ModernCompanyTheme is a GoPress theme for professional company websites.
// It embeds BaseTheme to gain runtime engine capabilities (rewrite, template hierarchy, SEO).
type ModernCompanyTheme struct {
	coreTheme.BaseTheme
	engine  *core.Engine
	handler *Handler
}

// Compile-time interface check.
var _ coreTheme.Theme = (*ModernCompanyTheme)(nil)
var _ coreTheme.DemoDataProvider = (*ModernCompanyTheme)(nil)
var _ coreTheme.SettingsProvider = (*ModernCompanyTheme)(nil)

// New creates a new ModernCompanyTheme.
func New(engine *core.Engine, themeDir string) *ModernCompanyTheme {
	svc := NewPageService(engine)
	handler := NewHandler(svc, themeDir, engine.Menus, engine.I18n)
	t := &ModernCompanyTheme{
		engine:  engine,
		handler: handler,
	}

	// Initialize BaseTheme with engine capabilities and theme-specific funcs.
	// Theme-specific helpers go here so they're merged into BaseFuncMap and
	// thus available to every template loader (page bundle, hierarchy loader).
	t.InitBase(engine, themeDir, template.FuncMap{
		"formatDateTime": func(tm *time.Time) string {
			if tm == nil {
				return ""
			}
			return tm.Format("January 2, 2006")
		},
		"stripTags": func(s string) string {
			return strings.TrimSpace(reHTMLTags.ReplaceAllString(s, " "))
		},
		"isMenuActive": isMenuActive,
		// whatsappLink turns a free-form WhatsApp number (e.g. "+86 510 8321 0000")
		// into a wa.me deep link by stripping every non-digit. Returns "" when the
		// input has no digits so callers can omit the link entirely.
		"whatsappLink": func(s string) string {
			digits := reNonDigit.ReplaceAllString(s, "")
			if digits == "" {
				return ""
			}
			return "https://wa.me/" + digits
		},
	})

	// Register custom static-page routes (these take priority over rewrite engine)
	t.AddRoute("GET", "/", t.handler.Home)
	t.AddRoute("GET", "/about", t.handler.About)
	t.AddRoute("GET", "/products", t.handler.Products)
	t.AddRoute("GET", "/services", t.handler.Services)
	t.AddRoute("GET", "/showcase", t.handler.Showcase)
	t.AddRoute("GET", "/blog", t.handler.Blog)
	t.AddRoute("GET", "/contact", t.handler.Contact)
	t.AddRoute("POST", "/contact", t.handler.ContactSubmit)

	// Compile per-page templates via core's shared bundle loader (uses
	// BaseFuncMap so all themes get the same helper surface).
	if err := t.handler.LoadPageTemplates(t); err != nil {
		panic(err)
	}

	// Load templates via core TemplateEngine (hierarchy-aware, for dynamic
	// content type resolution).
	t.LoadTemplates(t)

	return t
}

// NewWithDB creates a ModernCompanyTheme with only a database connection.
// Useful for testing or standalone usage without a full Engine.
func NewWithDB(db *gorm.DB, themeDir string) *ModernCompanyTheme {
	svc := NewPageServiceDB(db)
	handler := NewHandler(svc, themeDir, nil, nil)
	t := &ModernCompanyTheme{
		handler: handler,
	}
	t.InitBase(nil, themeDir, template.FuncMap{
		"formatDateTime": func(tm *time.Time) string {
			if tm == nil {
				return ""
			}
			return tm.Format("January 2, 2006")
		},
		"stripTags":    func(s string) string { return strings.TrimSpace(reHTMLTags.ReplaceAllString(s, " ")) },
		"isMenuActive": isMenuActive,
		"whatsappLink": func(s string) string {
			digits := reNonDigit.ReplaceAllString(s, "")
			if digits == "" {
				return ""
			}
			return "https://wa.me/" + digits
		},
	})
	t.AddRoute("GET", "/", t.handler.Home)
	t.AddRoute("GET", "/about", t.handler.About)
	t.AddRoute("GET", "/products", t.handler.Products)
	t.AddRoute("GET", "/services", t.handler.Services)
	t.AddRoute("GET", "/showcase", t.handler.Showcase)
	t.AddRoute("GET", "/blog", t.handler.Blog)
	t.AddRoute("GET", "/contact", t.handler.Contact)
	t.AddRoute("POST", "/contact", t.handler.ContactSubmit)
	return t
}

// --- Metadata ---

func (t *ModernCompanyTheme) Name() string    { return "Modern Company" }
func (t *ModernCompanyTheme) Version() string { return "1.0.0" }
func (t *ModernCompanyTheme) Description() string {
	return "A modern, professional company website theme for GoPress."
}
func (t *ModernCompanyTheme) Author() string { return "Hurricane Techs" }

// --- Lifecycle ---

// Setup registers theme runtime hooks and menu locations.
func (t *ModernCompanyTheme) Setup(app coreTheme.App) {
	if t.engine == nil {
		return
	}

	t.engine.Menus.RegisterLocation("header", "顶部导航")
	t.engine.Menus.RegisterLocation("footer", "底部导航")

	// Register translatable option keys (text-based settings that need translation)
	registerTranslatableOptions()
}

// ServeHTTP handles all frontend requests.
// Detail routes (e.g. /products/slug) are matched here before falling back to BaseTheme.
func (t *ModernCompanyTheme) ServeHTTP(c *gin.Context) {
	path := c.Request.URL.Path
	if c.Request.Method == "GET" {
		// Taxonomy archive routes are handled by BaseTheme via rewrite engine,
		// but we intercept them here so the theme-specific handler/template is used.
		if slug, ok := matchPrefix(path, "/category/"); ok {
			t.handler.TaxonomyArchive(c, "category", slug)
			return
		}
		if slug, ok := matchPrefix(path, "/tag/"); ok {
			t.handler.TaxonomyArchive(c, "tag", slug)
			return
		}
		if slug, ok := matchPrefix(path, "/products/"); ok {
			c.Params = append(c.Params, gin.Param{Key: "slug", Value: slug})
			t.handler.ProductDetail(c)
			return
		}
		if slug, ok := matchPrefix(path, "/services/"); ok {
			c.Params = append(c.Params, gin.Param{Key: "slug", Value: slug})
			t.handler.ServiceDetail(c)
			return
		}
		if slug, ok := matchPrefix(path, "/showcase/"); ok {
			c.Params = append(c.Params, gin.Param{Key: "slug", Value: slug})
			t.handler.ShowcaseDetail(c)
			return
		}
		if slug, ok := matchPrefix(path, "/blog/"); ok {
			c.Params = append(c.Params, gin.Param{Key: "slug", Value: slug})
			t.handler.BlogPost(c)
			return
		}
	}
	t.BaseTheme.ServeHTTP(c)
}

// matchPrefix checks if path starts with prefix and returns the remaining slug segment.
// Returns false if the path has additional segments beyond the slug.
func matchPrefix(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	slug := strings.TrimPrefix(path, prefix)
	slug = strings.TrimSuffix(slug, "/")
	if slug == "" || strings.Contains(slug, "/") {
		return "", false
	}
	return slug, true
}

// --- Templates ---

func (t *ModernCompanyTheme) TemplateFuncs() template.FuncMap {
	return t.BaseFuncMap()
}

func (t *ModernCompanyTheme) TemplateDir() string {
	return filepath.Join(t.ThemeDir, "templates")
}

func (t *ModernCompanyTheme) StaticDir() string {
	return filepath.Join(t.ThemeDir, "static")
}

// --- Demo Data ---

// DemoSeedPath returns the path to the bundled demo seed.toml.
func (t *ModernCompanyTheme) DemoSeedPath() string {
	return filepath.Join(t.ThemeDir, "demo", "data", "seed.toml")
}

// --- Settings ---

// SettingsTemplatePath returns the path to the admin settings template.
func (t *ModernCompanyTheme) SettingsTemplatePath() string {
	return filepath.Join(t.ThemeDir, "templates", "admin", "theme_settings.tmpl")
}
