package gopresslanding

import (
	"html/template"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go-press/core"
	coreTheme "go-press/core/theme"
)

func init() {
	core.RegisterTheme("go-press-landing", func(engine *core.Engine, themeDir string) coreTheme.Theme {
		return New(engine, themeDir)
	})
}

// LandingTheme is a single-page tech landing page theme.
type LandingTheme struct {
	coreTheme.BaseTheme
	engine  *core.Engine
	handler *Handler
}

var _ coreTheme.Theme = (*LandingTheme)(nil)
var _ coreTheme.DemoDataProvider = (*LandingTheme)(nil)
var _ coreTheme.SettingsProvider = (*LandingTheme)(nil)

// New creates and initialises the Landing theme.
func New(engine *core.Engine, themeDir string) *LandingTheme {
	svc := NewPageService(engine)
	handler := NewHandler(svc, themeDir, engine.Menus)
	t := &LandingTheme{
		engine:  engine,
		handler: handler,
	}

	t.InitBase(engine, themeDir, DefaultFuncMap(engine.SiteLocation()))
	t.handler.loadTemplates(t.TemplateFuncs())

	// Single-page: only the root route
	t.AddRoute("GET", "/", t.handler.Home)

	t.LoadTemplates(t)
	return t
}

// NewWithDB creates the theme with a bare DB connection (for testing).
func NewWithDB(db *gorm.DB, themeDir string) *LandingTheme {
	svc := NewPageServiceDB(db)
	handler := NewHandler(svc, themeDir, nil)
	t := &LandingTheme{handler: handler}
	t.InitBase(nil, themeDir, DefaultFuncMap(time.UTC))
	t.handler.loadTemplates(t.TemplateFuncs())
	t.AddRoute("GET", "/", t.handler.Home)
	return t
}

func (t *LandingTheme) Name() string    { return "GoPress Landing" }
func (t *LandingTheme) Version() string { return "1.0.0" }
func (t *LandingTheme) Description() string {
	return "A futuristic, single-page tech landing page theme for GoPress."
}
func (t *LandingTheme) Author() string { return "GoPress Team" }

// Setup registers landing-page menu locations.
func (t *LandingTheme) Setup(app coreTheme.App) {
	if t.engine == nil {
		return
	}
	t.engine.Menus.RegisterLocation("header", "顶部导航")
	t.engine.Menus.RegisterLocation("footer", "底部导航")
}

// ServeHTTP delegates all requests to BaseTheme (custom routes → rewrite → fallback).
func (t *LandingTheme) ServeHTTP(c *gin.Context) {
	t.BaseTheme.ServeHTTP(c)
}

func (t *LandingTheme) TemplateFuncs() template.FuncMap { return t.BaseFuncMap() }
func (t *LandingTheme) TemplateDir() string             { return filepath.Join(t.ThemeDir, "templates") }
func (t *LandingTheme) StaticDir() string               { return filepath.Join(t.ThemeDir, "static") }

// DemoSeedPath returns the path to demo seed data.
func (t *LandingTheme) DemoSeedPath() string {
	return filepath.Join(t.ThemeDir, "demo", "data", "seed.toml")
}

// SettingsTemplatePath returns the path to the admin settings template.
func (t *LandingTheme) SettingsTemplatePath() string {
	return filepath.Join(t.ThemeDir, "templates", "admin", "theme_settings.tmpl")
}
