package gopresslanding

import (
	"html/template"
	"log"
	"net/http"
	"path/filepath"

	"go-press/core/menu"

	"github.com/gin-gonic/gin"
)

// Handler processes all front-end HTTP requests for the Landing theme.
type Handler struct {
	pageService *PageService
	templates   map[string]*template.Template
	templateDir string
	menuStore   *menu.Store
}

// NewHandler creates a Handler and compiles all page templates.
func NewHandler(pageService *PageService, themeDir string, menuStore *menu.Store) *Handler {
	return &Handler{
		pageService: pageService,
		templates:   make(map[string]*template.Template),
		templateDir: filepath.Join(themeDir, "templates"),
		menuStore:   menuStore,
	}
}

func (h *Handler) loadTemplates(funcMap template.FuncMap) {
	if h.menuStore != nil {
		funcMap["menuByLocation"] = func(location string) *menu.Menu {
			return h.menuStore.GetByLocation(location)
		}
	} else {
		funcMap["menuByLocation"] = func(location string) *menu.Menu { return nil }
	}

	layoutFiles := []string{
		filepath.Join(h.templateDir, "layouts", "base.tmpl"),
		filepath.Join(h.templateDir, "partials", "header.tmpl"),
		filepath.Join(h.templateDir, "partials", "footer.tmpl"),
	}

	pages := []string{"home"}

	for _, page := range pages {
		files := make([]string, len(layoutFiles))
		copy(files, layoutFiles)
		files = append(files, filepath.Join(h.templateDir, "pages", page+".tmpl"))

		tmpl, err := template.New("").Funcs(funcMap).ParseFiles(files...)
		if err != nil {
			log.Fatalf("[go-press-landing] Failed to parse template %s: %v", page, err)
		}
		h.templates[page] = tmpl
	}

	log.Printf("[go-press-landing] Loaded %d page templates", len(h.templates))
}

func (h *Handler) render(c *gin.Context, page string, data interface{}) {
	tmpl, ok := h.templates[page]
	if !ok {
		c.String(http.StatusInternalServerError, "Template not found: "+page)
		return
	}
	if setter, ok := data.(interface{ SetCtx(*gin.Context) }); ok {
		setter.SetCtx(c)
	}

	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "base", data); err != nil {
		log.Printf("[go-press-landing] Template render error [%s]: %v", page, err)
	}
}

// Home renders the single-page landing.
func (h *Handler) Home(c *gin.Context) {
	data, err := h.pageService.GetHomeData()
	if err != nil {
		log.Printf("[go-press-landing] Error getting home data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "home", data)
}
