package financialnews

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
	core.RegisterTheme("financial-news", func(engine *core.Engine, themeDir string) coreTheme.Theme {
		return New(engine, themeDir)
	})
}

// FinancialNewsTheme is a GoPress theme for financial news portals.
// It embeds BaseTheme to gain runtime engine capabilities.
type FinancialNewsTheme struct {
	coreTheme.BaseTheme
	engine  *core.Engine
	handler *Handler
}

var _ coreTheme.Theme = (*FinancialNewsTheme)(nil)

// New creates a new FinancialNewsTheme.
func New(engine *core.Engine, themeDir string) *FinancialNewsTheme {
	svc := NewPageService(engine)
	handler := NewHandler(svc, themeDir, engine.Menus, engine.I18n)
	t := &FinancialNewsTheme{
		engine:  engine,
		handler: handler,
	}

	// Initialize BaseTheme with engine capabilities and theme-specific funcs
	t.InitBase(engine, themeDir, template.FuncMap{
		"formatDateTime": func(tm *time.Time) string {
			if tm == nil {
				return ""
			}
			return tm.Format("2006年01月02日 15:04")
		},
		"timeAgo": func(tm *time.Time) string {
			if tm == nil {
				return ""
			}
			d := time.Since(*tm)
			switch {
			case d < time.Minute:
				return "刚刚"
			case d < time.Hour:
				m := int(d.Minutes())
				return strings.TrimRight(strings.TrimRight(
					time.Duration(m).String(), "0"), ".") + " 分钟前"
			case d < 24*time.Hour:
				h := int(d.Hours())
				return strings.TrimRight(strings.TrimRight(
					time.Duration(h).String(), "0"), ".") + " 小时前"
			default:
				return tm.Format("01-02 15:04")
			}
		},
	})

	// Register custom static-page routes
	t.AddRoute("GET", "/", t.handler.Home)
	t.AddRoute("GET", "/articles", t.handler.Articles)
	t.AddRoute("GET", "/blog", t.handler.Articles) // /blog also lists posts
	t.AddRoute("GET", "/market", t.handler.Market)
	t.AddRoute("GET", "/analysis", t.handler.Analysis)
	t.AddRoute("GET", "/about", t.handler.About)

	// Compile per-page templates via core's shared bundle loader.
	if err := t.handler.LoadPageTemplates(t); err != nil {
		panic(err)
	}

	// Load templates via core TemplateEngine
	t.LoadTemplates(t)

	return t
}

// NewWithDB creates a FinancialNewsTheme with only a database connection.
func NewWithDB(db *gorm.DB, themeDir string) *FinancialNewsTheme {
	svc := NewPageServiceDB(db)
	handler := NewHandler(svc, themeDir, nil, nil)
	t := &FinancialNewsTheme{
		handler: handler,
	}
	t.InitBase(nil, themeDir, nil)
	t.AddRoute("GET", "/", t.handler.Home)
	t.AddRoute("GET", "/articles", t.handler.Articles)
	t.AddRoute("GET", "/blog", t.handler.Articles)
	t.AddRoute("GET", "/market", t.handler.Market)
	t.AddRoute("GET", "/analysis", t.handler.Analysis)
	t.AddRoute("GET", "/about", t.handler.About)
	return t
}

// --- Metadata ---

func (t *FinancialNewsTheme) Name() string    { return "Financial News" }
func (t *FinancialNewsTheme) Version() string { return "1.0.0" }
func (t *FinancialNewsTheme) Description() string {
	return "A fast, data-driven financial news portal theme for GoPress."
}
func (t *FinancialNewsTheme) Author() string { return "GoPress Team" }

// --- Lifecycle ---

func (t *FinancialNewsTheme) Setup(app coreTheme.App) {
	if t.engine == nil {
		return
	}

	t.engine.Menus.RegisterLocation("top-nav", "顶部导航")
	t.engine.Menus.RegisterLocation("sidebar", "侧边栏")
	t.engine.Menus.RegisterLocation("footer", "底部导航")
}

// ServeHTTP handles frontend requests.
// Blog post detail routes (/blog/:slug) are intercepted here so they use
// the theme's styled template instead of the framework fallback.
func (t *FinancialNewsTheme) ServeHTTP(c *gin.Context) {
	path := c.Request.URL.Path
	if c.Request.Method == "GET" {
		if slug, ok := matchPrefix(path, "/blog/"); ok {
			t.handler.BlogPost(c, slug)
			return
		}
	}
	t.BaseTheme.ServeHTTP(c)
}

// matchPrefix checks if path starts with prefix and returns the remaining slug segment.
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

func (t *FinancialNewsTheme) TemplateFuncs() template.FuncMap {
	return t.BaseFuncMap()
}

func (t *FinancialNewsTheme) TemplateDir() string {
	return filepath.Join(t.ThemeDir, "templates")
}

func (t *FinancialNewsTheme) StaticDir() string {
	return filepath.Join(t.ThemeDir, "static")
}
