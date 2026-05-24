package axisform

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
	core.RegisterTheme("axis-form", func(engine *core.Engine, themeDir string) coreTheme.Theme {
		return New(engine, themeDir)
	})
}

// AxisFormTheme is a GoPress theme for architecture and interior design studios.
// It embeds BaseTheme to gain runtime engine capabilities (rewrite, template hierarchy, SEO).
type AxisFormTheme struct {
	coreTheme.BaseTheme
	engine  *core.Engine
	handler *Handler
}

// Compile-time interface check.
var _ coreTheme.Theme = (*AxisFormTheme)(nil)
var _ coreTheme.DemoDataProvider = (*AxisFormTheme)(nil)
var _ coreTheme.SettingsProvider = (*AxisFormTheme)(nil)

// New creates a new AxisFormTheme.
func New(engine *core.Engine, themeDir string) *AxisFormTheme {
	svc := NewPageService(engine)
	handler := NewHandler(svc, themeDir, engine.Menus, engine.I18n)
	t := &AxisFormTheme{
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
			return tm.In(engine.SiteLocation()).Format("January 2, 2006")
		},
		"stripTags": func(s string) string {
			return strings.TrimSpace(reHTMLTags.ReplaceAllString(s, " "))
		},
		"add": func(a, b int) int { return a + b },
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

// NewWithDB creates an AxisFormTheme with only a database connection.
// Useful for testing or standalone usage without a full Engine.
func NewWithDB(db *gorm.DB, themeDir string) *AxisFormTheme {
	svc := NewPageServiceDB(db)
	handler := NewHandler(svc, themeDir, nil, nil)
	t := &AxisFormTheme{
		handler: handler,
	}
	t.InitBase(nil, themeDir, template.FuncMap{
		"formatDateTime": func(tm *time.Time) string {
			if tm == nil {
				return ""
			}
			return tm.Format("January 2, 2006")
		},
		"stripTags": func(s string) string { return strings.TrimSpace(reHTMLTags.ReplaceAllString(s, " ")) },
		"add":       func(a, b int) int { return a + b },
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
	t.AddRoute("GET", "/contact", t.handler.Contact)
	t.AddRoute("POST", "/contact", t.handler.ContactSubmit)
	return t
}

// --- Metadata ---

func (t *AxisFormTheme) Name() string    { return "Axis Form" }
func (t *AxisFormTheme) Version() string { return "1.0.0" }
func (t *AxisFormTheme) Description() string {
	return "A minimal architecture studio theme with portfolio, services, process, and CMS demo data."
}
func (t *AxisFormTheme) Author() string { return "GoPress Team" }

// --- Lifecycle ---

// Setup registers theme runtime hooks and menu locations.
func (t *AxisFormTheme) Setup(app coreTheme.App) {
	if t.engine == nil {
		return
	}

	t.engine.Menus.RegisterLocation("header", "顶部导航")
	t.engine.Menus.RegisterLocation("footer", "底部导航")

	// Register translatable option keys (text-based settings that need translation)
	registerTranslatableOptions()
}

// ServeHTTP delegates frontend routing to BaseTheme. Content archives and singles are resolved from the active content registry and theme.toml rewrite slugs.
func (t *AxisFormTheme) ServeHTTP(c *gin.Context) {
	t.BaseTheme.ServeHTTP(c)
}

// --- Templates ---

func (t *AxisFormTheme) TemplateFuncs() template.FuncMap {
	return t.BaseFuncMap()
}

func (t *AxisFormTheme) TemplateDir() string {
	return filepath.Join(t.ThemeDir, "templates")
}

func (t *AxisFormTheme) StaticDir() string {
	return filepath.Join(t.ThemeDir, "static")
}

// --- Demo Data ---

// DemoSeedPath returns the path to the bundled demo seed.toml.
func (t *AxisFormTheme) DemoSeedPath() string {
	return filepath.Join(t.ThemeDir, "demo", "data", "seed.toml")
}

// --- Settings ---

// SettingsTemplatePath returns the path to the admin settings template.
func (t *AxisFormTheme) SettingsTemplatePath() string {
	return filepath.Join(t.ThemeDir, "templates", "admin", "theme_settings.tmpl")
}
