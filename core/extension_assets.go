// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/admin"
	"github.com/0xmattg/go-press/core/assets"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/plugin"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// Extension files: where core reads a theme's or plugin's own templates,
// static files, locales and demo data from (GIP-M0002 S3a).
//
// A module that ships its files in the binary (a theme initialized with
// BaseTheme.InitBaseFS, a plugin implementing plugin.AssetsProvider) is read
// through that filesystem, and core finds its settings page and demo seed at
// the conventional paths. Every other module keeps being read from
// themes/<slug>/ or plugins/<slug>/ on disk, exactly as before.

// PreferDiskAssets reports whether extensions read their directory on disk
// ahead of their built-in files. It is true unless server.mode is "release",
// the same switch that puts gin in release mode, so local development sees
// template and static edits without rebuilding.
func (e *Engine) PreferDiskAssets() bool {
	return e == nil || e.Config == nil || e.Config.Server.Mode != "release"
}

// PluginFS returns the filesystem rooted at p's directory: its built-in files
// when it implements plugin.AssetsProvider (or plugins/<slug>/ on disk outside
// release mode, see PreferDiskAssets), otherwise plugins/<slug>/ on disk.
// Plugins use it to read their own fragments, for example with
// RenderNamespacedInActiveThemeFS and admin.Handler.RenderExtensionPageFS.
func (e *Engine) PluginFS(p plugin.Plugin) fs.FS {
	if p == nil {
		return nil
	}
	diskDir := filepath.Join("plugins", plugin.Slug(p))
	ap, ok := p.(plugin.AssetsProvider)
	if !ok {
		return assets.DirFS(diskDir)
	}
	return assets.Resolve(diskDir, ap.Assets(), e.PreferDiskAssets())
}

// themeFS returns the filesystem rooted at the theme's directory: its asset
// filesystem, or the directory it was registered with (see ThemeDir).
func (e *Engine) themeFS(slug string) fs.FS {
	t, ok := e.lookupTheme(slug)
	if !ok {
		return nil
	}
	return e.themeFSFor(slug, t)
}

// themeFSFor is themeFS for a given theme instance, which need not be in the
// theme set yet.
func (e *Engine) themeFSFor(slug string, t coreTheme.Theme) fs.FS {
	if fsys := coreTheme.AssetFSOf(t); fsys != nil {
		return fsys
	}
	return assets.DirFS(ThemeDir(slug))
}

// diskFallbackThemes lists, sorted, the themes that still read their files
// from their directory on disk instead of an asset filesystem.
func diskFallbackThemes(themes map[string]coreTheme.Theme) []string {
	var slugs []string
	for slug, t := range themes {
		if coreTheme.AssetFSOf(t) == nil {
			slugs = append(slugs, slug)
		}
	}
	sort.Strings(slugs)
	return slugs
}

// diskFallbackPlugins lists, sorted, the plugins that do not implement
// plugin.AssetsProvider and so read their files from plugins/<slug>.
func diskFallbackPlugins(plugins []plugin.Plugin) []string {
	var slugs []string
	for _, p := range plugins {
		if _, ok := p.(plugin.AssetsProvider); !ok {
			slugs = append(slugs, plugin.Slug(p))
		}
	}
	sort.Strings(slugs)
	return slugs
}

// ActiveThemeLocaleMessages returns the active theme's own locale messages as
// language code → message ID → text, as core loaded them from the theme's
// locales/*.json. Plugins that let administrators override theme strings use
// it to list the keys without reading theme directories themselves. The result
// is a copy; overrides and plugin catalogs are not included.
func (e *Engine) ActiveThemeLocaleMessages() map[string]map[string]string {
	if e == nil || e.I18n == nil {
		return map[string]map[string]string{}
	}
	return e.I18n.ThemeMessages()
}

// ThemeSettingsTemplate locates the admin settings template of the theme, or
// returns the zero TemplateRef when the theme has none.
func (e *Engine) ThemeSettingsTemplate(slug string) admin.TemplateRef {
	t, ok := e.lookupTheme(slug)
	if !ok {
		return admin.TemplateRef{}
	}
	if fsys := coreTheme.AssetFSOf(t); fsys != nil {
		if assets.Exists(fsys, coreTheme.SettingsTemplateName) {
			return admin.TemplateRef{FS: fsys, Name: coreTheme.SettingsTemplateName}
		}
		return admin.TemplateRef{}
	}
	if sp, ok := t.(coreTheme.SettingsProvider); ok {
		return admin.TemplateRefFromPath(sp.SettingsTemplatePath())
	}
	return admin.TemplateRef{}
}

// HasThemeDemoData reports whether the theme ships demo data to import.
func (e *Engine) HasThemeDemoData(slug string) bool {
	_, ok := e.themeDemoSeed(slug)
	return ok
}

// themeDemoSeed locates the theme's demo seed.toml.
func (e *Engine) themeDemoSeed(slug string) (admin.TemplateRef, bool) {
	t, ok := e.lookupTheme(slug)
	if !ok {
		return admin.TemplateRef{}, false
	}
	if fsys := coreTheme.AssetFSOf(t); fsys != nil {
		if assets.Exists(fsys, coreTheme.DemoSeedName) {
			return admin.TemplateRef{FS: fsys, Name: coreTheme.DemoSeedName}, true
		}
		return admin.TemplateRef{}, false
	}
	if dp, ok := t.(coreTheme.DemoDataProvider); ok {
		if ref := admin.TemplateRefFromPath(dp.DemoSeedPath()); ref.Valid() {
			return ref, true
		}
	}
	return admin.TemplateRef{}, false
}

// pluginSettingsTemplate locates p's admin settings template, or returns the
// zero TemplateRef when p has no settings page.
func (e *Engine) pluginSettingsTemplate(p plugin.Plugin) admin.TemplateRef {
	if _, ok := p.(plugin.AssetsProvider); ok {
		fsys := e.PluginFS(p)
		if assets.Exists(fsys, plugin.SettingsTemplateName) {
			return admin.TemplateRef{FS: fsys, Name: plugin.SettingsTemplateName}
		}
		return admin.TemplateRef{}
	}
	if sp, ok := p.(plugin.SettingsProvider); ok {
		return admin.TemplateRefFromPath(sp.SettingsTemplatePath())
	}
	return admin.TemplateRef{}
}

// pluginAdminCatalog loads p's admin locale catalog.
func (e *Engine) pluginAdminCatalog(p plugin.Plugin) *coreI18n.Catalog {
	if _, ok := p.(plugin.AssetsProvider); ok {
		return extensionAdminCatalogFS(e.PluginFS(p))
	}
	if sp, ok := p.(plugin.SettingsProvider); ok {
		if baseDir := extensionBaseFromSettingsTemplate(sp.SettingsTemplatePath()); baseDir != "" {
			if catalog := extensionAdminCatalog(baseDir); catalog != nil {
				return catalog
			}
		}
	}
	return extensionAdminCatalog(filepath.Join("plugins", plugin.Slug(p)))
}

func extensionAdminCatalog(baseDir string) *coreI18n.Catalog {
	if baseDir == "" {
		return nil
	}
	return extensionAdminCatalogFS(assets.DirFS(baseDir))
}

// extensionAdminCatalogFS loads locales/admin/*.json from an extension
// filesystem, falling back to locales/*.json.
func extensionAdminCatalogFS(fsys fs.FS) *coreI18n.Catalog {
	if fsys == nil {
		return nil
	}
	messages := coreI18n.LoadFlatMessages(fsys, "locales/admin")
	if len(messages) == 0 {
		messages = coreI18n.LoadFlatMessages(fsys, "locales")
	}
	if len(messages) == 0 {
		return nil
	}
	return coreI18n.NewCatalog(coreI18n.DefaultUILanguage, messages)
}

func extensionBaseFromSettingsTemplate(tmplPath string) string {
	if tmplPath == "" {
		return ""
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(tmplPath)))
}

// serveStaticFS serves name from fsys with the cache policy of
// serveStaticFile. Files without a modification time, such as embedded files,
// get a content-hash ETag so conditional requests still end in 304; files on
// disk keep Last-Modified as before.
func serveStaticFS(c *gin.Context, fsys fs.FS, name string, immutable bool) {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if fsys == nil || name == "" || !fs.ValidPath(name) {
		serveStaticNotFound(c)
		return
	}
	f, err := fsys.Open(name)
	if err != nil {
		serveStaticNotFound(c)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		serveStaticNotFound(c)
		return
	}
	content, ok := f.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(f)
		if err != nil {
			serveStaticNotFound(c)
			return
		}
		content = bytes.NewReader(data)
	}
	modTime := info.ModTime()
	if modTime.IsZero() {
		etag, err := contentETag(content)
		if err != nil {
			serveStaticNotFound(c)
			return
		}
		c.Header("ETag", etag)
	}
	setStaticCacheHeaders(c, immutable)
	http.ServeContent(c.Writer, c.Request, info.Name(), modTime, content)
}

// contentETag hashes the whole content and rewinds it.
func contentETag(content io.ReadSeeker) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, content); err != nil {
		return "", err
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return `"` + hex.EncodeToString(h.Sum(nil)[:16]) + `"`, nil
}

func setStaticCacheHeaders(c *gin.Context, immutable bool) {
	if immutable {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.Header("Expires", time.Now().Add(365*24*time.Hour).Format(time.RFC1123))
	} else {
		c.Header("Cache-Control", "public, max-age=86400")
		c.Header("Expires", time.Now().Add(24*time.Hour).Format(time.RFC1123))
	}
}
