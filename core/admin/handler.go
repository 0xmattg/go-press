package admin

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-press/core/content"
	"go-press/core/hook"
	coreI18n "go-press/core/i18n"
	"go-press/version"

	"github.com/gin-gonic/gin"
)

// ThemeDisplayInfo holds info about a theme for admin display.
type ThemeDisplayInfo struct {
	Name         string
	Slug         string
	Version      string
	Description  string
	Author       string
	Active       bool
	HasDemoData  bool // theme provides bundled demo seed data
	DemoImported bool // demo data has already been imported
	HasSettings  bool // theme provides a settings page
}

// ThemeManager provides theme switching callbacks to the admin handler.
type ThemeManager struct {
	SwitchFn           func(name string) error
	ActiveFn           func() string
	AvailableFn        func() []ThemeDisplayInfo
	ImportDemoFn       func(slug string) error  // import demo data for a theme
	SettingsTemplateFn func(slug string) string // returns settings template path
	LocaleCatalogFn    func(slug string) *coreI18n.Catalog
}

// CacheManagerInfo holds cache status for admin display.
type CacheManagerInfo struct {
	L1Type string // "memory" or "noop"
	L2Type string // "redis" or "noop"
	IsNoop bool
}

// CacheCallbacks provides cache management callbacks to the admin handler.
type CacheCallbacks struct {
	StatusFn    func() CacheManagerInfo
	FlushAllFn  func()
	FlushPageFn func()
	FlushFragFn func()
}

// RedirectInfo holds a redirect rule for admin display.
type RedirectInfo struct {
	ID         uint
	SourcePath string
	TargetPath string
	StatusCode int
	HitCount   int64
}

// RedirectCallbacks provides redirect management callbacks to the admin handler.
type RedirectCallbacks struct {
	AllFn    func() []RedirectInfo
	AddFn    func(source, target string, code int) error
	RemoveFn func(source string) error
}

// PluginInfo holds plugin metadata for admin display.
type PluginInfo struct {
	Name        string
	DisplayName string
	Slug        string
	Version     string
	Description string
	Active      bool
	HasSettings bool
}

// PluginCallbacks provides plugin management callbacks to the admin handler.
type PluginCallbacks struct {
	AllFn              func() []PluginInfo
	ActivateFn         func(name string) error
	DeactivateFn       func(name string) error
	SettingsTemplateFn func(slug string) string                      // returns settings template path
	SettingsDataFn     func(slug string) map[string]interface{}      // extra template data for plugin settings page
	SettingsSaveFn     func(slug string, settings map[string]string) // hook called after plugin settings are saved
	LocaleCatalogFn    func(slug string) *coreI18n.Catalog
}

// SitemapCallbacks provides sitemap generation callbacks to the admin handler.
type SitemapCallbacks struct {
	GenerateFn func() (int, error) // generates sitemap.xml, returns URL count
}

// MenuLocationInfo holds a menu location for admin display.
type MenuLocationInfo struct {
	Name  string
	Label string
}

// MenuInfo holds menu data for admin display.
type MenuInfo struct {
	ID       uint
	Name     string
	Location string
	Items    []MenuItemInfo
}

// MenuItemInfo holds menu item data for admin display.
type MenuItemInfo struct {
	ID        uint           `json:"id"`
	ParentID  *uint          `json:"parent_id"`
	Title     string         `json:"title"`
	URL       string         `json:"url"`
	Target    string         `json:"target"`
	ContentID *uint          `json:"content_id"`
	SortOrder int            `json:"sort_order"`
	Children  []MenuItemInfo `json:"children"`
}

// MenuCallbacks provides menu management callbacks to the admin handler.
type MenuCallbacks struct {
	AllFn       func() ([]MenuInfo, error)
	GetByIDFn   func(id uint) (*MenuInfo, error)
	CreateFn    func(name, location string) error
	UpdateFn    func(id uint, name, location string) error
	DeleteFn    func(id uint) error
	SaveItemsFn func(menuID uint, items []MenuItemInfo) error
	LocationsFn func() []MenuLocationInfo
	ReloadFn    func()
}

// Handler manages all admin HTTP endpoints.
type Handler struct {
	svc               *Service
	registry          *content.Registry
	templates         map[string]*template.Template
	funcMap           template.FuncMap
	tmplDir           string
	hooks             *hook.Bus
	themeManager      *ThemeManager
	cacheCallbacks    *CacheCallbacks
	redirectCallbacks *RedirectCallbacks
	pluginCallbacks   *PluginCallbacks
	sitemapCallbacks  *SitemapCallbacks
	menuCallbacks     *MenuCallbacks
}

// SetHookBus injects the engine's hook bus so handlers can invoke plugin filters.
func (h *Handler) SetHookBus(b *hook.Bus) {
	h.hooks = b
}

// SetThemeManager injects the theme management callbacks.
func (h *Handler) SetThemeManager(tm *ThemeManager) {
	h.themeManager = tm
}

// SetCacheCallbacks injects cache management callbacks.
func (h *Handler) SetCacheCallbacks(cc *CacheCallbacks) {
	h.cacheCallbacks = cc
}

// SetRedirectCallbacks injects redirect management callbacks.
func (h *Handler) SetRedirectCallbacks(rc *RedirectCallbacks) {
	h.redirectCallbacks = rc
}

// SetPluginCallbacks injects plugin management callbacks.
func (h *Handler) SetPluginCallbacks(pc *PluginCallbacks) {
	h.pluginCallbacks = pc
}

// SetSitemapCallbacks injects sitemap generation callbacks.
func (h *Handler) SetSitemapCallbacks(sc *SitemapCallbacks) {
	h.sitemapCallbacks = sc
}

// SetMenuCallbacks injects menu management callbacks.
func (h *Handler) SetMenuCallbacks(mc *MenuCallbacks) {
	h.menuCallbacks = mc
}

// invalidatePageCache flushes page cache after data mutations.
// Safe to call even if cache is not configured (noop).
func (h *Handler) invalidatePageCache() {
	if h.cacheCallbacks != nil && h.cacheCallbacks.FlushPageFn != nil {
		h.cacheCallbacks.FlushPageFn()
	}
}

// NewHandler creates the admin Handler and compiles templates.
func NewHandler(svc *Service, registry *content.Registry, templateDir string) *Handler {
	h := &Handler{
		svc:      svc,
		registry: registry,
		tmplDir:  templateDir,
	}
	h.buildFuncMap()
	h.loadTemplates(templateDir)
	return h
}

// resourceMap translates old-style admin resource names to core RBAC resources.
var resourceMap = map[string]string{
	"post":            "content",
	"contact_message": "content",
	"category":        "taxonomy",
	"tag":             "taxonomy",
}

func mapResource(res string) string {
	if mapped, ok := resourceMap[res]; ok {
		return mapped
	}
	return res
}

func (h *Handler) mapResource(res string) string {
	if h.registry != nil {
		if h.registry.GetType(res) != nil {
			return "content"
		}
		if h.registry.GetTaxonomy(res) != nil {
			return "taxonomy"
		}
	}
	return mapResource(res)
}

func (h *Handler) buildFuncMap() {
	rbac := h.svc.rbac
	h.funcMap = template.FuncMap{
		"formatDate": func(t interface{}) string {
			switch v := t.(type) {
			case time.Time:
				return v.Format("2006-01-02 15:04")
			case *time.Time:
				if v == nil {
					return ""
				}
				return v.Format("2006-01-02 15:04")
			default:
				return ""
			}
		},
		"formatDateInput": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02T15:04")
		},
		"timeAgoCN": func(t interface{}) string {
			return formatAdminTimeAgo("zh-CN", t)
		},
		"timeAgo": func(lang string, t interface{}) string {
			return formatAdminTimeAgo(lang, t)
		},
		"T": func(root interface{}, key string, args ...interface{}) string {
			return adminT(langFromTemplateRoot(root), key, args...)
		},
		"TF": func(root interface{}, key string, args ...interface{}) string {
			return adminT(langFromTemplateRoot(root), key, args...)
		},
		"adminLanguageName": adminLanguageName,
		"adminContentTypeLabel": func(lang, name, fallback string) string {
			return h.contentTypeLabel(lang, name, fallback)
		},
		"adminTaxonomyLabel": func(lang, name, fallback string) string {
			return h.taxonomyLabel(lang, name, fallback)
		},
		"adminMetaFieldLabel": func(root interface{}, typeName, key, fallback string) string {
			return h.metaFieldLabel(langFromTemplateRoot(root), typeName, key, fallback)
		},
		"X": func(root interface{}, key, fallback string, args ...interface{}) string {
			return extensionT(root, key, fallback, args...)
		},
		"jsT": func(root interface{}, key string, args ...interface{}) template.JS {
			b, err := json.Marshal(adminT(langFromTemplateRoot(root), key, args...))
			if err != nil {
				return template.JS("''")
			}
			return template.JS(b)
		},
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"toJSON": func(v interface{}) template.JS {
			b, err := json.Marshal(v)
			if err != nil {
				return template.JS("\"\"")
			}
			return template.JS(b)
		},
		"truncate": func(s string, n int) string {
			rs := []rune(s)
			if len(rs) <= n {
				return s
			}
			return string(rs[:n]) + "..."
		},
		"lower":    strings.ToLower,
		"contains": strings.Contains,
		"can": func(role, resource, action string) bool {
			return rbac.Can(role, h.mapResource(resource), action)
		},
		"roleDisplay": func(role string) string {
			return h.roleDisplay(defaultAdminLanguage, role)
		},
		"roleDisplayFor": func(root interface{}, role string) string {
			return h.roleDisplay(langFromTemplateRoot(root), role)
		},
		"formatSize": func(size int64) string {
			if size < 1024 {
				return fmt.Sprintf("%d B", size)
			}
			if size < 1024*1024 {
				return fmt.Sprintf("%.1f KB", float64(size)/1024)
			}
			return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
		},
		"hasKey": func(m map[uint]bool, key uint) bool {
			return m[key]
		},
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i
			}
			return s
		},
		"add": func(a, b int) int { return a + b },
		"hasSupport": func(supports []string, feature string) bool {
			for _, s := range supports {
				if s == feature {
					return true
				}
			}
			return false
		},
		"metaVal": func(meta map[string]string, key string) string {
			return meta[key]
		},
		"firstTaxName": func(taxonomies map[string][]TaxonomyItemView, taxType string) string {
			items := taxonomies[taxType]
			if len(items) > 0 {
				return items[0].Name
			}
			return ""
		},
		"dict": func(values ...interface{}) map[string]interface{} {
			m := make(map[string]interface{})
			for i := 0; i+1 < len(values); i += 2 {
				key, _ := values[i].(string)
				m[key] = values[i+1]
			}
			return m
		},
		"settingOr": func(m map[string]string, key, def string) string {
			if v, ok := m[key]; ok && v != "" {
				return v
			}
			return def
		},
		// renderHook lets admin templates emit a plugin extension slot. The
		// closure resolves h.hooks at call time, so it works even though the
		// hook bus is wired in via SetHookBus after buildFuncMap runs.
		"renderHook": func(name string, args ...interface{}) template.HTML {
			if h.hooks == nil || name == "" {
				return ""
			}
			output := h.hooks.ApplyFilter(name, template.HTML(""), args...)
			switch v := output.(type) {
			case template.HTML:
				return v
			case string:
				return template.HTML(v)
			case nil:
				return ""
			default:
				return template.HTML(fmt.Sprint(v))
			}
		},
	}
}

func formatAdminTimeAgo(lang string, t interface{}) string {
	var ts time.Time
	switch v := t.(type) {
	case time.Time:
		ts = v
	case *time.Time:
		if v == nil {
			return ""
		}
		ts = *v
	default:
		return ""
	}

	now := time.Now()
	if ts.After(now) {
		ts = now
	}

	d := now.Sub(ts)
	switch {
	case d < time.Minute:
		return adminT(lang, "time.ago.now")
	case d < time.Hour:
		return adminT(lang, "time.ago.minutes", int(d/time.Minute))
	case d < 24*time.Hour:
		return adminT(lang, "time.ago.hours", int(d/time.Hour))
	case d < 365*24*time.Hour:
		return adminT(lang, "time.ago.days", int(d/(24*time.Hour)))
	default:
		return adminT(lang, "time.ago.years", int(d/(365*24*time.Hour)))
	}
}

func langFromTemplateRoot(root interface{}) string {
	if data, ok := root.(gin.H); ok {
		if lang, ok := data["AdminLanguage"].(string); ok {
			return lang
		}
	}
	if data, ok := root.(map[string]interface{}); ok {
		if lang, ok := data["AdminLanguage"].(string); ok {
			return lang
		}
	}
	return defaultAdminLanguage
}

func extensionCatalogFromTemplateRoot(root interface{}) *coreI18n.Catalog {
	if data, ok := root.(gin.H); ok {
		if catalog, ok := data["ExtensionCatalog"].(*coreI18n.Catalog); ok {
			return catalog
		}
	}
	if data, ok := root.(map[string]interface{}); ok {
		if catalog, ok := data["ExtensionCatalog"].(*coreI18n.Catalog); ok {
			return catalog
		}
	}
	return nil
}

func extensionT(root interface{}, key, fallback string, args ...interface{}) string {
	lang := langFromTemplateRoot(root)
	if catalog := extensionCatalogFromTemplateRoot(root); catalog != nil {
		if msg := catalog.Message(lang, key); msg != "" {
			return coreI18n.FormatMessage(msg, args...)
		}
	}
	if msg := adminMessage(lang, key); msg != "" {
		return coreI18n.FormatMessage(msg, args...)
	}
	return coreI18n.FormatMessage(fallback, args...)
}

func (h *Handler) roleDisplay(lang, role string) string {
	switch role {
	case "super_admin":
		return adminT(lang, "role.super_admin")
	case "editor":
		return adminT(lang, "role.editor")
	case "author":
		return adminT(lang, "role.author")
	case "contributor":
		return adminT(lang, "role.contributor")
	case "subscriber":
		return adminT(lang, "role.subscriber")
	default:
		def := h.svc.rbac.GetRole(role)
		if def != nil {
			return def.DisplayName
		}
		return role
	}
}

func adminContentTypeLabel(lang, name, fallback string) string {
	if msg := adminMessage(normalizeAdminLanguage(lang), "content_type."+name); msg != "" {
		return msg
	}
	if fallback != "" {
		return fallback
	}
	return fallback
}

func adminTaxonomyLabel(lang, name, fallback string) string {
	if msg := adminMessage(normalizeAdminLanguage(lang), "taxonomy."+name); msg != "" {
		return msg
	}
	return fallback
}

func (h *Handler) activeThemeCatalog() *coreI18n.Catalog {
	if h.themeManager == nil || h.themeManager.LocaleCatalogFn == nil || h.themeManager.ActiveFn == nil {
		return nil
	}
	slug := h.themeManager.ActiveFn()
	if slug == "" {
		return nil
	}
	return h.themeManager.LocaleCatalogFn(slug)
}

func (h *Handler) themeCatalog(slug string) *coreI18n.Catalog {
	if h.themeManager == nil || h.themeManager.LocaleCatalogFn == nil || slug == "" {
		return nil
	}
	return h.themeManager.LocaleCatalogFn(slug)
}

func (h *Handler) pluginCatalog(slug string) *coreI18n.Catalog {
	if h.pluginCallbacks == nil || h.pluginCallbacks.LocaleCatalogFn == nil || slug == "" {
		return nil
	}
	return h.pluginCallbacks.LocaleCatalogFn(slug)
}

func catalogMessage(catalog *coreI18n.Catalog, lang, key string) string {
	if catalog == nil {
		return ""
	}
	return catalog.Message(normalizeAdminLanguage(lang), key)
}

func (h *Handler) contentTypeLabel(lang, name, fallback string) string {
	lang = normalizeAdminLanguage(lang)
	if msg := catalogMessage(h.activeThemeCatalog(), lang, "admin.content_type."+name); msg != "" {
		return msg
	}
	return adminContentTypeLabel(lang, name, fallback)
}

func (h *Handler) taxonomyLabel(lang, name, fallback string) string {
	lang = normalizeAdminLanguage(lang)
	if msg := catalogMessage(h.activeThemeCatalog(), lang, "admin.taxonomy."+name); msg != "" {
		return msg
	}
	return adminTaxonomyLabel(lang, name, fallback)
}

func (h *Handler) metaFieldLabel(lang, typeName, key, fallback string) string {
	lang = normalizeAdminLanguage(lang)
	if msg := catalogMessage(h.activeThemeCatalog(), lang, "admin.meta."+typeName+"."+key); msg != "" {
		return msg
	}
	if msg := adminMessage(lang, "admin.meta."+typeName+"."+key); msg != "" {
		return msg
	}
	return fallback
}

func (h *Handler) loadTemplates(dir string) {
	h.templates = make(map[string]*template.Template)

	// Login page (standalone, no layout)
	h.templates["login"] = template.Must(
		template.New("login.tmpl").Funcs(h.funcMap).ParseFiles(
			filepath.Join(dir, "pages", "login.tmpl"),
		),
	)

	// Generic pages with admin layout
	layout := filepath.Join(dir, "layouts", "admin.tmpl")
	pages := []string{
		"dashboard",
		"content_list", "content_form", "content_detail",
		"taxonomy_list",
		"settings", "media",
		"users", "user_form",
		"themes",
		"cache_mgmt", "redirects",
		"plugins",
		"menus", "menu_edit",
	}
	for _, page := range pages {
		pagePath := filepath.Join(dir, "pages", page+".tmpl")
		h.templates[page] = template.Must(
			template.New("").Funcs(h.funcMap).ParseFiles(layout, pagePath),
		)
	}
}

var menuIcons = map[string]string{
	"default": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="5" y="3.8" width="14" height="16.4" rx="2"/><path d="M8.2 8.2h7.6M8.2 12h7.6M8.2 15.8h5.2"/></svg>`,

	"dashboard": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 10.5L12 3l9 7.5"/><path d="M5 9.8V20h14V9.8"/><path d="M9 20v-5h6v5"/></svg>`,

	"blocks":          `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="4" y="4" width="6.5" height="6.5" rx="1.2"/><rect x="13.5" y="4" width="6.5" height="6.5" rx="1.2"/><rect x="4" y="13.5" width="6.5" height="6.5" rx="1.2"/><rect x="13.5" y="13.5" width="6.5" height="6.5" rx="1.2"/></svg>`,
	"edit":            `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m4 20 5.2-1.3L20 7.9 16.1 4 5.3 14.8z"/><path d="M14.8 5.3 18.7 9.2"/></svg>`,
	"collection":      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3.5" y="6" width="17" height="12.5" rx="2.3"/><path d="M3.5 9.5h17"/><path d="M8 6V4.2"/><path d="M16 6V4.2"/></svg>`,
	"post":            `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="5" y="3.8" width="14" height="16.4" rx="2"/><path d="M8.2 8.2h7.6M8.2 12h7.6M8.2 15.8h5.2"/></svg>`,
	"contact_message": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 11.5a8 8 0 1 1-3-6.3"/><path d="M8 20.2 6 22"/></svg>`,

	"category": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3.8 12h8.5l3.7-5h4l-3.4 10.2h-4z"/><circle cx="8.2" cy="16.8" r="1.8"/></svg>`,
	"tag":      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 10.2 13.8 4H6v7.8l6.2 6.2a2.2 2.2 0 0 0 3.1 0l4.7-4.7a2.2 2.2 0 0 0 0-3.1z"/><circle cx="8.9" cy="8.9" r="1.1"/></svg>`,

	"themes":     `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3.5A8.5 8.5 0 1 0 20.5 12c0-1.6-1.3-2.9-2.9-2.9H15a2.4 2.4 0 0 1-2.4-2.4V4.4"/></svg>`,
	"plugins":    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9.4 4.6a2.6 2.6 0 0 1 5.2 0v2.1h2.1a2.6 2.6 0 0 1 0 5.2h-2.1V14a2.6 2.6 0 0 1-5.2 0v-2.1H7.3a2.6 2.6 0 0 1 0-5.2h2.1z"/><path d="M12 11.9v.2"/></svg>`,
	"cache_mgmt": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><ellipse cx="12" cy="6" rx="6.8" ry="2.8"/><path d="M5.2 6v6c0 1.6 3 2.8 6.8 2.8s6.8-1.2 6.8-2.8V6"/><path d="M5.2 12v6c0 1.6 3 2.8 6.8 2.8s6.8-1.2 6.8-2.8v-6"/></svg>`,
	"redirects":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 7h10"/><path d="m11 3 4 4-4 4"/><path d="M19 17H9"/><path d="m13 13-4 4 4 4"/></svg>`,
	"media":      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3.5" y="5" width="17" height="14" rx="2.2"/><circle cx="9" cy="10" r="1.6"/><path d="m20.5 15.4-4.2-4.2a1.8 1.8 0 0 0-2.5 0l-4.9 4.9"/></svg>`,
	"settings":   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="2.8"/><path d="M19.4 15a1 1 0 0 0 .2 1.1l.1.1a1.8 1.8 0 1 1-2.5 2.5l-.1-.1a1 1 0 0 0-1.1-.2 1 1 0 0 0-.6.9V20a1.8 1.8 0 1 1-3.6 0v-.2a1 1 0 0 0-.6-.9 1 1 0 0 0-1.1.2l-.1.1a1.8 1.8 0 1 1-2.5-2.5l.1-.1A1 1 0 0 0 8.6 15a1 1 0 0 0-.9-.6H7.5a1.8 1.8 0 1 1 0-3.6h.2a1 1 0 0 0 .9-.6 1 1 0 0 0-.2-1.1l-.1-.1a1.8 1.8 0 1 1 2.5-2.5l.1.1a1 1 0 0 0 1.1.2h.1a1 1 0 0 0 .6-.9V4a1.8 1.8 0 1 1 3.6 0v.2a1 1 0 0 0 .6.9h.1a1 1 0 0 0 1.1-.2l.1-.1a1.8 1.8 0 1 1 2.5 2.5l-.1.1a1 1 0 0 0-.2 1.1v.1a1 1 0 0 0 .9.6h.2a1.8 1.8 0 1 1 0 3.6h-.2a1 1 0 0 0-.9.6z"/></svg>`,
	"users":      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="8" r="3.1"/><path d="M5.2 20a6.8 6.8 0 0 1 13.6 0"/></svg>`,
	"menus":      `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>`,
}

func menuIcon(key string) string {
	if icon, ok := menuIcons[key]; ok {
		return icon
	}
	return menuIcons["default"]
}

func resolveMenuIcon(custom, fallbackKey string) string {
	custom = strings.TrimSpace(custom)
	if custom == "" {
		return menuIcon(fallbackKey)
	}
	if strings.HasPrefix(custom, "<svg") {
		return custom
	}
	return menuIcon(custom)
}

// buildMenuItems creates the dynamic sidebar menu from the registry.
func (h *Handler) buildMenuItems(lang string) []AdminMenuItem {
	lang = normalizeAdminLanguage(lang)
	var items []AdminMenuItem

	// Dashboard
	items = append(items, AdminMenuItem{Label: adminT(lang, "nav.dashboard"), URL: "/admin/", Active: "dashboard", Icon: menuIcon("dashboard")})

	// Content types
	allTypes := h.registry.AllTypes()
	sort.Slice(allTypes, func(i, j int) bool {
		if allTypes[i].MenuOrder != allTypes[j].MenuOrder {
			return allTypes[i].MenuOrder < allTypes[j].MenuOrder
		}
		return allTypes[i].Name < allTypes[j].Name
	})

	items = append(items, AdminMenuItem{Section: adminT(lang, "nav.content")})
	for _, td := range allTypes {
		slug := AdminSlug(td.Name)
		label := h.contentTypeLabel(lang, td.Name, td.Label)
		items = append(items, AdminMenuItem{
			Label:  adminT(lang, "content.type.manage", label),
			URL:    "/admin/" + slug,
			Active: slug,
			Icon:   resolveMenuIcon(td.MenuIcon, td.Name),
		})
	}

	// Taxonomies
	allTax := h.registry.AllTaxonomies()
	if len(allTax) > 0 {
		sort.Slice(allTax, func(i, j int) bool {
			return allTax[i].Name < allTax[j].Name
		})
		items = append(items, AdminMenuItem{Section: adminT(lang, "nav.taxonomy")})
		for _, td := range allTax {
			slug := AdminSlug(td.Name)
			label := h.taxonomyLabel(lang, td.Name, td.Label)
			items = append(items, AdminMenuItem{
				Label:  adminT(lang, "content.type.manage", label),
				URL:    "/admin/" + slug,
				Active: slug,
				Icon:   resolveMenuIcon(td.MenuIcon, td.Name),
			})
		}
	}

	// System
	items = append(items, AdminMenuItem{Section: adminT(lang, "nav.system")})
	items = append(items, AdminMenuItem{Label: adminT(lang, "nav.menus"), URL: "/admin/menus", Active: "menus", Icon: menuIcon("menus")})
	items = append(items, AdminMenuItem{Label: adminT(lang, "nav.themes"), URL: "/admin/themes", Active: "themes", Icon: menuIcon("themes")})
	items = append(items, AdminMenuItem{Label: adminT(lang, "nav.plugins"), URL: "/admin/plugins", Active: "plugins", Icon: menuIcon("plugins")})
	items = append(items, AdminMenuItem{Label: adminT(lang, "nav.cache"), URL: "/admin/cache", Active: "cache_mgmt", Icon: menuIcon("cache_mgmt")})
	items = append(items, AdminMenuItem{Label: adminT(lang, "nav.redirects"), URL: "/admin/redirects", Active: "redirects", Icon: menuIcon("redirects")})
	items = append(items, AdminMenuItem{Label: adminT(lang, "nav.media"), URL: "/admin/media", Active: "media", Icon: menuIcon("media")})
	items = append(items, AdminMenuItem{Label: adminT(lang, "nav.settings"), URL: "/admin/settings", Active: "settings", Icon: menuIcon("settings")})
	items = append(items, AdminMenuItem{Label: adminT(lang, "nav.users"), URL: "/admin/users", Active: "users", Icon: menuIcon("users")})

	return items
}

func (h *Handler) render(c *gin.Context, name string, data gin.H) {
	tmpl, ok := h.templates[name]
	if !ok {
		c.String(http.StatusInternalServerError, "Template not found: "+name)
		return
	}

	adminLang := h.svc.AdminLanguage()
	data["AdminLanguage"] = adminLang
	data["CurrentRole"] = c.GetString("admin_role")
	data["CurrentUser"] = c.GetString("admin_username")
	data["MenuItems"] = h.buildMenuItems(adminLang)
	data["SiteName"] = h.svc.SiteName()
	data["PublicBaseURL"] = requestBaseURL(c)
	data["GoPressVersion"] = version.String()

	if success := c.Query("success"); success != "" {
		data["Success"] = success
	}
	if errMsg := c.Query("error"); errMsg != "" {
		data["Error"] = errMsg
	}

	execName := "admin"
	if name == "login" {
		execName = "login"
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, execName, data); err != nil {
		log.Printf("Template render error (%s): %v", name, err)
		c.String(http.StatusInternalServerError, "Template error")
	}
}

func requestBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwardedProto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwardedProto != "" {
		scheme = strings.TrimSpace(strings.Split(forwardedProto, ",")[0])
	} else if strings.EqualFold(c.GetHeader("X-Forwarded-Ssl"), "on") {
		scheme = "https"
	}

	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host != "" {
		host = strings.TrimSpace(strings.Split(host, ",")[0])
	} else {
		host = c.Request.Host
	}
	if host == "" {
		return ""
	}

	return scheme + "://" + host
}

// ==================== Helpers ====================

func (h *Handler) checkPermission(c *gin.Context, resource, action string) bool {
	role := c.GetString("admin_role")
	if !h.svc.rbac.Can(role, h.mapResource(resource), action) {
		c.Redirect(http.StatusFound, "/admin/?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.permission_denied")))
		c.Abort()
		return false
	}
	return true
}

func (h *Handler) logAction(c *gin.Context, action, resource string, resourceID uint, details string) {
	var userID uint
	if uid, exists := c.Get("admin_user_id"); exists {
		userID, _ = uid.(uint)
	}
	username := c.GetString("admin_username")
	h.svc.LogAction(userID, username, action, resource, resourceID, details, c.ClientIP())
}

func getIDParam(c *gin.Context) uint {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	return uint(id)
}

func parseUintSlice(strs []string) []uint {
	var result []uint
	for _, s := range strs {
		if id, err := strconv.ParseUint(s, 10, 32); err == nil {
			result = append(result, uint(id))
		}
	}
	return result
}

// hasSupport checks if a supports list contains a feature.
func hasSupport(supports []string, feature string) bool {
	for _, s := range supports {
		if s == feature {
			return true
		}
	}
	return false
}

// ==================== Auth Handlers ====================

func (h *Handler) LoginPage(c *gin.Context) {
	lang := h.svc.AdminLanguage()
	h.render(c, "login", gin.H{
		"Title": adminT(lang, "page.login"),
	})
}

func (h *Handler) LoginSubmit(c *gin.Context) {
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	lang := h.svc.AdminLanguage()
	if username == "" || password == "" {
		h.render(c, "login", gin.H{"Title": adminT(lang, "page.login"), "Error": adminT(lang, "error.login_required")})
		return
	}

	u, token, err := h.svc.Login(username, password)
	if err != nil {
		h.render(c, "login", gin.H{"Title": adminT(lang, "page.login"), "Error": adminT(lang, "error.invalid_login")})
		return
	}

	c.SetCookie("admin_token", token, 86400, "/admin", "", false, true)
	h.svc.LogAction(u.ID, u.Username, "login", "auth", 0, "user login", c.ClientIP())
	c.Redirect(http.StatusFound, "/admin/")
}

func (h *Handler) Logout(c *gin.Context) {
	var userID uint
	if uid, exists := c.Get("admin_user_id"); exists {
		userID, _ = uid.(uint)
	}
	username := c.GetString("admin_username")
	h.svc.LogAction(userID, username, "logout", "auth", 0, "user logout", c.ClientIP())
	c.SetCookie("admin_token", "", -1, "/admin", "", false, true)
	c.Redirect(http.StatusFound, "/admin/login")
}

// ==================== Dashboard ====================

func (h *Handler) Dashboard(c *gin.Context) {
	stats := h.svc.GetDashboardStats()
	logs := h.svc.ListRecentAuditLogs(10)
	lang := h.svc.AdminLanguage()

	h.render(c, "dashboard", gin.H{
		"Title":  adminT(lang, "nav.dashboard"),
		"Active": "dashboard",
		"Stats":  stats,
		"Logs":   logs,
	})
}
