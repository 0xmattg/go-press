package admin

import (
	"fmt"
	"html/template"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go-press/core/hook"
	"go-press/core/option"
	"go-press/core/user"

	"github.com/gin-gonic/gin"
)

// ==================== Settings ====================

func (h *Handler) SettingList(c *gin.Context) {
	if !h.checkPermission(c, "setting", "read") {
		return
	}
	adminLang := h.svc.AdminLanguage()
	items := h.svc.GetAllSettings(adminLang)
	h.render(c, "settings", gin.H{
		"Title":  adminT(adminLang, "settings.system_settings"),
		"Active": "settings",
		"Items":  items,
	})
}

func (h *Handler) SettingUpdate(c *gin.Context) {
	if !h.checkPermission(c, "setting", "update") {
		return
	}
	items := h.svc.GetAllSettings(h.svc.AdminLanguage())
	for _, s := range items {
		if s.ReadOnly {
			continue
		}
		newValue := c.PostForm(s.Key)
		if s.InputType == "checkbox" {
			newValue = "0"
			if c.PostForm(s.Key) != "" {
				newValue = "1"
			}
		}
		if newValue != s.Value {
			h.svc.UpdateSetting(s.Key, newValue)
		}
	}
	if newKey := c.PostForm("new_key"); newKey != "" {
		newValue := c.PostForm("new_value")
		h.svc.CreateSetting(newKey, newValue)
	}
	h.invalidatePageCache()
	h.fireOptionsBulkUpdated(c)
	h.logAction(c, "update", "setting", 0, "system settings updated")
	c.Redirect(http.StatusFound, "/admin/settings?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.settings_updated")))
}

// SitemapGenerate generates a static sitemap.xml file in the project root.
func (h *Handler) SitemapGenerate(c *gin.Context) {
	if !h.checkPermission(c, "setting", "update") {
		return
	}
	if h.sitemapCallbacks == nil || h.sitemapCallbacks.GenerateFn == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": adminT(h.svc.AdminLanguage(), "error.sitemap_unconfigured")})
		return
	}
	count, err := h.sitemapCallbacks.GenerateFn()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.logAction(c, "generate", "sitemap", 0, fmt.Sprintf("generated sitemap.xml with %d URLs", count))
	c.JSON(http.StatusOK, gin.H{"success": true, "count": count})
}

// ==================== Media ====================

func (h *Handler) MediaList(c *gin.Context) {
	if !h.checkPermission(c, "media", "read") {
		return
	}
	items, _, _ := h.svc.mediaRepo.List("", 1, 1000)
	lang := h.svc.AdminLanguage()
	h.render(c, "media", gin.H{
		"Title":           adminT(lang, "nav.media"),
		"Active":          "media",
		"Items":           items,
		"MediaVariantJob": h.svc.MediaVariantJobStatus(),
	})
}

func (h *Handler) MediaUpload(c *gin.Context) {
	if !h.checkPermission(c, "media", "create") {
		return
	}

	var userID uint
	if uid, exists := c.Get("admin_user_id"); exists {
		userID, _ = uid.(uint)
	}

	// Accept multi-file upload via name="files" and fall back to the legacy
	// single-file name="file" so any older form/picker that posts here still
	// works without changes.
	form, _ := c.MultipartForm()
	var files []*multipart.FileHeader
	if form != nil {
		files = append(files, form.File["files"]...)
		files = append(files, form.File["file"]...)
	}
	if len(files) == 0 {
		c.Redirect(http.StatusFound, "/admin/media?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.file_required")))
		return
	}

	var okCount int
	var lastErr string
	for _, file := range files {
		media, err := h.svc.UploadFile(file, userID)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		okCount++
		h.logAction(c, "create", "media", media.ID, media.OriginalName)
	}

	switch {
	case okCount == 0:
		c.Redirect(http.StatusFound, "/admin/media?error="+url.QueryEscape(lastErr))
	case lastErr != "":
		c.Redirect(http.StatusFound, "/admin/media?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.media_uploaded_partial", okCount, len(files)-okCount, lastErr)))
	default:
		c.Redirect(http.StatusFound, "/admin/media?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.media_uploaded", okCount)))
	}
}

func (h *Handler) MediaDelete(c *gin.Context) {
	if !h.checkPermission(c, "media", "delete") {
		return
	}
	id := getIDParam(c)
	_ = h.svc.DeleteMediaFile(id)
	h.logAction(c, "delete", "media", id, "")
	c.Redirect(http.StatusFound, "/admin/media?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.media_deleted")))
}

// MediaRegenerateVariants rebuilds responsive image derivatives for existing media.
func (h *Handler) MediaRegenerateVariants(c *gin.Context) {
	if !h.checkPermission(c, "media", "update") {
		return
	}
	force := c.PostForm("force") == "1"
	_, started := h.svc.StartMediaVariantRegeneration(force)
	if !started {
		c.Redirect(http.StatusFound, "/admin/media")
		return
	}
	h.logAction(c, "update", "media", 0, "regenerate variants")
	c.Redirect(http.StatusFound, "/admin/media")
}

// MediaJSON returns media items as JSON for the media picker modal.
func (h *Handler) MediaJSON(c *gin.Context) {
	if !h.checkPermission(c, "media", "read") {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	perPage := 20
	items, total, _ := h.svc.mediaRepo.List("image", page, perPage)
	type mediaItem struct {
		ID      uint   `json:"id"`
		URL     string `json:"url"`
		Name    string `json:"name"`
		AltText string `json:"alt_text"`
		Title   string `json:"title"`
		Caption string `json:"caption"`
	}
	result := make([]mediaItem, len(items))
	for i, m := range items {
		result[i] = mediaItem{
			ID:      m.ID,
			URL:     m.Path,
			Name:    m.OriginalName,
			AltText: m.AltText,
			Title:   m.Title,
			Caption: m.Caption,
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"items": result,
		"total": total,
		"page":  page,
		"pages": (total + int64(perPage) - 1) / int64(perPage),
	})
}

// MediaUploadJSON handles AJAX file uploads and returns the media URL as JSON.
func (h *Handler) MediaUploadJSON(c *gin.Context) {
	if !h.checkPermission(c, "media", "create") {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": adminT(h.svc.AdminLanguage(), "error.file_required")})
		return
	}
	var userID uint
	if uid, exists := c.Get("admin_user_id"); exists {
		userID, _ = uid.(uint)
	}
	media, err := h.svc.UploadFile(file, userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.logAction(c, "create", "media", media.ID, media.OriginalName)
	c.JSON(http.StatusOK, gin.H{"url": media.Path, "id": media.ID, "name": media.OriginalName})
}

// MediaUpdateMeta updates SEO metadata (alt_text/title/caption) for a media item.
func (h *Handler) MediaUpdateMeta(c *gin.Context) {
	if !h.checkPermission(c, "media", "update") {
		return
	}
	id := getIDParam(c)
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": adminT(h.svc.AdminLanguage(), "error.invalid_id")})
		return
	}
	altText := strings.TrimSpace(c.PostForm("alt_text"))
	title := strings.TrimSpace(c.PostForm("title"))
	caption := strings.TrimSpace(c.PostForm("caption"))
	if err := h.svc.UpdateMediaMeta(id, altText, title, caption); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.logAction(c, "update", "media", id, "meta")
	c.JSON(http.StatusOK, gin.H{"ok": true, "alt_text": altText, "title": title, "caption": caption})
}

// ==================== Users ====================

func (h *Handler) UserList(c *gin.Context) {
	if !h.checkPermission(c, "user", "read") {
		return
	}
	items, _, _ := h.svc.userRepo.List("", 1, 1000)
	lang := h.svc.AdminLanguage()
	h.render(c, "users", gin.H{
		"Title":  adminT(lang, "nav.users"),
		"Active": "users",
		"Items":  items,
		"Roles":  h.svc.GetRoles(),
	})
}

func (h *Handler) UserNew(c *gin.Context) {
	if !h.checkPermission(c, "user", "create") {
		return
	}
	lang := h.svc.AdminLanguage()
	h.render(c, "user_form", gin.H{
		"Title":  adminT(lang, "user.new"),
		"Active": "users",
		"Roles":  h.svc.GetRoles(),
	})
}

func (h *Handler) UserCreate(c *gin.Context) {
	if !h.checkPermission(c, "user", "create") {
		return
	}
	username := c.PostForm("username")
	email := c.PostForm("email")
	password := c.PostForm("password")
	displayName := c.PostForm("display_name")
	role := c.PostForm("role")
	lang := h.svc.AdminLanguage()

	if username == "" || email == "" || password == "" {
		h.render(c, "user_form", gin.H{
			"Title": adminT(lang, "user.new"), "Active": "users",
			"Roles": h.svc.GetRoles(),
			"Error": adminT(lang, "error.user_required"),
		})
		return
	}

	if err := h.svc.CreateUser(username, email, password, displayName, role); err != nil {
		h.render(c, "user_form", gin.H{
			"Title": adminT(lang, "user.new"), "Active": "users",
			"Roles": h.svc.GetRoles(),
			"Error": adminT(lang, "error.create_failed", err.Error()),
		})
		return
	}
	h.logAction(c, "create", "user", 0, username)
	c.Redirect(http.StatusFound, "/admin/users?success="+url.QueryEscape(adminT(lang, "notice.created")))
}

func (h *Handler) UserEdit(c *gin.Context) {
	if !h.checkPermission(c, "user", "update") {
		return
	}
	item, err := h.svc.userRepo.FindByID(getIDParam(c))
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/users?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.not_found")))
		return
	}
	lang := h.svc.AdminLanguage()
	h.render(c, "user_form", gin.H{
		"Title":  adminT(lang, "user.edit"),
		"Active": "users",
		"Item":   item,
		"Roles":  h.svc.GetRoles(),
	})
}

func (h *Handler) UserUpdate(c *gin.Context) {
	if !h.checkPermission(c, "user", "update") {
		return
	}
	item, err := h.svc.userRepo.FindByID(getIDParam(c))
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/users?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.not_found")))
		return
	}
	lang := h.svc.AdminLanguage()
	item.Email = c.PostForm("email")
	item.DisplayName = c.PostForm("display_name")
	item.Role = c.PostForm("role")

	if password := c.PostForm("password"); password != "" {
		hash, err := user.HashPassword(password)
		if err == nil {
			item.PasswordHash = hash
		}
	}
	if err := h.svc.userRepo.Update(item); err != nil {
		h.render(c, "user_form", gin.H{
			"Title": adminT(lang, "user.edit"), "Active": "users",
			"Item": item, "Roles": h.svc.GetRoles(),
			"Error": adminT(lang, "error.update_failed", err.Error()),
		})
		return
	}
	h.logAction(c, "update", "user", item.ID, item.Username)
	c.Redirect(http.StatusFound, "/admin/users?success="+url.QueryEscape(adminT(lang, "notice.updated")))
}

func (h *Handler) UserDelete(c *gin.Context) {
	if !h.checkPermission(c, "user", "delete") {
		return
	}
	id := getIDParam(c)
	_ = h.svc.userRepo.Delete(id)
	h.logAction(c, "delete", "user", id, "")
	c.Redirect(http.StatusFound, "/admin/users?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.deleted")))
}

// ==================== Themes ====================

func (h *Handler) ThemeList(c *gin.Context) {
	if !h.checkPermission(c, "setting", "read") {
		return
	}
	var themes []ThemeDisplayInfo
	if h.themeManager != nil {
		themes = h.themeManager.AvailableFn()
	}
	lang := h.svc.AdminLanguage()
	h.localizeThemes(themes, lang)
	sort.Slice(themes, func(i, j int) bool {
		if themes[i].Active != themes[j].Active {
			return themes[i].Active
		}
		return themes[i].Name < themes[j].Name
	})
	h.render(c, "themes", gin.H{
		"Title":  adminT(lang, "nav.themes"),
		"Active": "themes",
		"Themes": themes,
	})
}

func (h *Handler) localizeThemes(themes []ThemeDisplayInfo, lang string) {
	for i := range themes {
		catalog := h.themeCatalog(themes[i].Slug)
		if msg := catalogMessage(catalog, lang, "theme.description"); msg != "" {
			themes[i].Description = msg
		}
		if msg := catalogMessage(catalog, lang, "theme.author"); msg != "" {
			themes[i].Author = msg
		}
	}
}

func (h *Handler) ThemeSwitch(c *gin.Context) {
	if !h.checkPermission(c, "setting", "update") {
		return
	}
	slug := c.PostForm("theme")
	if slug == "" {
		c.Redirect(http.StatusFound, "/admin/themes?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.theme_required")))
		return
	}
	if h.themeManager == nil {
		c.Redirect(http.StatusFound, "/admin/themes?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.theme_manager_unavailable")))
		return
	}
	if err := h.themeManager.SwitchFn(slug); err != nil {
		c.Redirect(http.StatusFound, "/admin/themes?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.switch_failed", err.Error())))
		return
	}
	h.logAction(c, "update", "theme", 0, "switch theme to "+slug)
	c.Redirect(http.StatusFound, "/admin/themes?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.theme_switched", slug)))
}

// ThemeDemoImport imports the bundled demo data for a theme.
func (h *Handler) ThemeDemoImport(c *gin.Context) {
	if !h.checkPermission(c, "setting", "update") {
		return
	}
	slug := c.PostForm("theme")
	if slug == "" {
		c.Redirect(http.StatusFound, "/admin/themes?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.theme_missing")))
		return
	}
	if h.themeManager == nil || h.themeManager.ImportDemoFn == nil {
		c.Redirect(http.StatusFound, "/admin/themes?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.theme_demo_unavailable")))
		return
	}
	if err := h.themeManager.ImportDemoFn(slug); err != nil {
		c.Redirect(http.StatusFound, "/admin/themes?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.import_failed", err.Error())))
		return
	}
	h.logAction(c, "create", "theme_demo", 0, "import theme demo data: "+slug)
	c.Redirect(http.StatusFound, "/admin/themes?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.theme_demo_imported")))
}

// ThemeSettings displays the settings page for a specific theme.
func (h *Handler) ThemeSettings(c *gin.Context) {
	if !h.checkPermission(c, "setting", "read") {
		return
	}
	slug := c.Param("slug")

	// Find theme display info
	var themeName string
	if h.themeManager != nil {
		for _, t := range h.themeManager.AvailableFn() {
			if t.Slug == slug {
				themeName = t.Name
				break
			}
		}
	}
	if themeName == "" {
		c.Redirect(http.StatusFound, "/admin/themes?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.theme_not_found")))
		return
	}

	// Get theme settings template path
	tmplPath := ""
	if h.themeManager.SettingsTemplateFn != nil {
		tmplPath = h.themeManager.SettingsTemplateFn(slug)
	}
	if tmplPath == "" {
		c.Redirect(http.StatusFound, "/admin/themes?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.theme_settings_unavailable")))
		return
	}

	// Compile the theme settings template with admin layout
	layout := filepath.Join(h.tmplDir, "layouts", "admin.tmpl")
	tmpl, err := template.New("").Funcs(h.funcMap).ParseFiles(layout, tmplPath)
	if err != nil {
		log.Printf("Theme settings template error (%s): %v", slug, err)
		c.Redirect(http.StatusFound, "/admin/themes?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.theme_template_failed")))
		return
	}

	colorScheme := h.svc.GetOption("theme_color_scheme")
	if colorScheme == "" {
		colorScheme = "orange"
	}

	data := gin.H{
		"Title":            adminT(h.svc.AdminLanguage(), "title.theme_settings", themeName),
		"Active":           "themes",
		"ThemeName":        themeName,
		"ThemeSlug":        slug,
		"ColorScheme":      colorScheme,
		"Settings":         h.svc.GetAllOptions(),
		"ExtensionCatalog": h.themeCatalog(slug),
	}

	adminLang := h.svc.AdminLanguage()
	data["AdminLanguage"] = adminLang
	data["CurrentUser"] = c.GetString("admin_username")
	data["CurrentRole"] = c.GetString("admin_role")
	data["MenuItems"] = h.buildMenuItems(adminLang)
	data["SiteName"] = h.svc.SiteName()
	data["PublicBaseURL"] = requestBaseURL(c)

	if success := c.Query("success"); success != "" {
		data["Success"] = success
	}
	if errMsg := c.Query("error"); errMsg != "" {
		data["Error"] = errMsg
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "admin", data); err != nil {
		log.Printf("Theme settings render error (%s): %v", slug, err)
		c.String(http.StatusInternalServerError, "Template error")
	}
}

// ThemeSettingsSave saves theme-specific settings.
func (h *Handler) ThemeSettingsSave(c *gin.Context) {
	if !h.checkPermission(c, "setting", "update") {
		return
	}
	slug := c.Param("slug")

	// Save color scheme. Core does not know which palette names a theme defines,
	// so we only sanity-check the shape (CSS-class-safe token) and let the theme
	// own the list of valid values.
	if cs := c.PostForm("color_scheme"); isSafeSchemeToken(cs) {
		h.svc.UpdateSetting("theme_color_scheme", cs)
	}

	// Save all theme-related settings from form. A key is accepted if it is
	// either registered as translatable (themes opt-in via option.RegisterTranslatable
	// during Setup) or matches one of the conventional theme-setting prefixes
	// used for non-translatable values like image URLs and links.
	if c.Request.Form == nil {
		c.Request.ParseForm()
	}
	themeSettingPrefixes := []string{"home_", "about_", "company_", "social_", "footer_", "showcase_", "site_", "nav_", "contact_", "package_"}
	for key, values := range c.Request.PostForm {
		if len(values) == 0 {
			continue
		}
		if !option.IsTranslatable(key) && !hasAnyPrefix(key, themeSettingPrefixes) {
			continue
		}
		h.svc.UpdateSetting(key, values[0])
	}

	h.invalidatePageCache()
	h.fireOptionsBulkUpdated(c)
	h.logAction(c, "update", "theme_settings", 0, "theme settings updated")
	c.Redirect(http.StatusFound, "/admin/themes/"+slug+"/settings?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.theme_settings_saved")))
}

// hasAnyPrefix reports whether s has any of the given prefixes.
func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// isSafeSchemeToken validates a color-scheme value is a short, CSS-class-safe
// token. Themes own the list of accepted palette names; core only guards
// against arbitrary user input ending up in a class attribute.
func isSafeSchemeToken(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// fireOptionsBulkUpdated emits the OptionsBulkUpdated hook so subscribers
// (e.g. plugins maintaining derived state from Options) can refresh.
func (h *Handler) fireOptionsBulkUpdated(c *gin.Context) {
	if h.hooks == nil {
		return
	}
	h.hooks.DoAction(c.Request.Context(), hook.OptionsBulkUpdated)
}

// ---- Cache Management ----

// CacheStatus displays cache status and management page.
func (h *Handler) CacheStatus(c *gin.Context) {
	if !h.checkPermission(c, "cache", "read") {
		return
	}
	data := gin.H{
		"Title":  adminT(h.svc.AdminLanguage(), "nav.cache"),
		"Active": "cache_mgmt",
	}
	if h.cacheCallbacks != nil && h.cacheCallbacks.StatusFn != nil {
		data["CacheInfo"] = h.cacheCallbacks.StatusFn()
	}
	if msg := c.Query("success"); msg != "" {
		data["Success"] = msg
	}
	h.render(c, "cache_mgmt", data)
}

// CacheFlush handles cache flush requests.
func (h *Handler) CacheFlush(c *gin.Context) {
	if !h.checkPermission(c, "cache", "delete") {
		return
	}
	if h.cacheCallbacks == nil {
		c.Redirect(http.StatusFound, "/admin/cache?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.cache_not_enabled")))
		return
	}
	target := c.PostForm("target")
	switch target {
	case "all":
		if h.cacheCallbacks.FlushAllFn != nil {
			h.cacheCallbacks.FlushAllFn()
		}
		h.logAction(c, "delete", "cache", 0, "clear all cache")
		c.Redirect(http.StatusFound, "/admin/cache?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.cache_all_flushed")))
	case "page":
		if h.cacheCallbacks.FlushPageFn != nil {
			h.cacheCallbacks.FlushPageFn()
		}
		h.logAction(c, "delete", "cache", 0, "clear page cache")
		c.Redirect(http.StatusFound, "/admin/cache?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.cache_page_flushed")))
	case "fragment":
		if h.cacheCallbacks.FlushFragFn != nil {
			h.cacheCallbacks.FlushFragFn()
		}
		h.logAction(c, "delete", "cache", 0, "clear fragment cache")
		c.Redirect(http.StatusFound, "/admin/cache?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.cache_fragment_flushed")))
	default:
		c.Redirect(http.StatusFound, "/admin/cache?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.unknown_target")))
	}
}

// ---- Redirect Management ----

// RedirectList displays all redirect rules.
func (h *Handler) RedirectList(c *gin.Context) {
	if !h.checkPermission(c, "redirect", "read") {
		return
	}
	data := gin.H{
		"Title":  adminT(h.svc.AdminLanguage(), "nav.redirects"),
		"Active": "redirects",
	}
	if h.redirectCallbacks != nil && h.redirectCallbacks.AllFn != nil {
		data["Redirects"] = h.redirectCallbacks.AllFn()
	}
	if msg := c.Query("success"); msg != "" {
		data["Success"] = msg
	}
	if msg := c.Query("error"); msg != "" {
		data["Error"] = msg
	}
	h.render(c, "redirects", data)
}

// RedirectAdd creates a new redirect rule.
func (h *Handler) RedirectAdd(c *gin.Context) {
	if !h.checkPermission(c, "redirect", "create") {
		return
	}
	if h.redirectCallbacks == nil || h.redirectCallbacks.AddFn == nil {
		c.Redirect(http.StatusFound, "/admin/redirects?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.redirect_unavailable")))
		return
	}
	source := c.PostForm("source")
	target := c.PostForm("target")
	codeStr := c.PostForm("code")
	code := 301
	if codeStr == "302" {
		code = 302
	}
	if source == "" || target == "" {
		c.Redirect(http.StatusFound, "/admin/redirects?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.redirect_required")))
		return
	}
	if err := h.redirectCallbacks.AddFn(source, target, code); err != nil {
		c.Redirect(http.StatusFound, "/admin/redirects?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.add_failed", err.Error())))
		return
	}
	h.logAction(c, "create", "redirect", 0, source+" → "+target)
	c.Redirect(http.StatusFound, "/admin/redirects?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.redirect_added")))
}

// RedirectRemove deletes a redirect rule by source path.
func (h *Handler) RedirectRemove(c *gin.Context) {
	if !h.checkPermission(c, "redirect", "delete") {
		return
	}
	if h.redirectCallbacks == nil || h.redirectCallbacks.RemoveFn == nil {
		c.Redirect(http.StatusFound, "/admin/redirects?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.redirect_unavailable")))
		return
	}
	source := c.PostForm("source")
	if source == "" {
		c.Redirect(http.StatusFound, "/admin/redirects?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.redirect_source_required")))
		return
	}
	if err := h.redirectCallbacks.RemoveFn(source); err != nil {
		c.Redirect(http.StatusFound, "/admin/redirects?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.delete_failed", err.Error())))
		return
	}
	h.logAction(c, "delete", "redirect", 0, "delete redirect "+source)
	c.Redirect(http.StatusFound, "/admin/redirects?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.redirect_deleted")))
}

// ---- Plugin Management ----

// PluginList displays all registered plugins with their status.
func (h *Handler) PluginList(c *gin.Context) {
	if !h.checkPermission(c, "plugin", "read") {
		return
	}
	data := gin.H{
		"Title":  adminT(h.svc.AdminLanguage(), "nav.plugins"),
		"Active": "plugins",
	}
	if h.pluginCallbacks != nil && h.pluginCallbacks.AllFn != nil {
		plugins := h.pluginCallbacks.AllFn()
		h.localizePlugins(plugins, h.svc.AdminLanguage())
		sort.Slice(plugins, func(i, j int) bool {
			if plugins[i].Active != plugins[j].Active {
				return plugins[i].Active
			}
			return plugins[i].Name < plugins[j].Name
		})
		data["Plugins"] = plugins
	}
	if msg := c.Query("success"); msg != "" {
		data["Success"] = msg
	}
	if msg := c.Query("error"); msg != "" {
		data["Error"] = msg
	}
	h.render(c, "plugins", data)
}

func (h *Handler) localizePlugins(plugins []PluginInfo, lang string) {
	for i := range plugins {
		catalog := h.pluginCatalog(plugins[i].Slug)
		if msg := catalogMessage(catalog, lang, "plugin.description"); msg != "" {
			plugins[i].Description = msg
		}
	}
}

// PluginActivate activates a registered plugin.
func (h *Handler) PluginActivate(c *gin.Context) {
	if !h.checkPermission(c, "plugin", "update") {
		return
	}
	if h.pluginCallbacks == nil || h.pluginCallbacks.ActivateFn == nil {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.plugin_unavailable")))
		return
	}
	name := c.PostForm("name")
	if name == "" {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.plugin_name_required")))
		return
	}
	if err := h.pluginCallbacks.ActivateFn(name); err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.activate_failed", err.Error())))
		return
	}
	h.logAction(c, "update", "plugin", 0, "activate plugin "+name)
	c.Redirect(http.StatusFound, "/admin/plugins?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.plugin_activated", name)))
}

// PluginDeactivate deactivates an active plugin.
func (h *Handler) PluginDeactivate(c *gin.Context) {
	if !h.checkPermission(c, "plugin", "update") {
		return
	}
	if h.pluginCallbacks == nil || h.pluginCallbacks.DeactivateFn == nil {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.plugin_unavailable")))
		return
	}
	name := c.PostForm("name")
	if name == "" {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.plugin_name_required")))
		return
	}
	if err := h.pluginCallbacks.DeactivateFn(name); err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.deactivate_failed", err.Error())))
		return
	}
	h.logAction(c, "update", "plugin", 0, "deactivate plugin "+name)
	c.Redirect(http.StatusFound, "/admin/plugins?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.plugin_deactivated", name)))
}

// PluginSettings displays the settings page for a specific plugin.
func (h *Handler) PluginSettings(c *gin.Context) {
	if !h.checkPermission(c, "plugin", "read") {
		return
	}
	slug := c.Param("slug")

	// Find plugin display info
	var pluginName string
	if h.pluginCallbacks != nil && h.pluginCallbacks.AllFn != nil {
		for _, p := range h.pluginCallbacks.AllFn() {
			if p.Slug == slug && p.Active && p.HasSettings {
				pluginName = p.Name
				break
			}
		}
	}
	if pluginName == "" {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.plugin_settings_missing")))
		return
	}

	// Get plugin settings template path
	tmplPath := ""
	if h.pluginCallbacks.SettingsTemplateFn != nil {
		tmplPath = h.pluginCallbacks.SettingsTemplateFn(slug)
	}
	if tmplPath == "" {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.plugin_settings_unavailable")))
		return
	}

	// Compile the plugin settings template with admin layout
	layout := filepath.Join(h.tmplDir, "layouts", "admin.tmpl")
	tmpl, err := template.New("").Funcs(h.funcMap).ParseFiles(layout, tmplPath)
	if err != nil {
		log.Printf("Plugin settings template error (%s): %v", slug, err)
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.plugin_template_failed")))
		return
	}

	data := gin.H{
		"Title":            adminT(h.svc.AdminLanguage(), "title.plugin_settings", pluginName),
		"Active":           "plugins",
		"PluginName":       pluginName,
		"PluginSlug":       slug,
		"Settings":         h.svc.GetAllOptions(),
		"ExtensionCatalog": h.pluginCatalog(slug),
	}

	// Merge plugin-specific extra data
	if h.pluginCallbacks.SettingsDataFn != nil {
		if extra := h.pluginCallbacks.SettingsDataFn(slug); extra != nil {
			for k, v := range extra {
				data[k] = v
			}
		}
	}

	adminLang := h.svc.AdminLanguage()
	data["AdminLanguage"] = adminLang
	data["CurrentUser"] = c.GetString("admin_username")
	data["CurrentRole"] = c.GetString("admin_role")
	data["MenuItems"] = h.buildMenuItems(adminLang)
	data["SiteName"] = h.svc.SiteName()
	data["PublicBaseURL"] = requestBaseURL(c)

	if success := c.Query("success"); success != "" {
		data["Success"] = success
	}
	if errMsg := c.Query("error"); errMsg != "" {
		data["Error"] = errMsg
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "admin", data); err != nil {
		log.Printf("Plugin settings render error (%s): %v", slug, err)
		c.String(http.StatusInternalServerError, "Template error")
	}
}

// PluginSettingsSave saves plugin-specific settings.
func (h *Handler) PluginSettingsSave(c *gin.Context) {
	if !h.checkPermission(c, "plugin", "update") {
		return
	}
	slug := c.Param("slug")

	// Save all plugin_<slug>_* settings from form
	prefix := "plugin_" + slug + "_"
	if c.Request.Form == nil {
		c.Request.ParseForm()
	}
	for key, values := range c.Request.PostForm {
		if strings.HasPrefix(key, prefix) && len(values) > 0 {
			h.svc.UpdateSetting(key, values[0])
		}
	}

	h.invalidatePageCache()
	h.fireOptionsBulkUpdated(c)
	h.logAction(c, "update", "plugin_settings", 0, "update plugin settings: "+slug)

	// Notify the plugin that settings were saved
	if h.pluginCallbacks != nil && h.pluginCallbacks.SettingsSaveFn != nil {
		saved := make(map[string]string)
		for key, values := range c.Request.PostForm {
			if strings.HasPrefix(key, prefix) && len(values) > 0 {
				saved[key] = values[0]
			}
		}
		h.pluginCallbacks.SettingsSaveFn(slug, saved)
	}

	c.Redirect(http.StatusFound, "/admin/plugins/"+slug+"/settings?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.plugin_settings_saved")))
}
