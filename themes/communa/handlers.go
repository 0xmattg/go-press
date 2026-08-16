package communa

import (
	"errors"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/content"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// Handler processes the custom-routed front-end pages (home / about / contact).
type Handler struct {
	pageService *PageService
	templates   map[string]*template.Template
	templateDir string
	i18nMgr     *coreI18n.Manager
}

// NewHandler creates a Handler. Templates are compiled later via
// LoadPageTemplates once the theme funcmap is fully assembled.
func NewHandler(pageService *PageService, themeDir string, i18nMgr *coreI18n.Manager) *Handler {
	return &Handler{
		pageService: pageService,
		templates:   make(map[string]*template.Template),
		templateDir: filepath.Join(themeDir, "templates"),
		i18nMgr:     i18nMgr,
	}
}

// pageNames are the custom-routed pages this handler renders directly.
var pageNames = []string{"home", "about", "contact"}

// LoadPageTemplates compiles the custom-routed page templates through the shared
// bundle loader, so they inherit the same helper surface as the rest of the theme.
func (h *Handler) LoadPageTemplates(t coreTheme.Theme) error {
	bundle, err := coreTheme.LoadPageBundle(t, pageNames)
	if err != nil {
		return err
	}
	h.templates = bundle
	log.Printf("[communa] Loaded %d custom page templates", len(bundle))
	return nil
}

func (h *Handler) render(c *gin.Context, page string, data interface{}) {
	tmpl, ok := h.templates[page]
	if !ok {
		c.String(http.StatusInternalServerError, "Template not found: "+page)
		return
	}

	type ctxSetter interface{ SetCtx(*gin.Context) }
	if s, ok := data.(ctxSetter); ok {
		s.SetCtx(c)
	}

	type settingsHolder interface {
		TranslateSettings(*gin.Context, *coreI18n.Manager)
	}
	if sh, ok := data.(settingsHolder); ok && h.i18nMgr != nil {
		sh.TranslateSettings(c, h.i18nMgr)
	}

	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "base", data); err != nil {
		log.Printf("[communa] Template render error [%s]: %v", page, err)
	}
}

// Home renders the community dashboard homepage.
func (h *Handler) Home(c *gin.Context) {
	data, err := h.pageService.ForRequest(c).GetHomeData()
	if err != nil {
		log.Printf("[communa] Error getting home data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "home", data)
}

// About renders the about page.
func (h *Handler) About(c *gin.Context) {
	data, err := h.pageService.ForRequest(c).GetAboutData()
	if err != nil {
		log.Printf("[communa] Error getting about data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "about", data)
}

// Contact renders the contact page.
func (h *Handler) Contact(c *gin.Context) {
	data, err := h.pageService.GetContactData()
	if err != nil {
		log.Printf("[communa] Error getting contact data: %v", err)
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
		data.Error = "missing"
		h.render(c, "contact", data)
		return
	}
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		data, _ := h.pageService.GetContactData()
		data.Error = "email"
		h.render(c, "contact", data)
		return
	}

	if err := h.pageService.SubmitContact(c, name, email, phone, message); err != nil {
		log.Printf("[communa] Error saving contact message: %v", err)
		data, _ := h.pageService.GetContactData()
		if errors.Is(err, content.ErrContactMessageRateLimited) {
			data.Error = "rate"
		} else {
			data.Error = "failed"
		}
		h.render(c, "contact", data)
		return
	}

	data, _ := h.pageService.GetContactData()
	data.Success = true
	h.render(c, "contact", data)
}
