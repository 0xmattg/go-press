package monojournal

import (
	"html/template"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/0xmattg/go-press/core"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

func init() {
	core.RegisterTheme("mono-journal", func(engine *core.Engine, themeDir string) coreTheme.Theme {
		return New(engine, themeDir)
	})
}

// Theme is a focused personal publishing theme built on GoPress core content.
type Theme struct {
	coreTheme.BaseTheme
	handler *Handler
}

var _ coreTheme.Theme = (*Theme)(nil)
var _ coreTheme.DemoDataProvider = (*Theme)(nil)
var _ coreTheme.SettingsProvider = (*Theme)(nil)

// New creates the runtime theme with request-scoped content and SEO services.
func New(engine *core.Engine, themeDir string) *Theme {
	t := &Theme{}
	service := NewPageService(engine)
	t.handler = NewHandler(service, themeDir, engine.I18n)
	t.handler.SetPublicRuntime(engine.CommentService(), engine)
	t.InitBase(engine, themeDir, themeTOML, themeFuncMap(engine.SiteLocation()))
	t.AddRoute("GET", "/", t.handler.Home)
	t.AddRoute("GET", "/search", t.handler.Search)
	t.AddRoute("GET", "/profile", t.handler.Profile)
	t.AddRoute("POST", "/comments", t.handler.CommentCreate)
	t.LoadTemplates(t)
	if err := t.handler.LoadPageTemplates(t); err != nil {
		panic(err)
	}
	return t
}

// NewWithDB creates a lightweight theme instance for tests and tooling.
func NewWithDB(db *gorm.DB, themeDir string) *Theme {
	t := &Theme{}
	service := NewPageServiceDB(db)
	t.handler = NewHandler(service, themeDir, nil)
	t.InitBase(nil, themeDir, themeTOML, themeFuncMap(nil))
	t.AddRoute("GET", "/", t.handler.Home)
	t.AddRoute("GET", "/search", t.handler.Search)
	t.AddRoute("GET", "/profile", t.handler.Profile)
	t.AddRoute("POST", "/comments", t.handler.CommentCreate)
	return t
}

func (t *Theme) Setup(_ coreTheme.App) {
	registerTranslatableOptions()
}

func (t *Theme) ServeHTTP(c *gin.Context) { t.BaseTheme.ServeHTTP(c) }

func (t *Theme) TemplateFuncs() template.FuncMap { return t.BaseFuncMap() }
func (t *Theme) TemplateDir() string             { return filepath.Join(t.ThemeDir, "templates") }
func (t *Theme) StaticDir() string               { return filepath.Join(t.ThemeDir, "static") }

func (t *Theme) DemoSeedPath() string {
	return filepath.Join(t.ThemeDir, "demo", "data", "seed.toml")
}

func (t *Theme) SettingsTemplatePath() string {
	return filepath.Join(t.ThemeDir, "templates", "admin", "theme_settings.tmpl")
}
