// Package communa implements "Communa", a friendly community and social-network
// theme for GoPress. It renders a BuddyPress-style surface — a widget-rich
// three-column homepage, a member directory, community groups, and discussion
// forums — entirely on top of the generic CMS primitives (contents, meta, terms,
// options, menus) and core extension points (hooks, SEO, i18n, media). It never
// imports or couples to any specific plugin.
package communa

import (
	"html/template"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/0xmattg/go-press/core"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

func init() {
	core.RegisterTheme("communa", func(engine *core.Engine, themeDir string) coreTheme.Theme {
		return New(engine, themeDir)
	})
}

// CommunaTheme is the GoPress theme for community and social sites. It embeds
// BaseTheme to inherit runtime engine capabilities (URL rewrite, template
// hierarchy, SEO, archive/single rendering) and only adds the community-specific
// homepage, about, and contact routes plus data assembly.
type CommunaTheme struct {
	coreTheme.BaseTheme
	engine  *core.Engine
	handler *Handler
}

// Compile-time interface checks.
var _ coreTheme.Theme = (*CommunaTheme)(nil)
var _ coreTheme.DemoDataProvider = (*CommunaTheme)(nil)
var _ coreTheme.SettingsProvider = (*CommunaTheme)(nil)

// New creates a CommunaTheme backed by the full engine.
func New(engine *core.Engine, themeDir string) *CommunaTheme {
	svc := NewPageService(engine)
	handler := NewHandler(svc, themeDir, engine.I18n)
	t := &CommunaTheme{engine: engine, handler: handler}

	t.InitBase(engine, themeDir, themeTOML, themeFuncMap(engine.SiteLocation()))

	// Community listings breathe better in a 12-item grid than the core default.
	t.SetArchivePageSize("post", 12)
	t.SetArchivePageSize("member", 15)
	t.SetArchivePageSize("group", 12)
	t.SetArchivePageSize("discussion", 15)

	// Custom static-page routes take priority over rewrite-engine resolution.
	t.AddRoute("GET", "/", t.handler.Home)
	t.AddRoute("GET", "/about", t.handler.About)
	t.AddRoute("GET", "/contact", t.handler.Contact)
	t.AddRoute("POST", "/contact", t.handler.ContactSubmit)

	if err := t.handler.LoadPageTemplates(t); err != nil {
		panic(err)
	}
	t.LoadTemplates(t)
	return t
}

// NewWithDB creates a CommunaTheme with only a database connection. Useful for
// tests and standalone usage without a full engine.
func NewWithDB(db *gorm.DB, themeDir string) *CommunaTheme {
	svc := NewPageServiceDB(db)
	handler := NewHandler(svc, themeDir, nil)
	t := &CommunaTheme{handler: handler}
	t.InitBase(nil, themeDir, themeTOML, themeFuncMap(time.Local))
	t.SetArchivePageSize("post", 12)
	t.AddRoute("GET", "/", t.handler.Home)
	t.AddRoute("GET", "/about", t.handler.About)
	t.AddRoute("GET", "/contact", t.handler.Contact)
	t.AddRoute("POST", "/contact", t.handler.ContactSubmit)
	return t
}

// themeFuncMap returns the theme-specific template helpers merged into the shared
// BaseFuncMap, so every template loader (page bundle + hierarchy) sees them.
func themeFuncMap(loc *time.Location) template.FuncMap {
	if loc == nil {
		loc = time.Local
	}
	return template.FuncMap{
		"formatDateTime": func(tm *time.Time) string {
			if tm == nil {
				return ""
			}
			return tm.In(loc).Format("January 2, 2006")
		},
		"stripTags":         stripHTMLTags,
		"initials":          initials,
		"settingIntBetween": settingIntBetween,
		"add":               func(a, b int) int { return a + b },
	}
}

// --- Lifecycle ---

// Setup registers menu locations and translatable option keys.
func (t *CommunaTheme) Setup(app coreTheme.App) {
	if t.engine == nil {
		return
	}
	t.engine.Menus.RegisterLocation("header", "Header Navigation")
	t.engine.Menus.RegisterLocation("footer", "Footer Navigation")
	registerTranslatableOptions()
}

// ServeHTTP delegates all front-end routing to BaseTheme. Archives and singles
// for member/group/discussion/post resolve from the content registry + rewrite
// slugs declared in theme.toml.
func (t *CommunaTheme) ServeHTTP(c *gin.Context) {
	t.BaseTheme.ServeHTTP(c)
}

// --- Templates ---

func (t *CommunaTheme) TemplateFuncs() template.FuncMap { return t.BaseFuncMap() }

func (t *CommunaTheme) TemplateDir() string { return filepath.Join(t.ThemeDir, "templates") }

func (t *CommunaTheme) StaticDir() string { return filepath.Join(t.ThemeDir, "static") }

// --- Demo Data ---

// DemoSeedPath returns the path to the bundled demo seed.toml.
func (t *CommunaTheme) DemoSeedPath() string {
	return filepath.Join(t.ThemeDir, "demo", "data", "seed.toml")
}

// --- Settings ---

// SettingsTemplatePath returns the path to the admin settings template.
func (t *CommunaTheme) SettingsTemplatePath() string {
	return filepath.Join(t.ThemeDir, "templates", "admin", "theme_settings.tmpl")
}
