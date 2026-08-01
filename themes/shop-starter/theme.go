// Package shopstarter provides the public lightweight Commerce reference theme.
// It only depends on Core's theme, content, i18n, and hook contracts; Commerce
// contributes storefront fragments and full pages through those generic seams.
package shopstarter

import (
	"html/template"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"go-press/core"
	"go-press/core/option"
	coreTheme "go-press/core/theme"
	"go-press/pkg/logger"
)

func init() {
	core.RegisterTheme("shop-starter", func(engine *core.Engine, themeDir string) coreTheme.Theme {
		return New(engine, themeDir)
	})
}

// ShopStarter is a compact one-page shop presentation backed by BaseTheme.
type ShopStarter struct {
	coreTheme.BaseTheme
	app         coreTheme.App
	pageService *PageService
}

var (
	_ coreTheme.Theme            = (*ShopStarter)(nil)
	_ coreTheme.DemoDataProvider = (*ShopStarter)(nil)
	_ coreTheme.SettingsProvider = (*ShopStarter)(nil)
)

// New initializes the theme from its embedded manifest and shared Core services.
func New(engine *core.Engine, themeDir string) *ShopStarter {
	var app coreTheme.App
	if engine != nil {
		app = engine
	}
	t := &ShopStarter{app: app, pageService: NewPageService(app)}
	t.InitBase(engine, themeDir, themeTOML, extraFuncs(t))
	t.LoadTemplates(t)
	t.AddRoute(http.MethodGet, "/", t.home)
	return t
}

// Setup declares menu locations and text settings through Core registries.
func (t *ShopStarter) Setup(app coreTheme.App) {
	t.app = app
	t.pageService = NewPageService(app)
	registerTranslatableOptions()
	if app == nil || app.MenuStore() == nil {
		return
	}
	app.MenuStore().RegisterLocation("header", "Header Navigation")
	app.MenuStore().RegisterLocation("footer", "Footer Navigation")
}

func (t *ShopStarter) home(c *gin.Context) {
	tmpl := t.PageTemplates["home"]
	if tmpl == nil || t.pageService == nil {
		c.String(http.StatusInternalServerError, "home page unavailable")
		return
	}
	data, err := t.pageService.ForRequest(c).GetHomeData()
	if err != nil {
		logger.Error("shop-starter: load home data", "error", err)
		c.String(http.StatusInternalServerError, "internal server error")
		return
	}
	data.Ctx = c
	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "base", data); err != nil {
		logger.Error("shop-starter: render home", "error", err)
	}
}

func (t *ShopStarter) DemoSeedPath() string {
	return filepath.Join(t.ThemeDir, "demo", "data", "seed.toml")
}

func (t *ShopStarter) SettingsTemplatePath() string {
	return filepath.Join(t.ThemeDir, "templates", "admin", "theme_settings.tmpl")
}

func (t *ShopStarter) TemplateFuncs() template.FuncMap { return t.BaseFuncMap() }
func (t *ShopStarter) TemplateDir() string             { return filepath.Join(t.ThemeDir, "templates") }
func (t *ShopStarter) StaticDir() string               { return filepath.Join(t.ThemeDir, "static") }

func extraFuncs(t *ShopStarter) template.FuncMap {
	return template.FuncMap{
		"settingOr": func(c *gin.Context, values map[string]string, key, fallback string) string {
			value := fallback
			if configured := values[key]; configured != "" {
				value = configured
			}
			if t != nil && t.app != nil && t.app.I18nManager() != nil && c != nil && option.IsTranslatable(key) {
				return t.app.I18nManager().TranslateOption(c, key, value)
			}
			return value
		},
		"themeURL": safeThemeURL,
	}
}
