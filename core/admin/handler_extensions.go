// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
)

// Extension package endpoints (D1 4.4, 4.9). They answer JSON for the
// market and package pages, and each checks its own capability:
//
//	GET  /admin/themes/market                extension.read (the page)
//	GET  /admin/extensions/themes/catalog    extension.read
//	GET  /admin/extensions/themes            extension.read
//	POST /admin/extensions/themes/install    extension.install
//	POST /admin/extensions/themes/upload     extension.install
//	POST /admin/extensions/themes/rollback   extension.install
//	POST /admin/extensions/themes/uninstall  extension.delete
//	POST /admin/settings/developer-mode      extension.developer_mode
//
// Only super_admin holds them by default. A denied request gets 403 JSON.
// Turning developer mode on also takes a confirmation and the account's
// password, with failed passwords limited per account.

// maxPackageUploadBytes caps an upload request: the 10 MB package limit plus
// room for the multipart framing and form fields.
const maxPackageUploadBytes = 10<<20 + 64<<10

// packageRequestDeadline replaces the server's 15-second read and write
// timeouts for requests that upload or download a package.
const packageRequestDeadline = 2 * time.Minute

// maxReportedProblems caps the check problems returned to the page.
const maxReportedProblems = 20

// Developer mode password attempts: per account, within the window.
const (
	developerModeMaxFailures = 5
	developerModeWindow      = 15 * time.Minute
)

// PackageManager provides extension package callbacks to the admin handler.
type PackageManager struct {
	ListFn             func(ctx context.Context) (*PackageList, error)
	CatalogFn          func(ctx context.Context, lang string) (*CatalogView, error)
	MarketFn           func() MarketInfo
	RevokedActiveFn    func() *RevokedThemeBanner
	InstallFn          func(ctx context.Context, slug, version string, actor uint) (*PackageOutcome, error)
	UploadFn           func(ctx context.Context, data []byte, confirmDigest string, actor uint) (*PackageOutcome, error)
	RollbackFn         func(ctx context.Context, id int64) (*PackageOutcome, error)
	UninstallFn        func(ctx context.Context, slug string) (*PackageOutcome, error)
	DeveloperModeFn    func() DeveloperModeInfo
	SetDeveloperModeFn func(on bool) error
}

// PackageVersionView is one installed version of a package.
type PackageVersionView struct {
	ID           int64     `json:"id"`
	Version      string    `json:"version"`
	Digest       string    `json:"digest"`
	Size         int64     `json:"size"`
	Source       string    `json:"source"`
	Signed       bool      `json:"signed"`
	PublisherKID string    `json:"publisher_kid,omitempty"`
	Status       string    `json:"status"`
	Current      bool      `json:"current"`
	InstalledAt  time.Time `json:"installed_at"`
	InstalledBy  *uint     `json:"installed_by,omitempty"`
}

// PackageView is one installed package with its versions.
type PackageView struct {
	Slug     string               `json:"slug"`
	Active   bool                 `json:"active"`
	Running  string               `json:"running,omitempty"`
	Preview  bool                 `json:"preview,omitempty"`
	Issue    string               `json:"issue,omitempty"`
	Update   *PackageUpdateView   `json:"update,omitempty"`
	Versions []PackageVersionView `json:"versions"`
}

// PackageUpdateView is a newer version the daily update check found.
type PackageUpdateView struct {
	LatestVersion string `json:"latest_version"`
	Severity      string `json:"severity"`
	ReleaseURL    string `json:"release_url,omitempty"`
}

// MarketInfo describes the market the site uses.
type MarketInfo struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
	Host    string `json:"host,omitempty"`
}

// DeveloperModeInfo is the developer mode state.
type DeveloperModeInfo struct {
	Enabled bool `json:"enabled"`
	Locked  bool `json:"locked"`
}

// PackageList answers GET /admin/extensions/themes.
type PackageList struct {
	Market        MarketInfo        `json:"market"`
	DeveloperMode DeveloperModeInfo `json:"developer_mode"`
	Packages      []PackageView     `json:"packages"`
}

// PackageSummaryView describes a checked package, for example the unsigned
// upload an administrator is asked to confirm.
type PackageSummaryView struct {
	Name      string   `json:"name"`
	Slug      string   `json:"slug"`
	Version   string   `json:"version"`
	Digest    string   `json:"digest"`
	Size      int64    `json:"size"`
	Files     int      `json:"files"`
	External  []string `json:"external"`
	Signature string   `json:"signature"`
}

// PackageProblemView is one reason a package failed the check.
type PackageProblemView struct {
	Rule    string `json:"rule"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

// PackageOutcome is a successful package operation.
type PackageOutcome struct {
	Slug    string               `json:"slug"`
	Package *PackageVersionView  `json:"package,omitempty"`
	Notice  string               `json:"notice,omitempty"`
	Summary *PackageSummaryView  `json:"summary,omitempty"`
	Removed []PackageVersionView `json:"removed,omitempty"`
	// Active is true when the package is the theme the site runs: an
	// install or upgrade took effect at once.
	Active bool `json:"active,omitempty"`
}

// CatalogView answers GET /admin/extensions/themes/catalog.
type CatalogView struct {
	Market           MarketInfo           `json:"market"`
	Sequence         int64                `json:"sequence"`
	GeneratedAt      time.Time            `json:"generated_at"`
	Items            []CatalogItemView    `json:"items"`
	RevokedInstalled []RevokedPackageView `json:"revoked_installed"`
}

// CatalogItemView is one theme the market offers, in the admin language.
type CatalogItemView struct {
	Slug          string                `json:"slug"`
	Title         string                `json:"title"`
	Summary       string                `json:"summary,omitempty"`
	Author        string                `json:"author,omitempty"`
	ScreenshotURL string                `json:"screenshot_url,omitempty"`
	ListingURL    string                `json:"listing_url,omitempty"`
	Latest        CatalogVersionView    `json:"latest"`
	Installed     *InstalledVersionView `json:"installed,omitempty"`
	Update        bool                  `json:"update"`
}

// CatalogVersionView is the version of a theme the market page offers.
type CatalogVersionView struct {
	Version      string                   `json:"version"`
	Size         int64                    `json:"size"`
	Core         string                   `json:"core,omitempty"`
	Compatible   bool                     `json:"compatible"`
	External     []string                 `json:"external"`
	Requires     []CatalogRequirementView `json:"requires"`
	PublisherKID string                   `json:"publisher_kid"`
}

// CatalogRequirementView is a plugin a theme needs.
type CatalogRequirementView struct {
	Slug     string `json:"slug"`
	Version  string `json:"version,omitempty"`
	Optional bool   `json:"optional,omitempty"`
}

// InstalledVersionView is the installed current version of a theme.
type InstalledVersionView struct {
	Version string `json:"version"`
	Status  string `json:"status"`
	Signed  bool   `json:"signed"`
	Source  string `json:"source"`
}

// RevokedPackageView is an installed version the market revoked.
type RevokedPackageView struct {
	Slug     string `json:"slug"`
	Version  string `json:"version"`
	Severity string `json:"severity"`
	Reason   string `json:"reason"`
	Note     string `json:"note,omitempty"`
}

// RevokedThemeBanner names the active theme when the market revoked it.
type RevokedThemeBanner struct {
	Slug    string
	Version string
}

// PackageError is a failed package operation. Code is stable; the page
// translates it. Detail is for developers and contains no server paths.
// Reason refines catalog failures (CatalogFailureReason in core).
type PackageError struct {
	Code     string
	Reason   string
	Detail   string
	Problems []PackageProblemView
	Package  *PackageSummaryView
}

func (e *PackageError) Error() string {
	if e.Detail == "" {
		return e.Code
	}
	return e.Code + ": " + e.Detail
}

func (m *PackageManager) List(ctx context.Context) (*PackageList, error) {
	if m == nil || m.ListFn == nil {
		return nil, errAdminCapabilityUnavailable
	}
	return m.ListFn(ctx)
}

func (m *PackageManager) Catalog(ctx context.Context, lang string) (*CatalogView, error) {
	if m == nil || m.CatalogFn == nil {
		return nil, errAdminCapabilityUnavailable
	}
	return m.CatalogFn(ctx, lang)
}

func (m *PackageManager) Market() MarketInfo {
	if m == nil || m.MarketFn == nil {
		return MarketInfo{Mode: "disabled"}
	}
	return m.MarketFn()
}

func (m *PackageManager) RevokedActive() *RevokedThemeBanner {
	if m == nil || m.RevokedActiveFn == nil {
		return nil
	}
	return m.RevokedActiveFn()
}

func (m *PackageManager) Install(ctx context.Context, slug, version string, actor uint) (*PackageOutcome, error) {
	if m == nil || m.InstallFn == nil {
		return nil, errAdminCapabilityUnavailable
	}
	return m.InstallFn(ctx, slug, version, actor)
}

func (m *PackageManager) Upload(ctx context.Context, data []byte, confirmDigest string, actor uint) (*PackageOutcome, error) {
	if m == nil || m.UploadFn == nil {
		return nil, errAdminCapabilityUnavailable
	}
	return m.UploadFn(ctx, data, confirmDigest, actor)
}

func (m *PackageManager) Rollback(ctx context.Context, id int64) (*PackageOutcome, error) {
	if m == nil || m.RollbackFn == nil {
		return nil, errAdminCapabilityUnavailable
	}
	return m.RollbackFn(ctx, id)
}

func (m *PackageManager) Uninstall(ctx context.Context, slug string) (*PackageOutcome, error) {
	if m == nil || m.UninstallFn == nil {
		return nil, errAdminCapabilityUnavailable
	}
	return m.UninstallFn(ctx, slug)
}

func (m *PackageManager) DeveloperMode() DeveloperModeInfo {
	if m == nil || m.DeveloperModeFn == nil {
		return DeveloperModeInfo{}
	}
	return m.DeveloperModeFn()
}

func (m *PackageManager) SetDeveloperMode(on bool) error {
	if m == nil || m.SetDeveloperModeFn == nil {
		return errAdminCapabilityUnavailable
	}
	return m.SetDeveloperModeFn(on)
}

// SetPackageManager injects the extension package callbacks.
func (h *Handler) SetPackageManager(pm *PackageManager) {
	h.packageManager = pm
}

// DeveloperModeBanner is the admin-wide developer mode banner. CanManage
// shows the link to turn the mode off.
type DeveloperModeBanner struct {
	CanManage bool
}

// injectExtensionBanners adds the banners every admin page shows while a
// lasting extension state needs attention. Neither can be dismissed:
//   - developer mode is on (everyone sees it; the link to turn it off only
//     those who may);
//   - the active theme is an installed package the market revoked (those
//     who can open the themes page see it, as with the fallback banner).
func (h *Handler) injectExtensionBanners(c *gin.Context, data gin.H) {
	if h == nil || h.packageManager == nil {
		return
	}
	if h.packageManager.DeveloperMode().Enabled {
		data["DeveloperModeBanner"] = DeveloperModeBanner{CanManage: h.can(c, "extension", "developer_mode")}
	}
	if h.can(c, "setting", "read") {
		if revoked := h.packageManager.RevokedActive(); revoked != nil {
			data["RevokedThemeBanner"] = revoked
		}
	}
}

// requirePermissionJSON is checkPermission for JSON endpoints: a denied
// request gets 403 JSON instead of a redirect.
func (h *Handler) requirePermissionJSON(c *gin.Context, resource, action string) bool {
	if h.can(c, resource, action) {
		return true
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"ok": false, "error": gin.H{
		"code": "forbidden", "message": adminT(h.svc.AdminLanguage(), "error.permission_denied"),
	}})
	return false
}

// packagesJSON returns the package controller, or answers 503 when none is
// wired.
func (h *Handler) packagesJSON(c *gin.Context) (PackageController, bool) {
	if h.packageManager == nil {
		h.packageErrorJSON(c, &PackageError{Code: "unavailable", Detail: "package management is not available"})
		return nil, false
	}
	return h.packageManager, true
}

// packageErrorStatus maps an error code to its HTTP status.
func packageErrorStatus(code string) int {
	switch code {
	case "invalid_request":
		return http.StatusBadRequest
	case "forbidden", "unsigned_not_allowed", "developer_mode_locked":
		return http.StatusForbidden
	case "not_in_catalog", "not_found":
		return http.StatusNotFound
	case "confirmation_required", "theme_active":
		return http.StatusConflict
	case "too_large":
		return http.StatusRequestEntityTooLarge
	case "check_failed", "incompatible", "revoked", "paid", "content_mismatch", "dependencies_unmet",
		"broken", "archive_damaged", "activation_failed", "password_incorrect", "password_unavailable":
		return http.StatusUnprocessableEntity
	case "too_many_attempts":
		return http.StatusTooManyRequests
	case "catalog_unavailable", "download_failed":
		return http.StatusBadGateway
	case "unavailable", "market_disabled":
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func (h *Handler) packageErrorJSON(c *gin.Context, err error) {
	var pe *PackageError
	if !errors.As(err, &pe) {
		pe = &PackageError{Code: "internal", Detail: err.Error()}
	}
	status := packageErrorStatus(pe.Code)
	if status == http.StatusInternalServerError {
		log.Printf("Extension package request %s failed: %v", c.Request.URL.Path, pe)
	}
	errBody := gin.H{"code": pe.Code, "message": pe.Detail}
	if pe.Reason != "" {
		errBody["reason"] = pe.Reason
	}
	body := gin.H{"ok": false, "error": errBody}
	if problems := pe.Problems; len(problems) > 0 {
		if len(problems) > maxReportedProblems {
			problems = problems[:maxReportedProblems]
		}
		body["problems"] = problems
	}
	if pe.Package != nil {
		body["package"] = pe.Package
	}
	c.JSON(status, body)
}

func adminUserID(c *gin.Context) uint {
	if uid, ok := c.Get("admin_user_id"); ok {
		if id, ok := uid.(uint); ok {
			return id
		}
	}
	return 0
}

// extendPackageDeadlines lets a request that moves a package take longer
// than the server's default timeouts. Writers that cannot change deadlines
// (tests) are left alone.
func extendPackageDeadlines(c *gin.Context, read bool) {
	rc := http.NewResponseController(c.Writer)
	deadline := time.Now().Add(packageRequestDeadline)
	_ = rc.SetWriteDeadline(deadline)
	if read {
		_ = rc.SetReadDeadline(deadline)
	}
}

// packageDetails describes a package version for the audit log.
func packageDetails(slug string, v *PackageVersionView) string {
	if v == nil {
		return slug
	}
	return slug + " " + v.Version + " (" + v.Digest + ")"
}

// ThemeMarket renders the theme market page. Its two tabs load their data
// from the JSON endpoints: the catalog, fetched anew on every visit, and the
// installed packages.
func (h *Handler) ThemeMarket(c *gin.Context) {
	if !h.checkPermission(c, "extension", "read") {
		return
	}
	lang := h.svc.AdminLanguage()
	tab := "browse"
	if c.Query("tab") == "installed" {
		tab = "installed"
	}
	market := MarketInfo{Mode: "disabled"}
	var developer DeveloperModeInfo
	if h.packageManager != nil {
		market = h.packageManager.Market()
		developer = h.packageManager.DeveloperMode()
	}
	h.render(c, "theme_market", gin.H{
		"Title":            adminT(lang, "ext.market_title"),
		"Active":           "themes",
		"Tab":              tab,
		"Market":           market,
		"DeveloperMode":    developer,
		"CanInstall":       h.can(c, "extension", "install"),
		"CanDelete":        h.can(c, "extension", "delete"),
		"CanDeveloperMode": h.can(c, "extension", "developer_mode"),
		"CanSwitchTheme":   h.can(c, "setting", "update"),
		"ExtensionText":    extensionScriptText(lang),
	})
}

// ExtensionThemeCatalog fetches the market catalog now and returns it in the
// admin language. A failure returns the error, never an older catalog.
func (h *Handler) ExtensionThemeCatalog(c *gin.Context) {
	if !h.requirePermissionJSON(c, "extension", "read") {
		return
	}
	pm, ok := h.packagesJSON(c)
	if !ok {
		return
	}
	catalog, err := pm.Catalog(c.Request.Context(), h.svc.AdminLanguage())
	if err != nil {
		h.packageErrorJSON(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "catalog": catalog})
}

// ExtensionThemeList lists the installed theme packages.
func (h *Handler) ExtensionThemeList(c *gin.Context) {
	if !h.requirePermissionJSON(c, "extension", "read") {
		return
	}
	pm, ok := h.packagesJSON(c)
	if !ok {
		return
	}
	list, err := pm.List(c.Request.Context())
	if err != nil {
		h.packageErrorJSON(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "market": list.Market, "developer_mode": list.DeveloperMode, "packages": list.Packages})
}

// ExtensionThemeInstall installs or upgrades a theme package from the
// market.
func (h *Handler) ExtensionThemeInstall(c *gin.Context) {
	if !h.requirePermissionJSON(c, "extension", "install") {
		return
	}
	pm, ok := h.packagesJSON(c)
	if !ok {
		return
	}
	extendPackageDeadlines(c, false)
	slug := strings.TrimSpace(c.PostForm("slug"))
	version := strings.TrimSpace(c.PostForm("version"))
	out, err := pm.Install(c.Request.Context(), slug, version, adminUserID(c))
	if err != nil {
		h.packageErrorJSON(c, err)
		return
	}
	h.logAction(c, "install", "extension", 0, "install theme package "+packageDetails(out.Slug, out.Package)+" from the market")
	c.JSON(http.StatusOK, gin.H{"ok": true, "result": out})
}

// ExtensionThemeUpload installs an uploaded theme package archive. An
// unsigned package needs developer mode and a second request that repeats
// the file with confirm_digest set to the digest the first one returned.
func (h *Handler) ExtensionThemeUpload(c *gin.Context) {
	if !h.requirePermissionJSON(c, "extension", "install") {
		return
	}
	pm, ok := h.packagesJSON(c)
	if !ok {
		return
	}
	extendPackageDeadlines(c, true)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPackageUploadBytes)
	file, err := c.FormFile("package")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) || strings.Contains(err.Error(), "request body too large") {
			h.packageErrorJSON(c, &PackageError{Code: "too_large", Detail: "the upload is larger than " + strconv.Itoa(maxPackageUploadBytes) + " bytes"})
			return
		}
		h.packageErrorJSON(c, &PackageError{Code: "invalid_request", Detail: "a package file is required"})
		return
	}
	f, err := file.Open()
	if err != nil {
		h.packageErrorJSON(c, &PackageError{Code: "invalid_request", Detail: err.Error()})
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPackageUploadBytes+1))
	_ = f.Close()
	if err != nil {
		h.packageErrorJSON(c, &PackageError{Code: "invalid_request", Detail: err.Error()})
		return
	}
	out, err := pm.Upload(c.Request.Context(), data, strings.TrimSpace(c.PostForm("confirm_digest")), adminUserID(c))
	if err != nil {
		h.packageErrorJSON(c, err)
		return
	}
	action, signature := "install", ""
	if out.Summary != nil {
		signature = out.Summary.Signature
		if signature != "verified" {
			action = "install_unsigned"
		}
	}
	h.logAction(c, action, "extension", 0, "upload theme package "+packageDetails(out.Slug, out.Package)+", signature "+signature)
	c.JSON(http.StatusOK, gin.H{"ok": true, "result": out})
}

// ExtensionThemeRollback makes a kept version of a theme package current
// again.
func (h *Handler) ExtensionThemeRollback(c *gin.Context) {
	if !h.requirePermissionJSON(c, "extension", "install") {
		return
	}
	pm, ok := h.packagesJSON(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(c.PostForm("id")), 10, 64)
	if err != nil || id <= 0 {
		h.packageErrorJSON(c, &PackageError{Code: "invalid_request", Detail: "a version id is required"})
		return
	}
	out, err := pm.Rollback(c.Request.Context(), id)
	if err != nil {
		h.packageErrorJSON(c, err)
		return
	}
	h.logAction(c, "rollback", "extension", 0, "roll back theme package to "+packageDetails(out.Slug, out.Package))
	c.JSON(http.StatusOK, gin.H{"ok": true, "result": out})
}

// ExtensionThemeUninstall removes a theme package that is not in use.
func (h *Handler) ExtensionThemeUninstall(c *gin.Context) {
	if !h.requirePermissionJSON(c, "extension", "delete") {
		return
	}
	pm, ok := h.packagesJSON(c)
	if !ok {
		return
	}
	out, err := pm.Uninstall(c.Request.Context(), strings.TrimSpace(c.PostForm("slug")))
	if err != nil {
		h.packageErrorJSON(c, err)
		return
	}
	h.logAction(c, "uninstall", "extension", 0, "uninstall theme package "+out.Slug+" ("+strconv.Itoa(len(out.Removed))+" versions)")
	c.JSON(http.StatusOK, gin.H{"ok": true, "result": out})
}

// DeveloperModeUpdate turns developer mode on or off (D1 4.4). Turning it
// on needs confirm=1 and the account's password and is refused while
// config.toml locks the mode; turning it off needs neither.
func (h *Handler) DeveloperModeUpdate(c *gin.Context) {
	if !h.requirePermissionJSON(c, "extension", "developer_mode") {
		return
	}
	pm, ok := h.packagesJSON(c)
	if !ok {
		return
	}
	if c.PostForm("enabled") != "1" {
		if err := pm.SetDeveloperMode(false); err != nil {
			h.packageErrorJSON(c, err)
			return
		}
		h.logAction(c, "update", "developer_mode", 0, "developer mode turned off")
		c.JSON(http.StatusOK, gin.H{"ok": true, "developer_mode": pm.DeveloperMode()})
		return
	}
	if pm.DeveloperMode().Locked {
		h.packageErrorJSON(c, &PackageError{Code: "developer_mode_locked", Detail: "[market] lock_developer_mode in config.toml keeps developer mode off"})
		return
	}
	if c.PostForm("confirm") != "1" {
		h.packageErrorJSON(c, &PackageError{Code: "confirmation_required", Detail: "confirm that you accept the risks"})
		return
	}
	userID := adminUserID(c)
	key := "developer-mode:" + strconv.FormatUint(uint64(userID), 10)
	if h.developerModeThrottle.blocked(key) {
		h.packageErrorJSON(c, &PackageError{Code: "too_many_attempts", Detail: "too many wrong passwords; try again later"})
		return
	}
	account, err := h.svc.userRepo.FindByID(userID)
	if err != nil || account == nil {
		h.packageErrorJSON(c, &PackageError{Code: "forbidden", Detail: "the account was not found"})
		return
	}
	if account.PasswordHash == "" {
		h.packageErrorJSON(c, &PackageError{Code: "password_unavailable", Detail: "the account has no password; set one first"})
		return
	}
	if !user.CheckPassword(account.PasswordHash, c.PostForm("password")) {
		h.developerModeThrottle.fail(key)
		h.packageErrorJSON(c, &PackageError{Code: "password_incorrect", Detail: "the password is not correct"})
		return
	}
	h.developerModeThrottle.reset(key)
	if err := pm.SetDeveloperMode(true); err != nil {
		h.packageErrorJSON(c, err)
		return
	}
	h.logAction(c, "update", "developer_mode", 0, "developer mode turned on")
	c.JSON(http.StatusOK, gin.H{"ok": true, "developer_mode": pm.DeveloperMode()})
}

// extensionScriptPrefix starts the keys of the texts extensions.js shows.
const extensionScriptPrefix = "ext."

var (
	extensionScriptKeysOnce sync.Once
	extensionScriptKeys     []string
)

// extensionScriptText returns the texts of extensions.js in the admin
// language, keyed without the "ext." prefix. The pages embed them as JSON, so
// the script needs no global dictionary.
func extensionScriptText(lang string) map[string]string {
	extensionScriptKeysOnce.Do(func() {
		data, err := adminLocaleFS.ReadFile("locales/" + defaultAdminLanguage + ".json")
		if err != nil {
			return
		}
		var messages map[string]string
		if json.Unmarshal(data, &messages) != nil {
			return
		}
		for key := range messages {
			if strings.HasPrefix(key, extensionScriptPrefix) {
				extensionScriptKeys = append(extensionScriptKeys, key)
			}
		}
		sort.Strings(extensionScriptKeys)
	})
	out := make(map[string]string, len(extensionScriptKeys))
	for _, key := range extensionScriptKeys {
		out[strings.TrimPrefix(key, extensionScriptPrefix)] = adminMessage(lang, key)
	}
	return out
}
