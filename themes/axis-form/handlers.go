package axisform

import (
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"go-press/core/content"
	coreI18n "go-press/core/i18n"
	"go-press/core/menu"
	coreTheme "go-press/core/theme"

	"github.com/gin-gonic/gin"
)

// Handler processes all front-end HTTP requests for the Axis Form theme.
type Handler struct {
	pageService *PageService
	templates   map[string]*template.Template
	templateDir string
	menuStore   *menu.Store
	i18nMgr     *coreI18n.Manager
}

// NewHandler creates a Handler. Templates are NOT loaded here — call
// LoadPageTemplates(theme) once the theme has been fully initialized so the
// shared funcmap (BaseFuncMap) includes engine-aware helpers.
func NewHandler(pageService *PageService, themeDir string, menuStore *menu.Store, i18nMgr *coreI18n.Manager) *Handler {
	return &Handler{
		pageService: pageService,
		templates:   make(map[string]*template.Template),
		templateDir: filepath.Join(themeDir, "templates"),
		menuStore:   menuStore,
		i18nMgr:     i18nMgr,
	}
}

// pageNames is the canonical list of pages this theme renders.
var pageNames = []string{
	"home", "about", "products", "services", "showcase", "blog", "contact",
	"product-detail", "service-detail", "showcase-detail", "post-detail", "taxonomy-archive",
}

// LoadPageTemplates compiles all page templates via the core page bundle
// loader. Funcmap is sourced from theme.TemplateFuncs() (== BaseFuncMap),
// so helpers added in core (T, langPrefixURL, currentLang, etc.) are
// uniformly available without per-handler duplication.
func (h *Handler) LoadPageTemplates(t coreTheme.Theme) error {
	bundle, err := coreTheme.LoadPageBundle(t, pageNames)
	if err != nil {
		return err
	}
	h.templates = bundle
	log.Printf("[axis-form] Loaded %d page templates", len(bundle))
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
		log.Printf("[axis-form] Template render error [%s]: %v", page, err)
	}
}

// Home renders the homepage.
func (h *Handler) Home(c *gin.Context) {
	data, err := h.pageService.ForRequest(c).GetHomeData()
	if err != nil {
		log.Printf("[axis-form] Error getting home data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "home", data)
}

// About renders the about page.
func (h *Handler) About(c *gin.Context) {
	data, err := h.pageService.GetAboutData()
	if err != nil {
		log.Printf("[axis-form] Error getting about data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "about", data)
}

// Products renders the products listing page.
func (h *Handler) Products(c *gin.Context) {
	data, err := h.pageService.ForRequest(c).GetProductsData()
	if err != nil {
		log.Printf("[axis-form] Error getting products data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "products", data)
}

// Services renders the services listing page.
func (h *Handler) Services(c *gin.Context) {
	data, err := h.pageService.ForRequest(c).GetServicesData()
	if err != nil {
		log.Printf("[axis-form] Error getting services data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "services", data)
}

// Showcase renders the project showcase page.
func (h *Handler) Showcase(c *gin.Context) {
	data, err := h.pageService.ForRequest(c).GetShowcaseData()
	if err != nil {
		log.Printf("[axis-form] Error getting showcase data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "showcase", data)
}

// Blog renders the blog listing page.
func (h *Handler) Blog(c *gin.Context) {
	category := c.Query("category")
	data, err := h.pageService.ForRequest(c).GetBlogData(category)
	if err != nil {
		log.Printf("[axis-form] Error getting blog data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "blog", data)
}

// Contact renders the contact page.
func (h *Handler) Contact(c *gin.Context) {
	data, err := h.pageService.GetContactData()
	if err != nil {
		log.Printf("[axis-form] Error getting contact data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "contact", data)
}

// ContactSubmit handles contact form submission.
func (h *Handler) ContactSubmit(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	email := strings.TrimSpace(c.PostForm("email"))
	phone := strings.TrimSpace(c.PostForm("phone"))
	message := strings.TrimSpace(c.PostForm("message"))

	if name == "" || email == "" || message == "" {
		data, _ := h.pageService.GetContactData()
		data.Error = "Please fill in all required fields."
		h.render(c, "contact", data)
		return
	}

	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		data, _ := h.pageService.GetContactData()
		data.Error = "Please provide a valid email address."
		h.render(c, "contact", data)
		return
	}

	if err := h.pageService.SubmitContact(c, name, email, phone, message); err != nil {
		log.Printf("[axis-form] Error saving contact message: %v", err)
		data, _ := h.pageService.GetContactData()
		if errors.Is(err, content.ErrContactMessageRateLimited) {
			data.Error = "Too many messages were submitted from your network. Please try again later."
		} else {
			data.Error = "Failed to send message. Please try again."
		}
		h.render(c, "contact", data)
		return
	}

	data, _ := h.pageService.GetContactData()
	data.Success = true
	data.Title = "Message Sent"
	h.render(c, "contact", data)
}

// ProductDetail renders a single product page.
func (h *Handler) ProductDetail(c *gin.Context) {
	slug := c.Param("slug")
	data, err := h.pageService.ForRequest(c).GetProductDetail(slug)
	if err != nil || data == nil {
		c.String(http.StatusNotFound, "Product not found")
		return
	}
	h.render(c, "product-detail", data)
}

// ServiceDetail renders a single service page.
func (h *Handler) ServiceDetail(c *gin.Context) {
	slug := c.Param("slug")
	data, err := h.pageService.ForRequest(c).GetServiceDetail(slug)
	if err != nil || data == nil {
		c.String(http.StatusNotFound, "Service not found")
		return
	}
	h.render(c, "service-detail", data)
}

// ShowcaseDetail renders a single showcase project page.
func (h *Handler) ShowcaseDetail(c *gin.Context) {
	slug := c.Param("slug")
	data, err := h.pageService.ForRequest(c).GetShowcaseDetail(slug)
	if err != nil || data == nil {
		c.String(http.StatusNotFound, "Project not found")
		return
	}
	h.render(c, "showcase-detail", data)
}

// BlogPost renders a single blog post page.
func (h *Handler) BlogPost(c *gin.Context) {
	slug := c.Param("slug")
	data, err := h.pageService.ForRequest(c).GetPostDetail(slug)
	if err != nil || data == nil {
		c.String(http.StatusNotFound, "Post not found")
		return
	}
	h.render(c, "post-detail", data)
}

// TaxonomyArchive renders the taxonomy archive page (/category/{slug} or /tag/{slug}).
func (h *Handler) TaxonomyArchive(c *gin.Context, taxonomyType, termSlug string) {
	data, err := h.pageService.ForRequest(c).GetTaxonomyArchive(taxonomyType, termSlug)
	if err != nil || data == nil {
		c.String(http.StatusNotFound, "Not found")
		return
	}
	h.render(c, "taxonomy-archive", data)
}

// Health returns a simple health check response.
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   fmt.Sprintf("%v", time.Now().Format(time.RFC3339)),
	})
}
