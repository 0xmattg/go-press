// Package multilang provides a WPML-like Multi-Language plugin for GoPress.
//
// Features:
//   - Per-content translation with independent URLs (/en/products/hepa-filters)
//   - Translation group (trid) linking content across languages
//   - Language prefix URL routing for non-default languages
//   - Plugin-level database tables for translations, languages, string translations
//   - Admin settings page for language management
//   - UI string translation via go-i18n message catalogs
//   - Language switcher rendered through theme hook slots
//
// Usage in main.go:
//
//	import _ "go-press/plugins/multilang"
package multilang

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gorm.io/gorm"

	"go-press/core"
	"go-press/core/admin"
	"go-press/core/cache"
	contentPkg "go-press/core/content"
	"go-press/core/hook"
	coreI18n "go-press/core/i18n"
	"go-press/core/menu"
	"go-press/core/option"
	"go-press/core/plugin"
	"go-press/core/rewrite"
	"go-press/pkg/dbprefix"
	"go-press/pkg/logger"
)

const (
	PluginName  = "multi-language"
	CookieName  = "gopress_lang"
	DefaultLang = "zh"
)

// Plugin implements plugin.Plugin and plugin.SettingsProvider.
type Plugin struct {
	defaultTag string
	engine     *core.Engine
	repo       *Repository
	pluginDir  string // absolute path to this plugin's directory

	// Runtime-deactivation support: handles to every callback we register
	// during Activate so Deactivate can cleanly unregister them without a
	// server restart. Gin middleware (LanguagePrefixMiddleware) can't be
	// removed from a running router, so
	// they guard themselves with p.engine.PluginManager.IsActive() instead.
	hookHandles   []hook.Handle
	sitemapHandle rewrite.TransformerHandle
}

// New creates a new Multi-Language plugin.
func New() *Plugin {
	return &Plugin{
		defaultTag: DefaultLang,
		pluginDir:  filepath.Join("plugins", "multilang"),
	}
}

// --- Plugin interface ---

func (p *Plugin) Name() string    { return PluginName }
func (p *Plugin) Version() string { return "2.0.0" }
func (p *Plugin) Description() string {
	return "WPML-like 多语言内容翻译插件：独立URL、内容克隆、语言前缀路由"
}

// --- SettingsProvider interface ---

func (p *Plugin) SettingsTemplatePath() string {
	return filepath.Join(p.pluginDir, "templates", "admin", "settings.tmpl")
}

// --- SettingsSaveProvider interface ---

// OnSettingsSave syncs the language form data from admin settings to the plugin's languages table.
func (p *Plugin) OnSettingsSave(settings map[string]string) {
	if p.repo == nil {
		return
	}

	// Parse language entries from form: plugin_multi-language_lang_0_code, etc.
	prefix := "plugin_multi-language_lang_"
	var formLangs []Language
	for i := 0; i < 50; i++ {
		code := settings[fmt.Sprintf("%s%d_code", prefix, i)]
		if code == "" {
			break
		}
		name := settings[fmt.Sprintf("%s%d_name", prefix, i)]
		flag := settings[fmt.Sprintf("%s%d_flag", prefix, i)]
		sortStr := settings[fmt.Sprintf("%s%d_sort", prefix, i)]
		activeStr := settings[fmt.Sprintf("%s%d_active", prefix, i)]
		sort := 0
		fmt.Sscanf(sortStr, "%d", &sort)
		formLangs = append(formLangs, Language{
			Code:      code,
			Name:      name,
			Flag:      flag,
			SortOrder: sort,
			Active:    activeStr != "false",
		})
	}

	defaultLangCode := settings["plugin_multi-language_default_lang"]

	// Get existing languages from DB
	existing, _ := p.repo.ListLanguages()
	existingMap := make(map[string]*Language)
	for i := range existing {
		existingMap[existing[i].Code] = &existing[i]
	}

	// Track which codes are in the form
	formCodes := make(map[string]bool)
	for _, fl := range formLangs {
		formCodes[fl.Code] = true
		fl.IsDefault = (fl.Code == defaultLangCode)

		if ex, ok := existingMap[fl.Code]; ok {
			// Update existing
			ex.Name = fl.Name
			ex.Flag = fl.Flag
			ex.SortOrder = fl.SortOrder
			ex.Active = fl.Active
			ex.IsDefault = fl.IsDefault
			p.repo.db.Save(ex)
		} else {
			// Create new
			p.repo.CreateLanguage(&fl)
		}
	}

	// Delete languages not in form
	for code, ex := range existingMap {
		if !formCodes[code] {
			p.repo.db.Delete(ex)
		}
	}

	// Update plugin default tag
	if defaultLangCode != "" {
		p.defaultTag = defaultLangCode
	}

	logger.Info("multi-language: synced languages from settings", "count", len(formLangs), "default", defaultLangCode)
}

// --- Lifecycle ---

// Activate wires the plugin into the GoPress engine.
func (p *Plugin) Activate(app plugin.App) {
	e, ok := app.(*core.Engine)
	if !ok {
		logger.Error("multi-language: failed to cast app to *core.Engine")
		return
	}
	p.engine = e
	p.repo = NewRepository(e.DB)

	// Set default tag from site config (overrides hardcoded fallback)
	if e.Config != nil && e.Config.Site.Language != "" {
		tag := e.Config.Site.Language
		if idx := strings.IndexAny(tag, "-_"); idx > 0 {
			tag = tag[:idx]
		}
		p.defaultTag = strings.ToLower(tag)
	}

	// 1. Auto-migrate plugin tables
	if err := p.repo.AutoMigrate(); err != nil {
		logger.Error("multi-language: table migration failed", "error", err)
		return
	}
	// Register tables with core table registry
	core.RegisterPluginTable(pluginSlug, "translations")
	core.RegisterPluginTable(pluginSlug, "languages")
	core.RegisterPluginTable(pluginSlug, "string_translations")
	core.RegisterPluginTable(pluginSlug, "menu_translations")

	// 2. Seed default languages if table is empty
	p.seedDefaultLanguages()

	// 3. Load DB string translations as override layer on core i18n
	p.loadDBOverrides()

	// Reset handles slice on (re-)activation to avoid stale entries.
	p.hookHandles = p.hookHandles[:0]

	// 3b. Register menu hooks for transparent menu translation.
	p.registerMenuHooks(e)

	// 4. Register middleware via early hook (runs BEFORE page cache)
	p.hookHandles = append(p.hookHandles, e.Hooks.AddAction("middleware.early", func(ctx context.Context, args ...interface{}) {
		if len(args) == 0 {
			return
		}
		r, ok := args[0].(*gin.Engine)
		if !ok {
			return
		}
		// Language prefix detection middleware (sets current_lang)
		r.Use(p.LanguagePrefixMiddleware())
	}, 5))

	// 4b. Register standard theme navigation hook. Themes choose the exact
	// placement by calling {{renderHook "header.nav.after" .}} in templates.
	p.hookHandles = append(p.hookHandles, e.Hooks.AddFilter(hook.ThemeHeaderNavAfter, func(value interface{}, args ...interface{}) interface{} {
		if p.engine == nil || !p.engine.PluginManager.IsActive(PluginName) {
			return value
		}
		c := ginContextFromHookArgs(args...)
		if c == nil {
			return value
		}
		existing := template.HTML("")
		switch v := value.(type) {
		case template.HTML:
			existing = v
		case string:
			existing = template.HTML(v)
		case nil:
		default:
			existing = template.HTML(fmt.Sprint(v))
		}
		return existing + template.HTML(p.buildNavDropdown(c))
	}, 10))

	// Register route via routes.register hook
	p.hookHandles = append(p.hookHandles, e.Hooks.AddAction("routes.register", func(ctx context.Context, args ...interface{}) {
		if len(args) == 0 {
			return
		}
		r, ok := args[0].(*gin.Engine)
		if !ok {
			return
		}
		r.GET("/lang/:tag", p.handleLangSwitch)
		// Translation admin API (protected by cookie-based admin session check)
		r.POST("/admin/plugins/multi-language/translate", p.handleCreateTranslation)
		r.POST("/admin/plugins/multi-language/unlink", p.handleUnlinkTranslation)
		// Menu translation admin API
		r.POST("/admin/plugins/multi-language/menu-translate", p.handleMenuTranslationSave)
		r.POST("/admin/plugins/multi-language/menu-unlink", p.handleMenuUnlink)
		// String translation admin API
		r.POST("/admin/plugins/multi-language/string-translate", p.handleStringTranslationSave)
		// Option translation admin API
		r.POST("/admin/plugins/multi-language/option-translate", p.handleOptionTranslationSave)
	}, 5))

	// 4c. Re-prime the i18n bundle whenever core admin reports a bulk option
	// write. Without this, _opt.<key> messages seeded from Options at startup
	// stay frozen and shadow newly-saved values via TranslateOption's
	// "msg != ''" guard in core/i18n.
	p.hookHandles = append(p.hookHandles, e.Hooks.AddAction(hook.OptionsBulkUpdated, func(ctx context.Context, args ...interface{}) {
		p.loadDBOverrides()
	}, 10))

	// 5. Log initialization
	p.hookHandles = append(p.hookHandles, e.Hooks.AddAction("engine.init", func(ctx context.Context, args ...interface{}) {
		langs, _ := p.repo.ActiveLanguages()
		tags := make([]string, len(langs))
		for i, l := range langs {
			tags[i] = l.Code
		}
		logger.Info("Multi-language plugin initialized",
			"languages", tags,
			"default", p.defaultTag,
		)
	}, 10))

	// 6. Register sitemap transformer for hreflang alternates + translated URLs
	if e.Sitemap != nil {
		p.sitemapHandle = e.Sitemap.AddTransformer(p.sitemapTransformer)
	}

	// 7. Contribute language filter tabs to admin content list pages.
	p.hookHandles = append(p.hookHandles, e.Hooks.AddFilter(admin.HookContentListTabs, p.adminContentListTabs, 10))

	// 8. Prepend "/<lang>" to admin permalink display for non-default languages.
	p.hookHandles = append(p.hookHandles, e.Hooks.AddFilter(admin.HookContentPermalinkPrefix, p.adminContentPermalinkPrefix, 10))

	logger.Info("Multi-language plugin activated (v2.0 WPML-like)")
}

// Deactivate fully unwinds every hook, filter, transformer and menu callback
// registered during Activate, so the plugin becomes a runtime no-op without a
// server restart. The two Gin middlewares it installed on Activate remain on
// the router (Gin has no removal API) but self-short-circuit via IsActive.
func (p *Plugin) Deactivate(app plugin.App) {
	e, ok := app.(*core.Engine)
	if !ok {
		logger.Error("multi-language: failed to cast app to *core.Engine on deactivate")
		return
	}

	// 1. Unregister every action/filter added during Activate.
	for _, h := range p.hookHandles {
		// RemoveAction / RemoveFilter both accept Handle; try both since we
		// don't track which kind each handle was (cheap: no-op if not matching).
		e.Hooks.RemoveAction(h)
		e.Hooks.RemoveFilter(h)
	}
	p.hookHandles = nil

	// 2. Unregister sitemap transformer.
	if e.Sitemap != nil {
		e.Sitemap.RemoveTransformer(p.sitemapHandle)
		p.sitemapHandle = rewrite.TransformerHandle{}
	}

	logger.Info("Multi-language plugin deactivated (hooks + transformer removed)")
}

// --- SettingsDataProvider interface ---

// ContentTranslationView holds a content item with its per-language translation status.
type ContentTranslationView struct {
	ID           uint
	Type         string
	TypeLabel    string
	Title        string
	Slug         string
	Status       string
	EditURL      string          // admin edit URL, e.g. /admin/posts/51/edit
	AdminSlug    string          // admin URL slug, e.g. "posts"
	HasArchive   bool            // whether this type supports /edit
	Trid         uint            // 0 if not in any translation group
	LangCode     string          // language of this content (from translation table)
	Translations map[string]uint // langCode → contentID for each existing translation
}

// StringKeyView holds a translation key with its file defaults and DB overrides per language.
type StringKeyView struct {
	Name      string
	Defaults  map[string]string            // langCode → default value from locale file
	Overrides map[string]StringOverrideVal // langCode → DB override
}

// StringOverrideVal is a DB string translation override.
type StringOverrideVal struct {
	ID    uint
	Value string
}

// OptionTransView holds a translatable option with its default-language value and per-language overrides.
type OptionTransView struct {
	Key          string                       // option key e.g. "home_about_title"
	Section      string                       // UI section e.g. "about"
	Label        string                       // human-readable label
	DefaultValue string                       // current value from Options table (default language)
	Overrides    map[string]StringOverrideVal // langCode → DB override (non-default languages)
}

// SettingsData returns extra template data for the plugin settings page.
func (p *Plugin) SettingsData() map[string]interface{} {
	data := make(map[string]interface{})

	// Load active languages
	langs, _ := p.repo.ActiveLanguages()
	data["Languages"] = langs

	// Load default language
	defaultLang := p.getDefaultLang()
	data["DefaultLang"] = defaultLang

	// Load all content with their translation status
	if p.engine != nil {
		var views []ContentTranslationView
		types := p.engine.Registry.AllTypes()
		sort.SliceStable(types, func(i, j int) bool {
			if types[i].MenuOrder != types[j].MenuOrder {
				return types[i].MenuOrder < types[j].MenuOrder
			}
			return types[i].Name < types[j].Name
		})
		seenTrid := make(map[uint]bool) // deduplicate: one row per translation group

		for _, ct := range types {
			items, err := p.engine.Content.Query().
				Type(ct.Name).
				OrderBy("sort_order", "ASC").
				OrderBy("id", "ASC").
				Get()
			if err != nil {
				continue
			}
			aSlug := adminSlug(ct.Name)
			for _, item := range items {
				// Check if this content has a translation record
				t, hasTranslation := p.repo.GetTranslation(item.ID)
				if hasTranslation != nil {
					// Not linked to any trid — treat as default language
					editURL := fmt.Sprintf("/admin/%s/%d/edit", aSlug, item.ID)
					if !ct.HasArchive {
						editURL = fmt.Sprintf("/admin/%s/%d", aSlug, item.ID)
					}
					views = append(views, ContentTranslationView{
						ID:           item.ID,
						Type:         item.Type,
						TypeLabel:    ct.Label,
						Title:        item.Title,
						Slug:         item.Slug,
						Status:       item.Status,
						EditURL:      editURL,
						AdminSlug:    aSlug,
						HasArchive:   ct.HasArchive,
						LangCode:     defaultLang,
						Translations: map[string]uint{defaultLang: item.ID},
					})
					continue
				}

				// Has translation record — deduplicate by trid
				if seenTrid[t.Trid] {
					continue
				}
				seenTrid[t.Trid] = true

				// Get all translations in this group
				translations := make(map[string]uint)
				if group, err := p.repo.GetTranslationsByTrid(t.Trid); err == nil {
					for _, gt := range group {
						translations[gt.LanguageCode] = gt.ContentID
					}
				}

				// Use the default-language content as the representative row
				repItem := item
				repLang := t.LanguageCode
				if defID, ok := translations[defaultLang]; ok && defID != item.ID {
					// Find the default-lang content from the already-loaded items
					for _, c := range items {
						if c.ID == defID {
							repItem = c
							repLang = defaultLang
							break
						}
					}
				}

				editURL := fmt.Sprintf("/admin/%s/%d/edit", aSlug, repItem.ID)
				if !ct.HasArchive {
					editURL = fmt.Sprintf("/admin/%s/%d", aSlug, repItem.ID)
				}
				views = append(views, ContentTranslationView{
					ID:           repItem.ID,
					Type:         repItem.Type,
					TypeLabel:    ct.Label,
					Title:        repItem.Title,
					Slug:         repItem.Slug,
					Status:       repItem.Status,
					EditURL:      editURL,
					AdminSlug:    aSlug,
					HasArchive:   ct.HasArchive,
					Trid:         t.Trid,
					LangCode:     repLang,
					Translations: translations,
				})
			}
		}
		data["ContentItems"] = views

		// Content types for filter
		data["ContentTypes"] = types

		// --- Menu Translation Data ---
		allMenus, _ := p.engine.Menus.GetAll()
		menuTrans, _ := p.repo.ListAllMenuTranslations()

		// Clean up ghost records (deleted menus)
		validMenuIDs := make(map[uint]bool)
		for _, m := range allMenus {
			validMenuIDs[m.ID] = true
		}
		for _, mt := range menuTrans {
			if !validMenuIDs[mt.MenuID] {
				p.repo.UnlinkMenuTranslation(mt.MenuID)
			}
		}
		// Re-fetch after cleanup
		menuTrans, _ = p.repo.ListAllMenuTranslations()

		// Build menuID → langCode map from translation records
		menuLangMap := make(map[uint]string)           // menuID → langCode
		menuTridMap := make(map[uint]uint)             // menuID → trid
		tridLangMenu := make(map[uint]map[string]uint) // trid → {lang → menuID}
		for _, mt := range menuTrans {
			menuLangMap[mt.MenuID] = mt.LanguageCode
			menuTridMap[mt.MenuID] = mt.Trid
			if tridLangMenu[mt.Trid] == nil {
				tridLangMenu[mt.Trid] = make(map[string]uint)
			}
			tridLangMenu[mt.Trid][mt.LanguageCode] = mt.MenuID
		}

		// Build per-location assignments from translation groups
		locations := p.engine.Menus.GetLocations()
		var menuLocations []MenuLocationAssignment
		for _, loc := range locations {
			assignment := MenuLocationAssignment{
				Location: loc.Name,
				Label:    loc.Label,
				Menus:    make(map[string]uint),
			}
			// Find trid groups that include a menu at this location
			for trid, langMenus := range tridLangMenu {
				_ = trid
				matchesLocation := false
				for _, mid := range langMenus {
					for _, m := range allMenus {
						if m.ID == mid && m.Location == loc.Name {
							matchesLocation = true
							break
						}
					}
					if matchesLocation {
						break
					}
				}
				if matchesLocation {
					for lang, mid := range langMenus {
						assignment.Menus[lang] = mid
					}
					break
				}
			}
			menuLocations = append(menuLocations, assignment)
		}

		data["MenuLocations"] = menuLocations
		data["AllMenus"] = allMenus

		// --- String Translation Data ---
		keysByLang, sortedKeys := p.readThemeLocaleKeys()

		// Load DB overrides for all languages
		overrides := make(map[string]map[string]StringOverrideVal) // key → langCode → override
		for _, lang := range langs {
			sts, _ := p.repo.ListStringTranslationsByLang(lang.Code)
			for _, st := range sts {
				if overrides[st.Name] == nil {
					overrides[st.Name] = make(map[string]StringOverrideVal)
				}
				overrides[st.Name][lang.Code] = StringOverrideVal{ID: st.ID, Value: st.Value}
			}
		}

		var stringKeys []StringKeyView
		for _, key := range sortedKeys {
			view := StringKeyView{
				Name:      key,
				Defaults:  make(map[string]string),
				Overrides: make(map[string]StringOverrideVal),
			}
			for langCode, keys := range keysByLang {
				if val, ok := keys[key]; ok {
					view.Defaults[langCode] = val
				}
			}
			if ov, ok := overrides[key]; ok {
				view.Overrides = ov
			}
			stringKeys = append(stringKeys, view)
		}
		data["StringKeys"] = stringKeys

		// --- Translatable Options Data ---
		trKeys := option.AllTranslatableOptions()
		defaultLangCode := p.getDefaultLang()
		nonDefaultLangs := make([]Language, 0, len(langs))
		for _, l := range langs {
			if l.Code != defaultLangCode {
				nonDefaultLangs = append(nonDefaultLangs, l)
			}
		}
		data["NonDefaultLangs"] = nonDefaultLangs

		// Load DB overrides for domain="option"
		optOverrides := make(map[string]map[string]StringOverrideVal) // key → langCode → override
		for _, l := range nonDefaultLangs {
			sts, _ := p.repo.ListStringTranslations("option", l.Code)
			for _, st := range sts {
				if optOverrides[st.Name] == nil {
					optOverrides[st.Name] = make(map[string]StringOverrideVal)
				}
				optOverrides[st.Name][l.Code] = StringOverrideVal{ID: st.ID, Value: st.Value}
			}
		}

		// Organize by section
		sectionOrder := []string{}
		sectionMap := make(map[string][]OptionTransView)
		allSettings := p.engine.Options.All()
		for _, tk := range trKeys {
			view := OptionTransView{
				Key:          tk.Key,
				Section:      tk.Section,
				Label:        tk.Label,
				DefaultValue: allSettings[tk.Key],
				Overrides:    make(map[string]StringOverrideVal),
			}
			if ov, ok := optOverrides[tk.Key]; ok {
				view.Overrides = ov
			}
			if _, exists := sectionMap[tk.Section]; !exists {
				sectionOrder = append(sectionOrder, tk.Section)
			}
			sectionMap[tk.Section] = append(sectionMap[tk.Section], view)
		}
		data["OptionSections"] = sectionOrder
		data["OptionsBySection"] = sectionMap
	}

	return data
}

// --- Translation Admin Handlers ---

// handleCreateTranslation clones a content item to a target language.
func (p *Plugin) handleCreateTranslation(c *gin.Context) {
	// Verify admin session (parse JWT from admin_token cookie)
	if !p.isAdmin(c) {
		c.Redirect(http.StatusFound, "/admin/login")
		c.Abort()
		return
	}

	contentIDStr := c.PostForm("content_id")
	targetLang := c.PostForm("target_lang")
	if contentIDStr == "" || targetLang == "" {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=参数不完整")
		return
	}

	var contentID uint
	if _, err := fmt.Sscanf(contentIDStr, "%d", &contentID); err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=无效的内容ID")
		return
	}

	// Load original content
	original, err := p.engine.Content.FindByID(contentID)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=内容不存在")
		return
	}

	// Ensure original is in a translation group (assign to default lang if not)
	origTrans, err := p.repo.GetTranslation(contentID)
	if err != nil {
		// Not yet linked — create a translation group for the original
		defLang := p.getDefaultLang()
		if linkErr := p.LinkTranslation(contentID, defLang, nil); linkErr != nil {
			c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=创建翻译组失败: "+linkErr.Error())
			return
		}
		// If the target language IS the default language, we just linked it — done
		if targetLang == defLang {
			c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?success=已将内容关联到"+targetLang)
			return
		}
		origTrans, _ = p.repo.GetTranslation(contentID)
	}

	// Check if target language already has a translation
	if _, err := p.repo.FindTranslation(origTrans.Trid, targetLang); err == nil {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=该语言的翻译已存在")
		return
	}

	// Clone content with a unique slug.
	//
	// WPML semantics: same slug allowed across languages — uniqueness is
	// scoped to the target language only. We register the language scope
	// against this gin.Context first so EnsureUniqueSlugScoped sees only
	// rows of `targetLang`. The original slug then almost always survives
	// unchanged (great for SEO: /products/foo + /zh/products/foo); a "-2"
	// suffix is only added on a real same-language collision.
	p.registerLangContentScope(c, targetLang)
	clonedSlug, err := p.engine.Content.EnsureUniqueSlugScoped(c, original.Type, original.Slug, 0)
	if err != nil {
		// Fall back to the source slug; collisions surface as a save error
		// downstream rather than silently appending a language suffix.
		clonedSlug = original.Slug
	}

	// Determine status: "publish" form field controls draft vs published
	clonedStatus := contentPkg.StatusDraft
	if c.PostForm("publish") == "1" {
		clonedStatus = contentPkg.StatusPublished
	}

	cloned := &contentPkg.Content{
		Type:          original.Type,
		Status:        clonedStatus,
		Title:         original.Title + " [" + targetLang + "]",
		Slug:          clonedSlug,
		Content:       original.Content,
		Excerpt:       original.Excerpt,
		ImageURL:      original.ImageURL,
		AuthorID:      original.AuthorID,
		ParentID:      original.ParentID,
		SortOrder:     original.SortOrder,
		CommentStatus: original.CommentStatus,
	}
	// PublishedAt must be set for published items so .Published() filter matches
	if clonedStatus == contentPkg.StatusPublished {
		now := time.Now()
		cloned.PublishedAt = &now
	}

	if err := p.engine.Content.Create(cloned); err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=克隆内容失败: "+err.Error())
		return
	}

	// Copy meta fields
	if meta, err := p.engine.Content.GetMeta(contentID); err == nil {
		for k, v := range meta {
			p.engine.Content.SaveMeta(cloned.ID, k, v)
		}
	}

	// Link cloned content to the same translation group
	sourceID := contentID
	if err := p.repo.CreateTranslation(&Translation{
		Trid:            origTrans.Trid,
		ContentID:       cloned.ID,
		LanguageCode:    targetLang,
		SourceContentID: &sourceID,
	}); err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=关联翻译失败: "+err.Error())
		return
	}

	logger.Info("Translation created", "original", contentID, "cloned", cloned.ID, "lang", targetLang, "status", clonedStatus)
	statusLabel := "草稿"
	if clonedStatus == contentPkg.StatusPublished {
		statusLabel = "已发布"
	}
	c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?success=已创建"+targetLang+"翻译（"+statusLabel+"），请编辑翻译内容")
}

// handleUnlinkTranslation removes a content item from its translation group.
func (p *Plugin) handleUnlinkTranslation(c *gin.Context) {
	if !p.isAdmin(c) {
		c.Redirect(http.StatusFound, "/admin/login")
		c.Abort()
		return
	}

	contentIDStr := c.PostForm("content_id")
	if contentIDStr == "" {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=参数不完整")
		return
	}

	var contentID uint
	if _, err := fmt.Sscanf(contentIDStr, "%d", &contentID); err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=无效的内容ID")
		return
	}

	if err := p.repo.DeleteTranslation(contentID); err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=取消关联失败: "+err.Error())
		return
	}

	c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?success=已取消翻译关联")
}

// isAdmin checks if the current request has a valid admin JWT token.
func (p *Plugin) isAdmin(c *gin.Context) bool {
	if p.engine == nil || p.engine.Auth == nil {
		return false
	}
	token, err := c.Cookie("admin_token")
	if err != nil || token == "" {
		return false
	}
	_, err = p.engine.Auth.ParseToken(token)
	return err == nil
}

// adminSlug computes the admin URL slug for a content type name.
// Mirrors core/admin.AdminSlug without importing the admin package.
func adminSlug(name string) string {
	slug := strings.ReplaceAll(name, "_", "-")
	if strings.HasSuffix(slug, "y") && !strings.HasSuffix(slug, "ey") {
		return slug[:len(slug)-1] + "ies"
	}
	if strings.HasSuffix(slug, "s") || strings.HasSuffix(slug, "x") ||
		strings.HasSuffix(slug, "ch") || strings.HasSuffix(slug, "sh") {
		return slug + "es"
	}
	return slug + "s"
}

// seedDefaultLanguages seeds the languages table based on the site language setting.
// The site language becomes the default; a second common language is added as non-default.
func (p *Plugin) seedDefaultLanguages() {
	langs, err := p.repo.ListLanguages()
	if err != nil || len(langs) > 0 {
		return
	}

	// Determine the site's configured language (e.g. "en", "zh-CN", "ja")
	siteLang := "zh"
	if p.engine != nil && p.engine.Config != nil && p.engine.Config.Site.Language != "" {
		siteLang = p.engine.Config.Site.Language
	}
	// Normalize: "zh-CN" → "zh", "en-US" → "en"
	if idx := strings.IndexAny(siteLang, "-_"); idx > 0 {
		siteLang = siteLang[:idx]
	}
	siteLang = strings.ToLower(siteLang)

	// Known languages with display names and flags
	knownLangs := map[string]Language{
		"zh": {Code: "zh", Name: "中文", Flag: "🇨🇳"},
		"en": {Code: "en", Name: "English", Flag: "🇬🇧"},
		"ja": {Code: "ja", Name: "日本語", Flag: "🇯🇵"},
		"ko": {Code: "ko", Name: "한국어", Flag: "🇰🇷"},
		"fr": {Code: "fr", Name: "Français", Flag: "🇫🇷"},
		"de": {Code: "de", Name: "Deutsch", Flag: "🇩🇪"},
		"es": {Code: "es", Name: "Español", Flag: "🇪🇸"},
	}

	// Build the default language entry
	defaultEntry, ok := knownLangs[siteLang]
	if !ok {
		defaultEntry = Language{Code: siteLang, Name: siteLang, Flag: "🌐"}
	}
	defaultEntry.IsDefault = true
	defaultEntry.SortOrder = 0
	defaultEntry.Active = true

	// Pick a secondary language (zh if site is not zh, otherwise en)
	secondCode := "en"
	if siteLang == "en" {
		secondCode = "zh"
	}
	secondEntry, ok := knownLangs[secondCode]
	if !ok {
		secondEntry = Language{Code: secondCode, Name: secondCode, Flag: "🌐"}
	}
	secondEntry.IsDefault = false
	secondEntry.SortOrder = 1
	secondEntry.Active = true

	for _, lang := range []Language{defaultEntry, secondEntry} {
		if err := p.repo.CreateLanguage(&lang); err != nil {
			logger.Warn("multi-language: failed to seed language", "code", lang.Code, "error", err)
		}
	}
	p.defaultTag = siteLang
	logger.Info("multi-language: seeded default languages", "default", siteLang, "secondary", secondCode)
}

// --- i18n DB Override Layer ---

// loadDBOverrides loads string translations from the DB and Options table,
// overlaying them on top of the core i18n bundle (file-based defaults).
func (p *Plugin) loadDBOverrides() {
	if p.engine == nil || p.engine.I18n == nil {
		return
	}

	// 0. Register all translatable option values as default-language messages.
	// go-i18n requires a message to exist in the bundle's default language
	// before translations in other languages can be found by the Localizer.
	bundleDefaultLang := p.engine.I18n.DefaultLang()
	allOpts := p.engine.Options.All()
	transKeys := option.AllTranslatableOptions()
	if len(transKeys) > 0 {
		defMsgs := make([]*goi18n.Message, 0, len(transKeys))
		for _, tk := range transKeys {
			val := allOpts[tk.Key] // may be "" if not saved to Options table
			defMsgs = append(defMsgs, &goi18n.Message{ID: coreI18n.OptMsgPrefix + tk.Key, Other: val})
		}
		p.engine.I18n.AddMessages(bundleDefaultLang, defMsgs)
	}

	// 1. Load from StringTranslation table (all domains, managed via admin UI)
	langs, _ := p.repo.ActiveLanguages()
	for _, lang := range langs {
		sts, err := p.repo.ListStringTranslationsByLang(lang.Code)
		if err != nil {
			continue
		}
		if len(sts) == 0 {
			continue
		}
		msgs := make([]*goi18n.Message, 0, len(sts))
		for _, st := range sts {
			msgID := st.Name
			// Translatable options use _opt. prefix in the i18n bundle
			// so they don't collide with locale-file message IDs.
			if st.Domain == "option" {
				msgID = coreI18n.OptMsgPrefix + st.Name
			}
			msgs = append(msgs, &goi18n.Message{ID: msgID, Other: st.Value})
		}
		p.engine.I18n.AddMessages(lang.Code, msgs)
	}

	// 2. Load from Options table (legacy i18n.{lang}.{key} format)
	msgsByLang := make(map[string][]*goi18n.Message)
	for key, val := range allOpts {
		if !strings.HasPrefix(key, "i18n.") {
			continue
		}
		parts := strings.SplitN(key, ".", 3)
		if len(parts) != 3 {
			continue
		}
		lang := parts[1]
		msgID := parts[2]
		msgsByLang[lang] = append(msgsByLang[lang], &goi18n.Message{ID: msgID, Other: val})
	}
	for lang, msgs := range msgsByLang {
		p.engine.I18n.AddMessages(lang, msgs)
	}
}

// --- Language Prefix Middleware ---

// LanguagePrefixMiddleware detects language from URL prefix, cookie, or Accept-Language.
// For non-default languages, URLs have a prefix like /en/products/hepa-filters.
// The middleware strips the prefix so the rewrite engine sees /products/hepa-filters.
func (p *Plugin) LanguagePrefixMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Self-guard for runtime deactivation: Gin doesn't let us detach
		// middleware from a live engine, so short-circuit when the plugin is
		// no longer active. Preserves zero-trace behavior after Deactivate.
		if p.engine == nil || !p.engine.PluginManager.IsActive(PluginName) {
			c.Next()
			return
		}
		path := c.Request.URL.Path

		// Skip admin, API, static
		if strings.HasPrefix(path, "/admin") ||
			strings.HasPrefix(path, "/api/") ||
			strings.HasPrefix(path, "/static/") ||
			strings.HasPrefix(path, "/lang/") ||
			path == "/health" ||
			path == "/sitemap.xml" ||
			path == "/robots.txt" {
			lang := p.detectFromCookieOrHeader(c)
			c.Set(coreI18n.CtxKeyLang, lang)
			if p.engine.I18n != nil {
				c.Set(coreI18n.CtxKeyLocalizer, p.engine.I18n.NewLocalizer(lang))
			}
			// Admin content list filter: when ?lang=<code> is present on an admin
			// path, register a language-scoped content filter so only that
			// language's content rows are returned. No query param → no scope
			// → all languages listed (preserves backward behavior).
			if strings.HasPrefix(path, "/admin") {
				if q := c.Query("lang"); q != "" && p.isSupported(q) {
					p.registerLangContentScope(c, q)
				}
			}
			c.Next()
			return
		}

		// Check URL prefix for language code: /en/... /ja/...
		lang := ""
		langs, _ := p.repo.ActiveLanguages()
		for _, l := range langs {
			prefix := "/" + l.Code + "/"
			if strings.HasPrefix(path, prefix) || path == "/"+l.Code {
				lang = l.Code
				// Strip the language prefix from URL so rewrite engine works normally
				newPath := strings.TrimPrefix(path, "/"+l.Code)
				if newPath == "" {
					newPath = "/"
				}
				c.Request.URL.Path = newPath
				break
			}
		}

		// If no prefix detected, try cookie/header, default to default language
		if lang == "" {
			lang = p.detectFromCookieOrHeader(c)
		}

		// Persist in cookie
		c.SetCookie(CookieName, lang, 365*24*3600, "/", "", false, false)
		c.Set(coreI18n.CtxKeyLang, lang)

		// Set per-goroutine language for menu translation hook
		menu.SetRequestLang(lang)
		defer menu.ClearRequestLang()

		// Register a content scope via core API for language-based content filtering.
		// Any theme using content.ScopedDB(c, db) will automatically get filtered results.
		p.registerLangContentScope(c, lang)

		// Override the core i18n localizer with language-aware one
		if p.engine.I18n != nil {
			c.Set(coreI18n.CtxKeyLocalizer, p.engine.I18n.NewLocalizer(lang))
		}

		c.Next()
	}
}

// adminContentListTabs implements the admin.HookContentListTabs filter. It
// appends one tab per active language (plus an "All" tab) to the slice the
// core admin passes in. When only one language is configured, returns the
// input unchanged — no tabs for single-language sites.
//
// Hook args: [*gin.Context, typeName string]
func (p *Plugin) adminContentListTabs(value interface{}, args ...interface{}) interface{} {
	tabs, _ := value.([]admin.ContentListTab)
	// Guard: the hook bus has no Remove API, so when the user deactivates the
	// plugin this callback stays registered. Short-circuit if the plugin is no
	// longer active so tabs disappear from the admin UI immediately.
	if p.engine == nil || !p.engine.PluginManager.IsActive(PluginName) {
		return tabs
	}
	if p.repo == nil || len(args) < 2 {
		return tabs
	}
	c, _ := args[0].(*gin.Context)
	typeName, _ := args[1].(string)
	if c == nil || typeName == "" {
		return tabs
	}

	languages, err := p.repo.ActiveLanguages()
	if err != nil || len(languages) < 2 {
		return tabs
	}

	activeLang := c.Query("lang")
	basePath := c.Request.URL.Path

	// "All" tab - no language filter.
	tabs = append(tabs, admin.ContentListTab{
		Key:    "all",
		Label:  "全部",
		Count:  p.countContent(typeName, ""),
		Active: activeLang == "",
		URL:    basePath,
	})

	for _, l := range languages {
		label := l.Name
		if l.Flag != "" {
			label = l.Flag + " " + l.Name
		}
		tabs = append(tabs, admin.ContentListTab{
			Key:    l.Code,
			Label:  label,
			Count:  p.countContent(typeName, l.Code),
			Active: activeLang == l.Code,
			URL:    basePath + "?lang=" + l.Code,
		})
	}
	return tabs
}

// adminContentPermalinkPrefix implements admin.HookContentPermalinkPrefix.
// It looks up the content's translation row and, when the language is not
// the site default, returns "/<lang>" so the admin edit page shows the full
// language-prefixed permalink (e.g. /zh/products/foo) instead of the bare
// path that would be ambiguous in a multilingual setup.
//
// Hook args: [*gin.Context, *content.Content]
func (p *Plugin) adminContentPermalinkPrefix(value interface{}, args ...interface{}) interface{} {
	prefix, _ := value.(string)
	// Hot-unplug guard: hook stays registered after Deactivate (until the
	// engine is restarted), so short-circuit when not active.
	if p.engine == nil || !p.engine.PluginManager.IsActive(PluginName) {
		return prefix
	}
	if p.repo == nil || len(args) < 2 {
		return prefix
	}
	item, ok := args[1].(*contentPkg.Content)
	if !ok || item == nil || item.ID == 0 {
		return prefix
	}
	trans, err := p.repo.GetTranslation(item.ID)
	if err != nil || trans == nil {
		// No translation record: treat as default language → no prefix.
		return prefix
	}
	if trans.LanguageCode == "" || trans.LanguageCode == p.getDefaultLang() {
		return prefix
	}
	return prefix + "/" + trans.LanguageCode
}

// countContent returns the number of contents of the given type, optionally
// constrained to a specific language using the same semantics as
// registerLangContentScope. lang == "" means "all languages".
func (p *Plugin) countContent(typeName, lang string) int {
	if p.engine == nil || p.engine.DB == nil {
		return 0
	}
	contentTable := dbprefix.Table("contents")
	q := p.engine.DB.Table(contentTable).
		Where("type = ? AND deleted_at IS NULL", typeName)

	if lang != "" {
		transTable := dbprefix.PluginTable(pluginSlug, "translations")
		defaultLang := p.getDefaultLang()
		if lang == defaultLang {
			q = q.Where(
				fmt.Sprintf("%s.id IN (SELECT content_id FROM %s WHERE language_code = ?) OR %s.id NOT IN (SELECT content_id FROM %s)",
					contentTable, transTable, contentTable, transTable),
				lang,
			)
		} else {
			q = q.Where(
				fmt.Sprintf("%s.id IN (SELECT content_id FROM %s WHERE language_code = ?)",
					contentTable, transTable),
				lang,
			)
		}
	}

	var n int64
	q.Count(&n)
	return int(n)
}

// registerLangContentScope attaches a GORM WHERE clause to the request-scoped
// content filter registry so any query running through content.ScopedDB(c, db)
// returns only rows belonging to the given language. Shared between front-end
// language-prefix requests and admin ?lang=<code> filtering.
//
// Semantics:
//   - Default language: rows explicitly linked to that language OR rows with
//     no translation row at all (treated as the canonical/source version).
//   - Non-default language: rows explicitly linked to that language only.
func (p *Plugin) registerLangContentScope(c *gin.Context, lang string) {
	defaultLang := p.getDefaultLang()
	transTable := dbprefix.PluginTable(pluginSlug, "translations")
	contentTable := dbprefix.Table("contents")
	contentPkg.AddContentScope(c, func(db *gorm.DB) *gorm.DB {
		if lang == defaultLang {
			return db.Where(
				fmt.Sprintf("%s.id IN (SELECT content_id FROM %s WHERE language_code = ?) OR %s.id NOT IN (SELECT content_id FROM %s)",
					contentTable, transTable, contentTable, transTable),
				lang,
			)
		}
		return db.Where(
			fmt.Sprintf("%s.id IN (SELECT content_id FROM %s WHERE language_code = ?)",
				contentTable, transTable),
			lang,
		)
	})
}

func (p *Plugin) detectFromCookieOrHeader(c *gin.Context) string {
	// Query parameter ?lang=en overrides
	if q := c.Query("lang"); q != "" && p.isSupported(q) {
		c.SetCookie(CookieName, q, 365*24*3600, "/", "", false, false)
		return q
	}

	// Cookie
	if cookie, err := c.Cookie(CookieName); err == nil && p.isSupported(cookie) {
		return cookie
	}

	// Accept-Language header
	accept := c.GetHeader("Accept-Language")
	if accept != "" {
		tags, _, err := language.ParseAcceptLanguage(accept)
		if err == nil {
			for _, tag := range tags {
				base, _ := tag.Base()
				if p.isSupported(base.String()) {
					return base.String()
				}
			}
		}
	}

	return p.getDefaultLang()
}

func (p *Plugin) isSupported(tag string) bool {
	tag = strings.ToLower(tag)
	langs, _ := p.repo.ActiveLanguages()
	for _, l := range langs {
		if strings.ToLower(l.Code) == tag {
			return true
		}
	}
	return false
}

func (p *Plugin) getDefaultLang() string {
	if p.repo != nil {
		if lang, err := p.repo.DefaultLanguage(); err == nil {
			return lang.Code
		}
	}
	return p.defaultTag
}

// handleLangSwitch switches language and redirects to the translated page.
func (p *Plugin) handleLangSwitch(c *gin.Context) {
	tag := c.Param("tag")
	if !p.isSupported(tag) {
		tag = p.getDefaultLang()
	}

	ref := c.GetHeader("Referer")
	if ref == "" {
		ref = "/"
	}

	// Try to resolve the translated URL for the target language
	redirectURL, switched := p.resolveTranslatedURL(ref, tag)
	if switched {
		c.SetCookie(CookieName, tag, 365*24*3600, "/", "", false, false)
	}
	c.Redirect(http.StatusFound, redirectURL)
}

// resolveTranslatedURL takes a referer URL and target language,
// and returns the best URL to redirect to in that language. The boolean
// indicates whether the target language was actually resolved and may be
// persisted in the language cookie.
func (p *Plugin) resolveTranslatedURL(referer, targetLang string) (string, bool) {
	defaultLang := p.getDefaultLang()

	// Parse the referer to get the path
	parsed, err := url.Parse(referer)
	if err != nil {
		return "/", true
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}

	// Strip any existing language prefix from the path
	langs, _ := p.repo.ActiveLanguages()
	originalLang := defaultLang
	for _, l := range langs {
		prefix := "/" + l.Code + "/"
		if strings.HasPrefix(path, prefix) || path == "/"+l.Code {
			originalLang = l.Code
			path = strings.TrimPrefix(path, "/"+l.Code)
			if path == "" {
				path = "/"
			}
			break
		}
	}
	// Try to find content from the path and resolve its translation.
	// Detail page patterns: /products/{slug}, /services/{slug}, /blog/{slug}, etc.
	// originalLang is needed so the source-side slug lookup picks the right
	// language row when same slug is reused across languages (WPML mode).
	if translatedPath, matched := p.resolveContentTranslation(path, originalLang, targetLang); matched {
		if translatedPath != "" {
			return translatedPath, true
		}
		return sameRefererPath(parsed), false
	}

	// For archive/list pages, just add or remove the language prefix
	if targetLang == defaultLang {
		return path, true
	}
	return "/" + targetLang + path, true
}

// resolveContentTranslation checks if the path matches a detail page pattern
// and returns the translated content's URL with proper language prefix.
// sourceLang scopes the slug lookup so a same-slug-across-languages setup
// (WPML mode) correctly picks the row for the page the user is currently on.
func (p *Plugin) resolveContentTranslation(path, sourceLang, targetLang string) (string, bool) {
	if p.engine == nil {
		return "", false
	}
	defaultLang := p.getDefaultLang()

	// Try common detail page patterns: /{archive}/{slug}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		return "", false
	}
	archivePath := parts[0] // e.g. "products", "services", "blog"
	slug := parts[1]

	// Build a throw-away gin.Context carrying the source-language scope so
	// FindBySlugScoped picks the right row. We can't reuse the request's
	// gin.Context here (we're in URL-rewriting helper land), so we synthesize
	// one with just the scope set.
	scopeCtx := &gin.Context{}
	p.registerLangContentScope(scopeCtx, sourceLang)

	// Find the content by slug across all types
	types := p.engine.Registry.AllTypes()
	for _, ct := range types {
		if !ct.HasArchive {
			continue
		}
		// Check if this type's rewrite slug matches the URL prefix
		archiveSlug := ct.Rewrite.Slug
		if archiveSlug == "" {
			continue
		}
		if archiveSlug != archivePath {
			continue
		}

		// Found matching type — look up content by slug, scoped to sourceLang.
		item, err := p.engine.Content.FindBySlugScoped(scopeCtx, ct.Name, slug)
		if err != nil || item == nil {
			continue
		}

		// Look up the translation for the target language
		trans, err := p.repo.GetTranslation(item.ID)
		if err != nil {
			// No translation record. If target is default lang, stay on same slug.
			if targetLang == defaultLang {
				return path, true
			}
			return "", true
		}

		translatedID, err := p.repo.GetTranslatedContentID(trans.Trid, targetLang)
		if err != nil {
			return "", true // No translation available for this language
		}

		// Load the translated content to get its slug
		translatedContent, err := p.engine.Content.FindByID(translatedID)
		if err != nil {
			return "", true
		}

		translatedPath := "/" + archivePath + "/" + translatedContent.Slug
		if targetLang == defaultLang {
			return translatedPath, true
		}
		return "/" + targetLang + translatedPath, true
	}
	return "", false
}

func sameRefererPath(parsed *url.URL) string {
	path := parsed.Path
	if path == "" {
		path = "/"
	}
	if parsed.RawQuery != "" {
		path += "?" + parsed.RawQuery
	}
	return path
}

// --- Content Translation Helpers ---

// GetTranslatedContent looks up the translated content ID for a given content
// in the target language. Returns 0 if no translation exists.
func (p *Plugin) GetTranslatedContent(contentID uint, targetLang string) uint {
	if p.repo == nil {
		return 0
	}
	t, err := p.repo.GetTranslation(contentID)
	if err != nil {
		return 0
	}
	translated, err := p.repo.GetTranslatedContentID(t.Trid, targetLang)
	if err != nil {
		return 0
	}
	return translated
}

// GetTranslationGroup returns all translations for a content item.
func (p *Plugin) GetTranslationGroup(contentID uint) []Translation {
	if p.repo == nil {
		return nil
	}
	t, err := p.repo.GetTranslation(contentID)
	if err != nil {
		return nil
	}
	group, err := p.repo.GetTranslationsByTrid(t.Trid)
	if err != nil {
		return nil
	}
	return group
}

// LinkTranslation creates or updates a translation link for a content item.
// If the content is not yet in any translation group, a new trid is assigned.
func (p *Plugin) LinkTranslation(contentID uint, langCode string, sourceContentID *uint) error {
	if p.repo == nil {
		return fmt.Errorf("repository not initialized")
	}

	// Check if this content already has a translation record
	existing, err := p.repo.GetTranslation(contentID)
	if err == nil {
		// Already linked — update language if needed
		existing.LanguageCode = langCode
		existing.SourceContentID = sourceContentID
		return p.repo.db.Save(existing).Error
	}

	// Determine trid: if source is provided and has a trid, use that
	var trid uint
	if sourceContentID != nil {
		if sourceTrans, err := p.repo.GetTranslation(*sourceContentID); err == nil {
			trid = sourceTrans.Trid
		}
	}
	if trid == 0 {
		trid, err = p.repo.NextTrid()
		if err != nil {
			return err
		}
	}

	return p.repo.CreateTranslation(&Translation{
		Trid:            trid,
		ContentID:       contentID,
		LanguageCode:    langCode,
		SourceContentID: sourceContentID,
	})
}

// LangPrefixURL prepends a language prefix to local site paths when the
// current language is not the default language. Absolute external URLs,
// protocol-relative URLs, anchors, query-only links, and scheme-based links
// such as mailto: or tel: are returned unchanged.
func (p *Plugin) LangPrefixURL(c *gin.Context, path string) string {
	if path == "" || !isLanguagePrefixableURL(path) {
		return path
	}
	lang := p.getDefaultLang()
	if c != nil {
		if l, ok := c.Get(coreI18n.CtxKeyLang); ok {
			if s, ok := l.(string); ok && s != "" {
				lang = s
			}
		}
	}
	defaultLang := p.getDefaultLang()
	if lang == defaultLang {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if path == "/" {
		return "/" + lang + "/"
	}
	return "/" + lang + path
}

// Translate translates a message for the current request's language.
// Delegates to the core i18n Manager.
func (p *Plugin) Translate(c *gin.Context, msgID string) string {
	if p.engine != nil && p.engine.I18n != nil {
		return p.engine.I18n.Translate(c, msgID)
	}
	return msgID
}

// RenderSwitcher generates the language switcher HTML.
func (p *Plugin) RenderSwitcher(c *gin.Context) template.HTML {
	currentLang := p.getDefaultLang()
	if lang, ok := c.Get(coreI18n.CtxKeyLang); ok {
		currentLang = lang.(string)
	}

	langs, _ := p.repo.ActiveLanguages()
	if len(langs) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(`<div class="lang-switcher">`)
	for i, l := range langs {
		if i > 0 {
			b.WriteString(` <span class="lang-sep">|</span> `)
		}
		if strings.EqualFold(l.Code, currentLang) {
			b.WriteString(fmt.Sprintf(`<span class="lang-active">%s</span>`, l.Name))
		} else {
			b.WriteString(fmt.Sprintf(`<a href="/lang/%s" class="lang-link">%s</a>`, l.Code, l.Name))
		}
	}
	b.WriteString(`</div>`)
	return template.HTML(b.String())
}

// ginContextFromHookArgs extracts the current request context from template hook
// data. Theme page data normally embeds a PageData struct with Ctx promoted.
func ginContextFromHookArgs(args ...interface{}) *gin.Context {
	for _, arg := range args {
		if c, ok := arg.(*gin.Context); ok {
			return c
		}
		rv := reflect.ValueOf(arg)
		if !rv.IsValid() {
			continue
		}
		for rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				break
			}
			rv = rv.Elem()
		}
		if rv.Kind() != reflect.Struct {
			continue
		}
		field := rv.FieldByName("Ctx")
		if !field.IsValid() || !field.CanInterface() {
			continue
		}
		if c, ok := field.Interface().(*gin.Context); ok {
			return c
		}
	}
	return nil
}

// buildNavDropdown builds a dropdown language switcher as a nav <li> item.
func (p *Plugin) buildNavDropdown(c *gin.Context) string {
	current := p.currentLangOption(c)
	langs, _ := p.repo.ActiveLanguages()

	var b strings.Builder
	b.WriteString(`<li class="gp-lang-dropdown">`)
	b.WriteString(fmt.Sprintf(`<a href="javascript:void(0)" class="gp-lang-trigger">%s %s <span class="gp-lang-arrow">▾</span></a>`, current.Flag, current.Name))
	b.WriteString(`<ul class="gp-lang-menu">`)
	for _, l := range langs {
		if strings.EqualFold(l.Code, current.Code) {
			b.WriteString(fmt.Sprintf(`<li class="gp-lang-item gp-lang-current"><span>%s %s</span></li>`, l.Flag, l.Name))
		} else {
			b.WriteString(fmt.Sprintf(`<li class="gp-lang-item"><a href="/lang/%s">%s %s</a></li>`, l.Code, l.Flag, l.Name))
		}
	}
	b.WriteString(`</ul>`)
	b.WriteString(`</li>`)
	b.WriteString(p.dropdownCSS())
	return b.String()
}

func (p *Plugin) currentLangOption(c *gin.Context) Language {
	tag := p.getDefaultLang()
	if lang, ok := c.Get(coreI18n.CtxKeyLang); ok {
		tag = lang.(string)
	}
	langs, _ := p.repo.ActiveLanguages()
	for _, l := range langs {
		if strings.EqualFold(l.Code, tag) {
			return l
		}
	}
	if len(langs) > 0 {
		return langs[0]
	}
	return Language{Code: "zh", Name: "中文", Flag: "🇨🇳"}
}

// --- CSS ---

func (p *Plugin) dropdownCSS() string {
	return `<style>
.gp-lang-dropdown {
	position: relative;
	list-style: none;
}
.gp-lang-trigger {
	display: inline-flex;
	align-items: center;
	gap: 6px;
	padding: 8px 12px;
	color: #334155;
	text-decoration: none;
	font-size: 15px;
	font-weight: 600;
	cursor: pointer;
	transition: color 0.2s;
}
.gp-lang-trigger:hover {
	color: #2563eb;
}
.gp-lang-arrow {
	font-size: 14px;
	transition: transform 0.2s;
}
.gp-lang-dropdown:hover .gp-lang-arrow {
	transform: rotate(180deg);
}
.gp-lang-menu {
	display: none;
	position: absolute;
	top: 100%;
	right: 0;
	min-width: 160px;
	background: #fff;
	border: 1px solid #e2e8f0;
	border-radius: 8px;
	box-shadow: 0 8px 24px rgba(0,0,0,0.12);
	padding: 6px 0;
	z-index: 1000;
	list-style: none;
	margin: 0;
}
.gp-lang-dropdown:hover .gp-lang-menu {
	display: block;
}
.gp-lang-item a,
.gp-lang-item span {
	display: flex;
	align-items: center;
	gap: 10px;
	padding: 10px 16px;
	color: #334155;
	text-decoration: none;
	font-size: 14px;
	transition: background 0.15s;
	white-space: nowrap;
}
.gp-lang-item a:hover {
	background: #f1f5f9;
	color: #2563eb;
}
.gp-lang-current span {
	font-weight: 600;
	color: #2563eb;
	background: #eff6ff;
}
</style>`
}

// --- Menu Translation Integration ---

// registerMenuHooks sets up language-aware menu resolution using core menu hooks.
// It does NOT assume the menu in the location map is always the default
// language's menu. It checks the translation table to determine the menu's
// actual language and resolves the correct menu for the current request
// language accordingly.
func (p *Plugin) registerMenuHooks(e *core.Engine) {
	if e.Menus == nil || e.Hooks == nil {
		return
	}
	p.hookHandles = append(p.hookHandles, e.Hooks.AddAction(hook.MenuDeleted, func(ctx context.Context, args ...interface{}) {
		if len(args) == 0 {
			return
		}
		menuID, ok := args[0].(uint)
		if !ok {
			return
		}
		p.repo.UnlinkMenuTranslation(menuID)
		logger.Info("multi-language: cleaned up translation record for deleted menu", "menu_id", menuID)
	}, 10))

	p.hookHandles = append(p.hookHandles, e.Hooks.AddFilter(hook.MenuLocationResolve, func(value interface{}, args ...interface{}) interface{} {
		defaultMenu, ok := value.(*menu.Menu)
		if !ok || defaultMenu == nil {
			return value
		}
		location := ""
		if len(args) > 0 {
			if s, ok := args[0].(string); ok {
				location = s
			}
		}
		targetLang := menu.GetRequestLang()
		if targetLang == "" {
			targetLang = p.getDefaultLang()
		}
		defaultLang := p.getDefaultLang()

		logger.Info("menu resolve hook called",
			"location", location, "defaultMenuID", defaultMenu.ID,
			"targetLang", targetLang, "defaultLang", defaultLang)

		// Look up translation record for the menu currently in the location map
		mt, err := p.repo.GetMenuTranslation(defaultMenu.ID)
		if err != nil {
			logger.Info("menu resolve: no translation record for menu", "menu_id", defaultMenu.ID, "err", err)
			// No translation record: this menu is unlinked.
			// For default language, use as-is; for others, no translation available.
			if targetLang != defaultLang {
				return p.rewriteMenuURLs(defaultMenu, targetLang)
			}
			return defaultMenu
		}

		// The menu in the location map belongs to language mt.LanguageCode.
		// If that matches the target language, use it (with URL rewrite if non-default).
		if mt.LanguageCode == targetLang {
			if targetLang == defaultLang {
				return defaultMenu // already the correct default-lang menu
			}
			return p.rewriteMenuURLs(defaultMenu, targetLang)
		}

		// Target language differs from the menu's language — find the correct translation
		translatedMenuID, err := p.repo.GetTranslatedMenuID(mt.Trid, targetLang)
		if err != nil || translatedMenuID == defaultMenu.ID {
			logger.Info("menu resolve: no translated menu found", "trid", mt.Trid, "targetLang", targetLang, "err", err)
			return defaultMenu // no translation for target language
		}

		translated := e.Menus.GetByIDCached(translatedMenuID)
		if translated == nil {
			logger.Info("menu resolve: translated menu not in cache", "translatedMenuID", translatedMenuID)
			return defaultMenu
		}

		logger.Info("menu resolve: switching menu", "from", defaultMenu.ID, "to", translatedMenuID, "lang", targetLang)

		if targetLang == defaultLang {
			return translated // default lang menu, no URL rewriting needed
		}
		return p.rewriteMenuURLs(translated, targetLang)
	}, 10))
	logger.Info("multi-language: menu hooks registered")
}

// rewriteMenuURLs clones a menu and adjusts item URLs for the target language.
// - Local URLs get language prefix: /products → /en/products
// - Content-linked items get resolved to translated content slug
func (p *Plugin) rewriteMenuURLs(m *menu.Menu, lang string) *menu.Menu {
	defaultLang := p.getDefaultLang()
	if lang == defaultLang {
		return m
	}
	clone := &menu.Menu{
		ID:       m.ID,
		Name:     m.Name,
		Location: m.Location,
		Items:    p.rewriteItems(m.Items, lang),
	}
	return clone
}

func (p *Plugin) rewriteItems(items []menu.Item, lang string) []menu.Item {
	result := make([]menu.Item, len(items))
	for i, item := range items {
		result[i] = item
		result[i].URL = p.rewriteItemURL(item, lang)
		if len(item.Children) > 0 {
			result[i].Children = p.rewriteItems(item.Children, lang)
		}
	}
	return result
}

func (p *Plugin) rewriteItemURL(item menu.Item, lang string) string {
	u := item.URL

	// Non-page links are not language-scoped.
	if !isLanguagePrefixableURL(u) {
		return u
	}

	// Content-linked items: resolve to translated content's URL
	if item.ContentID != nil && *item.ContentID > 0 {
		if translatedURL := p.resolveContentURL(*item.ContentID, lang); translatedURL != "" {
			return translatedURL
		}
	}

	// Local URLs: add language prefix
	if !strings.HasPrefix(u, "/") {
		u = "/" + u
	}
	return "/" + lang + u
}

func isLanguagePrefixableURL(raw string) bool {
	u := strings.TrimSpace(raw)
	if u == "" || strings.HasPrefix(u, "#") || strings.HasPrefix(u, "?") || strings.HasPrefix(u, "//") {
		return false
	}
	parsed, err := url.Parse(u)
	if err == nil && parsed.Scheme != "" {
		return false
	}
	return true
}

// resolveContentURL finds the translated content's URL for a menu item's content_id.
func (p *Plugin) resolveContentURL(contentID uint, lang string) string {
	if p.engine == nil {
		return ""
	}
	defaultLang := p.getDefaultLang()

	// Get the translation group for this content
	t, err := p.repo.GetTranslation(contentID)
	if err != nil {
		// No translation record; add lang prefix to original URL
		return ""
	}

	// Find translated content ID
	translatedID, err := p.repo.GetTranslatedContentID(t.Trid, lang)
	if err != nil {
		return "" // no translation, fall back to default URL with prefix
	}

	// Load translated content to get its slug
	translated, err := p.engine.Content.FindByID(translatedID)
	if err != nil {
		return ""
	}

	// Build the URL using the content type's rewrite slug
	types := p.engine.Registry.AllTypes()
	for _, ct := range types {
		if ct.Name == translated.Type && ct.HasArchive && ct.Rewrite.Slug != "" {
			path := "/" + ct.Rewrite.Slug + "/" + translated.Slug
			if lang == defaultLang {
				return path
			}
			return "/" + lang + path
		}
	}
	return ""
}

// --- Menu Translation Admin Handlers ---

// MenuLocationAssignment holds the per-language menu assignments for one location.
type MenuLocationAssignment struct {
	Location string          // e.g. "header"
	Label    string          // e.g. "Header navigation"
	Menus    map[string]uint // langCode → menuID (0 = unassigned)
}

// handleMenuTranslationSave saves all menu-location-language assignments at once.
// Form fields: "loc_{location}_{langCode}" = menuID (or empty for unassigned).
func (p *Plugin) handleMenuTranslationSave(c *gin.Context) {
	if !p.isAdmin(c) {
		c.Redirect(http.StatusFound, "/admin/login")
		return
	}

	langs, _ := p.repo.ActiveLanguages()
	locations := p.engine.Menus.GetLocations()

	// Clear ALL existing menu translation records first
	allTrans, _ := p.repo.ListAllMenuTranslations()
	for _, mt := range allTrans {
		p.repo.UnlinkMenuTranslation(mt.MenuID)
	}

	// Rebuild from form data: group by location, each location gets a trid
	for _, loc := range locations {
		assigned := make(map[string]uint) // langCode → menuID
		for _, lang := range langs {
			key := "loc_" + loc.Name + "_" + lang.Code
			val := c.PostForm(key)
			if val == "" || val == "0" {
				continue
			}
			var menuID uint
			fmt.Sscanf(val, "%d", &menuID)
			if menuID > 0 {
				assigned[lang.Code] = menuID
			}
		}

		// Only create translation group if at least 2 languages are assigned
		if len(assigned) < 2 {
			continue
		}

		trid, err := p.repo.NextMenuTrid()
		if err != nil {
			logger.Error("Failed to create menu trid", "error", err)
			continue
		}

		for lang, menuID := range assigned {
			p.repo.LinkMenuTranslation(menuID, lang, trid)
		}
		logger.Info("Menu translation group saved", "location", loc.Name, "trid", trid, "assignments", assigned)
	}

	// Reload menus so the hook picks up new assignments
	p.engine.Menus.LoadAll()

	c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?success=菜单语言分配已保存")
}

// handleMenuUnlink removes a menu from its translation group.
func (p *Plugin) handleMenuUnlink(c *gin.Context) {
	if !p.isAdmin(c) {
		c.Redirect(http.StatusFound, "/admin/login")
		return
	}

	menuIDStr := c.PostForm("menu_id")
	if menuIDStr == "" {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=参数不完整")
		return
	}

	var menuID uint
	fmt.Sscanf(menuIDStr, "%d", &menuID)

	if err := p.repo.UnlinkMenuTranslation(menuID); err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=取消关联失败: "+err.Error())
		return
	}

	c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?success=已取消菜单翻译关联")
}

// --- String Translation Admin ---

// readThemeLocaleKeys reads the active theme's locale JSON files and returns
// all keys organized by language, plus a sorted list of unique keys.
func (p *Plugin) readThemeLocaleKeys() (map[string]map[string]string, []string) {
	result := make(map[string]map[string]string)
	keySet := make(map[string]bool)

	themeName := p.engine.ActiveThemeName()
	if themeName == "" {
		return result, nil
	}

	localesDir := filepath.Join("themes", themeName, "locales")
	entries, err := os.ReadDir(localesDir)
	if err != nil {
		return result, nil
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		langCode := strings.TrimSuffix(entry.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(localesDir, entry.Name()))
		if err != nil {
			continue
		}
		var flat map[string]string
		if err := json.Unmarshal(data, &flat); err != nil {
			continue
		}
		result[langCode] = flat
		for k := range flat {
			keySet[k] = true
		}
	}

	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return result, keys
}

// handleStringTranslationSave saves string translation overrides from the admin UI.
// Form fields: "st_{langCode}:{key}" = override value (empty = delete override).
func (p *Plugin) handleStringTranslationSave(c *gin.Context) {
	if !p.isAdmin(c) {
		c.Redirect(http.StatusFound, "/admin/login")
		return
	}

	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=表单解析失败")
		return
	}

	count := 0
	for key, values := range c.Request.PostForm {
		if !strings.HasPrefix(key, "st_") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(key, "st_"), ":", 2)
		if len(parts) != 2 {
			continue
		}
		langCode := parts[0]
		msgKey := parts[1]
		value := strings.TrimSpace(values[0])

		if value == "" {
			// Remove override → revert to file default
			p.repo.DeleteStringTranslationByKey("theme", msgKey, langCode)
		} else {
			p.repo.UpsertStringTranslation(&StringTranslation{
				Domain:       "theme",
				Name:         msgKey,
				LanguageCode: langCode,
				Value:        value,
				Status:       "translated",
			})
			count++
		}
	}

	// Reload DB overrides into core i18n
	p.loadDBOverrides()

	// Flush page cache so visitors see the new translations
	cache.InvalidatePageCache(p.engine.Cache)

	logger.Info("String translations saved", "count", count)
	c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?success=字符串翻译已保存")
}

// handleOptionTranslationSave saves translatable option overrides from the admin UI.
// Form fields: "ot_{langCode}:{optionKey}" = translated value.
func (p *Plugin) handleOptionTranslationSave(c *gin.Context) {
	if !p.isAdmin(c) {
		c.Redirect(http.StatusFound, "/admin/login")
		return
	}

	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?error=表单解析失败")
		return
	}

	count := 0
	for key, values := range c.Request.PostForm {
		if !strings.HasPrefix(key, "ot_") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(key, "ot_"), ":", 2)
		if len(parts) != 2 {
			continue
		}
		langCode := parts[0]
		optKey := parts[1]
		value := strings.TrimSpace(values[0])

		if value == "" {
			p.repo.DeleteStringTranslationByKey("option", optKey, langCode)
		} else {
			p.repo.UpsertStringTranslation(&StringTranslation{
				Domain:       "option",
				Name:         optKey,
				LanguageCode: langCode,
				Value:        value,
				Status:       "translated",
			})
			count++
		}
	}

	// Reload DB overrides into core i18n
	p.loadDBOverrides()

	// Flush page cache so visitors see the new translations
	cache.InvalidatePageCache(p.engine.Cache)

	logger.Info("Option translations saved", "count", count)
	c.Redirect(http.StatusFound, "/admin/plugins/multi-language/settings?success=主题设置翻译已保存")
}
