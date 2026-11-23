// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package admin

import (
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
)

// ==================== Settings ====================

func (h *Handler) SettingList(c *gin.Context) {
	if !h.checkPermission(c, "setting", "read") {
		return
	}
	adminLang := h.svc.AdminLanguage()
	items := h.svc.GetAllSettings(adminLang)
	data := gin.H{
		"Title":   adminT(adminLang, "settings.system_settings"),
		"Active":  "settings",
		"Items":   items,
		"Modules": h.optInModules(adminLang),
	}
	// Developer options are their own form and endpoint (D1 4.4), shown
	// only to those who may change them.
	if h.packageManager != nil && h.can(c, "extension", "developer_mode") {
		mode := h.packageManager.DeveloperMode()
		data["DeveloperOptions"] = &mode
		data["ExtensionText"] = extensionScriptText(adminLang)
		if _, on, err := h.secondFactorEnabled(adminUserID(c)); err == nil && on {
			data["DeveloperSecondFactor"] = true
		}
	}
	h.render(c, "settings", data)
}

// optInModules returns the opt-in (default-inactive) plugins for the settings
// Modules panel, where the operator toggles them on/off.
func (h *Handler) optInModules(lang string) []PluginInfo {
	if h.pluginCallbacks == nil {
		return nil
	}
	var mods []PluginInfo
	for _, p := range h.pluginCallbacks.All() {
		if p.DefaultInactive {
			mods = append(mods, p)
		}
	}
	h.localizePlugins(mods, lang)
	return mods
}

func (h *Handler) SettingUpdate(c *gin.Context) {
	if !h.checkPermission(c, "setting", "update") {
		return
	}
	adminLang := h.svc.AdminLanguage()
	// new_key adds a custom option. It cannot create or overwrite one that
	// core manages: those have their own fields and pages, which check
	// their own capabilities and rules (switching themes, activating
	// plugins, developer mode).
	newKey := strings.TrimSpace(c.PostForm("new_key"))
	if newKey != "" && coreOwnedOption(newKey) {
		c.Redirect(http.StatusFound, "/admin/settings?error="+url.QueryEscape(adminT(adminLang, "error.setting_key_reserved")))
		return
	}
	items := h.svc.GetAllSettings(adminLang)
	for _, s := range items {
		if s.Key != "site_icon" || s.ReadOnly {
			continue
		}
		newValue := c.PostForm(s.Key)
		if err := h.svc.SyncSiteIcon(newValue); err != nil {
			c.Redirect(http.StatusFound, "/admin/settings?error="+url.QueryEscape(err.Error()))
			return
		}
		break
	}
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
		if err := h.validateSystemSettingValue(s.Key, newValue, adminLang); err != nil {
			c.Redirect(http.StatusFound, "/admin/settings?error="+url.QueryEscape(err.Error()))
			return
		}
		if newValue != s.Value {
			h.svc.UpdateSetting(s.Key, newValue)
		}
	}
	if newKey != "" {
		h.svc.CreateSetting(newKey, c.PostForm("new_value"))
	}
	h.invalidatePageCache()
	h.fireOptionsBulkUpdated(c)
	h.logAction(c, "update", "setting", 0, "system settings updated")
	c.Redirect(http.StatusFound, "/admin/settings?success="+url.QueryEscape(adminT(adminLang, "notice.settings_updated")))
}

// MailSettings shows SMTP transport and notification settings.
func (h *Handler) MailSettings(c *gin.Context) {
	if !h.checkPermission(c, "setting", "read") {
		return
	}
	lang := h.svc.AdminLanguage()
	h.render(c, "mail_settings", gin.H{
		"Title":  adminT(lang, "nav.mail"),
		"Active": "mail",
		"Mail":   h.svc.MailSettings(lang),
	})
}

// MailSettingsSave persists SMTP transport settings to site config.toml and
// notification switches to options.
func (h *Handler) MailSettingsSave(c *gin.Context) {
	if !h.checkPermission(c, "setting", "update") {
		return
	}
	lang := h.svc.AdminLanguage()
	form := map[string]string{
		"driver":                     c.PostForm("driver"),
		"enabled":                    boolPost(c, "enabled"),
		"host":                       c.PostForm("host"),
		"port":                       c.PostForm("port"),
		"encryption":                 c.PostForm("encryption"),
		"username":                   c.PostForm("username"),
		"mail_key":                   c.PostForm("mail_key"),
		"clear_mail_key":             boolPost(c, "clear_mail_key"),
		"from_email":                 c.PostForm("from_email"),
		"from_name":                  c.PostForm("from_name"),
		"reply_to":                   c.PostForm("reply_to"),
		"timeout_seconds":            c.PostForm("timeout_seconds"),
		"contact_message_notify":     boolPost(c, "contact_message_notify"),
		"contact_message_recipients": c.PostForm("contact_message_recipients"),
	}
	if err := h.svc.UpdateMailSettings(form); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail?error="+url.QueryEscape(err.Error()))
		return
	}
	h.logAction(c, "update", "mail", 0, "mail settings updated")
	c.Redirect(http.StatusFound, "/admin/mail?success="+url.QueryEscape(adminT(lang, "notice.mail_settings_updated")))
}

// MailTest sends a test email using the currently saved SMTP settings.
func (h *Handler) MailTest(c *gin.Context) {
	if !h.checkPermission(c, "setting", "update") {
		return
	}
	lang := h.svc.AdminLanguage()
	recipient := c.PostForm("test_recipient")
	if err := h.svc.SendTestMail(c.Request.Context(), recipient); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail?error="+url.QueryEscape(err.Error()))
		return
	}
	h.logAction(c, "test", "mail", 0, "test email sent")
	c.Redirect(http.StatusFound, "/admin/mail?success="+url.QueryEscape(adminT(lang, "notice.mail_test_sent", recipient)))
}

func boolPost(c *gin.Context, key string) string {
	if c.PostForm(key) != "" {
		return "1"
	}
	return "0"
}

// SitemapGenerate generates a static sitemap.xml file under the active site's
// public directory.
func (h *Handler) SitemapGenerate(c *gin.Context) {
	if !h.checkPermission(c, "setting", "update") {
		return
	}
	if h.sitemapCallbacks == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": adminT(h.svc.AdminLanguage(), "error.sitemap_unconfigured")})
		return
	}
	count, err := h.sitemapCallbacks.Generate()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.logAction(c, "generate", "sitemap", 0, fmt.Sprintf("generated sitemap.xml with %d URLs", count))
	c.JSON(http.StatusOK, gin.H{"success": true, "count": count})
}

// ==================== Media ====================

const adminMediaPerPage = 20

func (h *Handler) MediaList(c *gin.Context) {
	if !h.checkPermission(c, "media", "read") {
		return
	}
	page := parseAdminPage(c)
	items, total, err := h.svc.mediaRepo.List("", page, adminMediaPerPage)
	if err != nil {
		c.String(http.StatusServiceUnavailable, err.Error())
		return
	}
	totalPages := mediaTotalPages(total, adminMediaPerPage)
	if totalPages > 0 && page > totalPages {
		c.Redirect(http.StatusFound, mediaListPageURL(c, totalPages, totalPages))
		return
	}
	lang := h.svc.AdminLanguage()
	h.render(c, "media", gin.H{
		"Title":      adminT(lang, "nav.media"),
		"Active":     "media",
		"Items":      items,
		"Pagination": buildMediaPagination(c, total, page, adminMediaPerPage, len(items)),
		"PageHiddenInputs": hiddenInputsFromQueryExcept(c.Request.URL.Query(), map[string]bool{
			"page": true, "success": true, "error": true,
		}),
		"MediaVariantJob": h.svc.MediaVariantJobStatus(),
	})
}

func mediaTotalPages(total int64, perPage int) int {
	if total <= 0 || perPage <= 0 {
		return 0
	}
	return int((total + int64(perPage) - 1) / int64(perPage))
}

func mediaListPageURL(c *gin.Context, targetPage, totalPages int) string {
	if targetPage < 1 {
		targetPage = 1
	}
	if totalPages > 0 && targetPage > totalPages {
		targetPage = totalPages
	}
	q := c.Request.URL.Query()
	q.Del("success")
	q.Del("error")
	q.Set("page", strconv.Itoa(targetPage))
	target := c.Request.URL.Path
	if encoded := q.Encode(); encoded != "" {
		target += "?" + encoded
	}
	return target
}

func buildMediaPagination(c *gin.Context, total int64, page, perPage, itemCount int) AdminPaginationView {
	totalPages := mediaTotalPages(total, perPage)
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	var from, to int64
	if total > 0 && itemCount > 0 {
		from = int64((page-1)*perPage) + 1
		to = from + int64(itemCount) - 1
	}
	return AdminPaginationView{
		Total: total, Page: page, PerPage: perPage, TotalPages: totalPages,
		From: from, To: to, Offset: (page - 1) * perPage,
		FirstURL: mediaListPageURL(c, 1, totalPages), PrevURL: mediaListPageURL(c, page-1, totalPages),
		NextURL: mediaListPageURL(c, page+1, totalPages), LastURL: mediaListPageURL(c, totalPages, totalPages),
	}
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
	page := parseAdminPage(c)
	perPage := adminMediaPerPage
	items, total, err := h.svc.mediaRepo.List("image", page, perPage)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
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
		"pages": mediaTotalPages(total, perPage),
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
	h.renderUserForm(c, nil, gin.H{"Title": adminT(lang, "user.new")})
}

// renderUserForm draws the user form. It asks for a second-factor code
// when the current user has two-step verification on (making someone a
// super admin and setting a password need one), and offers to reset the
// edited user's two-step verification.
func (h *Handler) renderUserForm(c *gin.Context, item *user.User, data gin.H) {
	data["Active"] = "users"
	data["Roles"] = h.svc.GetRoles()
	if item != nil {
		data["Item"] = item
		if item.ID != adminUserID(c) {
			if _, on, err := h.secondFactorEnabled(item.ID); err == nil && on {
				data["TargetSecondFactor"] = true
			}
		}
	}
	if _, on, err := h.secondFactorEnabled(adminUserID(c)); err == nil && on {
		data["AskSecondFactor"] = true
	}
	h.render(c, "user_form", data)
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
		h.renderUserForm(c, nil, gin.H{"Title": adminT(lang, "user.new"), "Error": adminT(lang, "error.user_required")})
		return
	}
	// A new super admin needs a code from an account with two-step
	// verification on.
	if role == user.RoleSuperAdmin {
		if code := h.stepUp(c); code != "" {
			h.renderUserForm(c, nil, gin.H{"Title": adminT(lang, "user.new"), "Error": adminT(lang, "mfa.error."+code)})
			return
		}
	}

	if err := h.svc.CreateUser(username, email, password, displayName, role); err != nil {
		h.renderUserForm(c, nil, gin.H{"Title": adminT(lang, "user.new"), "Error": adminT(lang, "error.create_failed", err.Error())})
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
	h.renderUserForm(c, item, gin.H{"Title": adminT(lang, "user.edit")})
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
	// Making someone a super admin and setting a password need a code from
	// an account with two-step verification on.
	newRole := c.PostForm("role")
	if (newRole == user.RoleSuperAdmin && item.Role != user.RoleSuperAdmin) || c.PostForm("password") != "" {
		if code := h.stepUp(c); code != "" {
			h.renderUserForm(c, item, gin.H{"Title": adminT(lang, "user.edit"), "Error": adminT(lang, "mfa.error."+code)})
			return
		}
	}
	item.Email = user.EmailPointer(c.PostForm("email"))
	item.DisplayName = c.PostForm("display_name")
	item.Role = newRole
	// Unchecked checkboxes are omitted from form submissions. Persist the
	// resulting false value explicitly so administrators can disable an
	// account, and accept any submitted value as the checked state.
	item.IsActive = c.PostForm("is_active") != ""

	if password := c.PostForm("password"); password != "" {
		hash, err := user.HashPassword(password)
		if err == nil {
			item.PasswordHash = hash
		}
	}
	if err := h.svc.userRepo.Update(item); err != nil {
		h.renderUserForm(c, item, gin.H{"Title": adminT(lang, "user.edit"), "Error": adminT(lang, "error.update_failed", err.Error())})
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
	if h.svc.secondFactor != nil {
		_ = h.svc.secondFactor.Remove(c.Request.Context(), id)
	}
	h.logAction(c, "delete", "user", id, "")
	c.Redirect(http.StatusFound, "/admin/users?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.deleted")))
}

// ==================== Themes ====================

func (h *Handler) ThemeList(c *gin.Context) {
	if !h.checkPermission(c, "setting", "read") {
		return
	}
	var themes []ThemeDisplayInfo
	var issues []ThemeLoadIssueInfo
	if h.themeManager != nil {
		themes = h.themeManager.Available()
		for _, issue := range h.themeManager.LoadIssues() {
			issue.Reason = truncateRunes(issue.Reason, maxThemeIssueRunes)
			issues = append(issues, issue)
		}
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
		"Title":           adminT(lang, "nav.themes"),
		"Active":          "themes",
		"Themes":          themes,
		"ThemeLoadIssues": issues,
	})
}

// maxThemeIssueRunes caps a failed theme's reason on the themes page; the
// full error is in the server log.
const maxThemeIssueRunes = 300

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
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
	if err := h.themeManager.Switch(slug); err != nil {
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
	if h.themeManager == nil {
		c.Redirect(http.StatusFound, "/admin/themes?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.theme_demo_unavailable")))
		return
	}
	if err := h.themeManager.ImportDemo(slug); err != nil {
		c.Redirect(http.StatusFound, "/admin/themes?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.import_failed", err.Error())))
		return
	}
	h.logAction(c, "create", "theme_demo", 0, "import theme demo data: "+slug)
	c.Redirect(http.StatusFound, "/admin/themes?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.theme_demo_imported")))
}

// validateSystemSettingValue applies the system settings rules to a write of
// a core-owned option. Keys that core does not own are always accepted here;
// their shape is the owning theme's or plugin's concern.
func (h *Handler) validateSystemSettingValue(key, value, lang string) error {
	def, ok := option.SystemDefinition(key)
	if !ok {
		return nil
	}
	if def.ReadOnly {
		return errors.New(adminT(lang, "error.permission_denied"))
	}
	if key == "site_timezone" && !config.IsValidTimezone(value) {
		return errors.New(adminT(lang, "error.invalid_timezone"))
	}
	if !h.svc.IsValidSystemSettingValue(key, value) {
		return errors.New(adminT(lang, "error.invalid_registration_role"))
	}
	return nil
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
	if h.cacheCallbacks != nil {
		data["CacheInfo"] = h.cacheCallbacks.Status()
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
		h.cacheCallbacks.FlushAll()
		h.logAction(c, "delete", "cache", 0, "clear all cache")
		c.Redirect(http.StatusFound, "/admin/cache?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.cache_all_flushed")))
	case "page":
		h.cacheCallbacks.FlushPage()
		h.logAction(c, "delete", "cache", 0, "clear page cache")
		c.Redirect(http.StatusFound, "/admin/cache?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.cache_page_flushed")))
	case "fragment":
		h.cacheCallbacks.FlushFrag()
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
	if h.redirectCallbacks != nil {
		data["Redirects"] = h.redirectCallbacks.All()
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
	if h.redirectCallbacks == nil {
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
	if err := h.redirectCallbacks.Add(source, target, code); err != nil {
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
	if h.redirectCallbacks == nil {
		c.Redirect(http.StatusFound, "/admin/redirects?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.redirect_unavailable")))
		return
	}
	source := c.PostForm("source")
	if source == "" {
		c.Redirect(http.StatusFound, "/admin/redirects?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.redirect_source_required")))
		return
	}
	if err := h.redirectCallbacks.Remove(source); err != nil {
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
	if h.pluginCallbacks != nil {
		plugins := h.pluginCallbacks.All()
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
		if msg := catalogMessage(catalog, lang, "plugin.name"); msg != "" {
			plugins[i].DisplayName = msg
		}
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
	if h.pluginCallbacks == nil {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.plugin_unavailable")))
		return
	}
	name := c.PostForm("name")
	if name == "" {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.plugin_name_required")))
		return
	}
	if err := h.pluginCallbacks.Activate(name); err != nil {
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
	if h.pluginCallbacks == nil {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.plugin_unavailable")))
		return
	}
	name := c.PostForm("name")
	if name == "" {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.plugin_name_required")))
		return
	}
	if err := h.pluginCallbacks.Deactivate(name); err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.deactivate_failed", err.Error())))
		return
	}
	h.logAction(c, "update", "plugin", 0, "deactivate plugin "+name)
	c.Redirect(http.StatusFound, "/admin/plugins?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.plugin_deactivated", name)))
}

// PluginSettings displays the settings page for a specific plugin.
func (h *Handler) PluginSettings(c *gin.Context) {
	slug := c.Param("slug")
	if !h.checkPermission(c, h.pluginSettingsResource(slug), "read") {
		return
	}

	// Find plugin display info
	var pluginName string
	if h.pluginCallbacks != nil {
		for _, p := range h.pluginCallbacks.All() {
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
	if msg := catalogMessage(h.pluginCatalog(slug), h.svc.AdminLanguage(), "plugin.name"); msg != "" {
		pluginName = msg
	}

	// Locate the plugin settings template
	settingsTmpl := h.pluginCallbacks.SettingsTemplate(slug)
	if !settingsTmpl.Valid() {
		c.Redirect(http.StatusFound, "/admin/plugins?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.plugin_settings_unavailable")))
		return
	}

	// Compile the plugin settings template with admin layout
	tmpl, err := h.parseWithAdminLayout(settingsTmpl)
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
	if extra := h.pluginCallbacks.SettingsData(slug); extra != nil {
		for k, v := range extra {
			data[k] = v
		}
	}

	adminLang := h.svc.AdminLanguage()
	data["AdminLanguage"] = adminLang
	data["CurrentUser"] = c.GetString("admin_username")
	data["CurrentRole"] = c.GetString("admin_role")
	data["MenuItems"] = h.buildMenuItems(adminLang, c.GetString("admin_role"))
	data["SiteName"] = h.svc.SiteName()
	data["PublicBaseURL"] = requestBaseURL(c)
	h.injectLayoutBanners(c, data)

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
	slug := c.Param("slug")
	if !h.checkPermission(c, h.pluginSettingsResource(slug), "update") {
		return
	}

	// Collect all plugin_<slug>_* settings, validate the complete submission,
	// then persist. A rejected payload must not partially update plugin options.
	prefix := "plugin_" + slug + "_"
	if c.Request.Form == nil {
		c.Request.ParseForm()
	}
	saved := make(map[string]string)
	for key, values := range c.Request.PostForm {
		if strings.HasPrefix(key, prefix) && len(values) > 0 {
			saved[key] = values[0]
		}
	}
	if h.pluginCallbacks != nil {
		if err := h.pluginCallbacks.ValidateSettings(slug, saved); err != nil {
			c.Redirect(http.StatusFound, "/admin/plugins/"+slug+"/settings?error="+url.QueryEscape(err.Error()))
			return
		}
	}
	if err := h.svc.UpdateSettings(saved); err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins/"+slug+"/settings?error="+url.QueryEscape(err.Error()))
		return
	}

	h.invalidatePageCache()
	h.fireOptionsBulkUpdated(c)
	h.logAction(c, "update", "plugin_settings", 0, "update plugin settings: "+slug)

	// Notify the plugin that settings were saved
	if h.pluginCallbacks != nil {
		h.pluginCallbacks.SettingsSave(slug, saved)
	}

	c.Redirect(http.StatusFound, "/admin/plugins/"+slug+"/settings?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.plugin_settings_saved")))
}
