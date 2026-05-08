package financialnews

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"time"

	coreI18n "go-press/core/i18n"
	"go-press/core/menu"
	coreTheme "go-press/core/theme"

	"github.com/gin-gonic/gin"
)

// Handler processes all front-end HTTP requests for the Financial News theme.
type Handler struct {
	pageService *PageService
	templates   map[string]*template.Template
	templateDir string
	menuStore   *menu.Store
	i18nMgr     *coreI18n.Manager
}

// NewHandler creates a Handler. Templates are loaded later via
// LoadPageTemplates(theme) so the central core funcmap is in effect.
func NewHandler(pageService *PageService, themeDir string, menuStore *menu.Store, i18nMgr *coreI18n.Manager) *Handler {
	return &Handler{
		pageService: pageService,
		templates:   make(map[string]*template.Template),
		templateDir: filepath.Join(themeDir, "templates"),
		menuStore:   menuStore,
		i18nMgr:     i18nMgr,
	}
}

// pageNames is the canonical page list for this theme.
var pageNames = []string{"home", "articles", "market", "analysis", "about", "post_detail"}

// LoadPageTemplates compiles per-page templates via the core bundle loader,
// inheriting the unified BaseFuncMap (T, langPrefixURL, currentLang, etc.).
func (h *Handler) LoadPageTemplates(t coreTheme.Theme) error {
	bundle, err := coreTheme.LoadPageBundle(t, pageNames)
	if err != nil {
		return err
	}
	h.templates = bundle
	log.Printf("[financial-news] Loaded %d page templates", len(bundle))
	return nil
}

func (h *Handler) render(c *gin.Context, page string, data interface{}) {
	tmpl, ok := h.templates[page]
	if !ok {
		c.String(http.StatusInternalServerError, "Template not found: "+page)
		return
	}

	// Inject gin.Context so templates can call {{T .Ctx "key"}}
	type ctxSetter interface{ SetCtx(*gin.Context) }
	if s, ok := data.(ctxSetter); ok {
		s.SetCtx(c)
	}

	// Translate settings map for current language (non-default → use DB overrides)
	type settingsHolder interface {
		TranslateSettings(*gin.Context, *coreI18n.Manager)
	}
	if sh, ok := data.(settingsHolder); ok && h.i18nMgr != nil {
		sh.TranslateSettings(c, h.i18nMgr)
	}

	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "base", data); err != nil {
		log.Printf("[financial-news] Template render error [%s]: %v", page, err)
	}
}

// Home renders the homepage with featured articles, market ticker, and latest analysis.
func (h *Handler) Home(c *gin.Context) {
	data, err := h.pageService.ForRequest(c).GetHomeData()
	if err != nil {
		log.Printf("[financial-news] Error getting home data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "home", data)
}

// Articles renders the news articles listing.
func (h *Handler) Articles(c *gin.Context) {
	category := c.Query("category")
	data, err := h.pageService.ForRequest(c).GetArticlesData(category)
	if err != nil {
		log.Printf("[financial-news] Error getting articles data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "articles", data)
}

// Market renders the market updates / ticker page.
func (h *Handler) Market(c *gin.Context) {
	data, err := h.pageService.ForRequest(c).GetMarketData()
	if err != nil {
		log.Printf("[financial-news] Error getting market data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "market", data)
}

// Analysis renders the deep analysis listing.
func (h *Handler) Analysis(c *gin.Context) {
	data, err := h.pageService.ForRequest(c).GetAnalysisData()
	if err != nil {
		log.Printf("[financial-news] Error getting analysis data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "analysis", data)
}

// About renders the about page.
func (h *Handler) About(c *gin.Context) {
	data, err := h.pageService.ForRequest(c).GetAboutData()
	if err != nil {
		log.Printf("[financial-news] Error getting about data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "about", data)
}

// BlogPost renders a single blog post detail page.
func (h *Handler) BlogPost(c *gin.Context, slug string) {
	data, err := h.pageService.ForRequest(c).GetPostDetailData(slug)
	if err != nil {
		log.Printf("[financial-news] Error getting post detail: %v", err)
		c.String(http.StatusNotFound, "Post not found")
		return
	}
	h.render(c, "post_detail", data)
}

// Health returns a simple health check response.
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   fmt.Sprintf("%v", time.Now().Format(time.RFC3339)),
	})
}
