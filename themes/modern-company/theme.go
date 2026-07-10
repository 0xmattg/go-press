package moderncompany

import (
	"html/template"
	"net/url"
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
			return tm.In(engine.SiteLocation()).Format("January 2, 2006")
		},
		"stripTags": func(s string) string {
			return strings.TrimSpace(reHTMLTags.ReplaceAllString(s, " "))
		},
		"settingIntBetween": settingIntBetween,
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
		"contentMegaMenu": func(c *gin.Context, contentType string) ContentMegaMenu {
			return svc.ContentMegaMenu(c, contentType)
		},
		"contentMegaMenuForURL": func(c *gin.Context, menuURL string) ContentMegaMenu {
			return svc.ContentMegaMenuForURL(c, menuURL)
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
		"stripTags":         func(s string) string { return strings.TrimSpace(reHTMLTags.ReplaceAllString(s, " ")) },
		"settingIntBetween": settingIntBetween,
		"whatsappLink": func(s string) string {
			digits := reNonDigit.ReplaceAllString(s, "")
			if digits == "" {
				return ""
			}
			return "https://wa.me/" + digits
		},
		"contentMegaMenu": func(c *gin.Context, contentType string) ContentMegaMenu {
			return svc.ContentMegaMenu(c, contentType)
		},
		"contentMegaMenuForURL": func(c *gin.Context, menuURL string) ContentMegaMenu {
			return svc.ContentMegaMenuForURL(c, menuURL)
		},
	})
	t.AddRoute("GET", "/", t.handler.Home)
	t.AddRoute("GET", "/about", t.handler.About)
	t.AddRoute("GET", "/contact", t.handler.Contact)
	t.AddRoute("POST", "/contact", t.handler.ContactSubmit)
	return t
}

func isProductArchiveURL(c *gin.Context, engine *core.Engine, raw string) bool {
	return isContentArchiveURL(c, engine, "product", raw)
}

func isContentArchiveURL(c *gin.Context, engine *core.Engine, contentType string, raw string) bool {
	if engine == nil || engine.Rewrite == nil {
		return false
	}
	route := engine.Rewrite.Resolve(normalizeNavPath(c, raw))
	if route == nil || !route.IsArchive {
		return false
	}
	return route.ContentType == strings.TrimSpace(contentType)
}

func normalizeNavPath(c *gin.Context, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		if c == nil || c.Request == nil || !strings.EqualFold(u.Host, c.Request.Host) {
			return ""
		}
	}
	path := u.Path
	if path == "" {
		path = raw
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimRight(path, "/")
	if path == "" {
		path = "/"
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 1 && looksLikeLanguageSegment(parts[0]) {
		path = "/" + strings.Join(parts[1:], "/")
	}
	return path
}

func looksLikeLanguageSegment(segment string) bool {
	if len(segment) == 2 {
		return true
	}
	if len(segment) == 5 && segment[2] == '-' {
		return true
	}
	return false
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

// ServeHTTP delegates frontend routing to BaseTheme. Content archives and singles are resolved from the active content registry and theme.toml rewrite slugs.
func (t *ModernCompanyTheme) ServeHTTP(c *gin.Context) {
	t.BaseTheme.ServeHTTP(c)
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
