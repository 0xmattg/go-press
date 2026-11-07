// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/assets"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// RenderNamespacedInActiveTheme renders an extension-owned page fragment inside
// the active theme's base layout, so the fragment shares the site header,
// footer, and styles. Namespace is the extension-controlled override directory
// below the theme's templates directory.
//
// Template resolution (first hit wins), mirroring WordPress-style theme
// overrides:
//
//	<theme templates>/<namespace>/<fragment>.tmpl — theme override
//	<extensionDefaultDir>/<fragment>.tmpl         — extension's bundled default
//
// The chosen fragment is composed with the theme's layouts/base.tmpl and
// partials/*.tmpl using the theme's own FuncMap, then "base" is executed with
// data (Ctx and Settings are injected when absent). Returns an error the caller
// can turn into a 404/500; it does not write an error page itself.
//
// Extensions that ship their files in an asset filesystem should call
// RenderNamespacedInActiveThemeFS instead.
func (e *Engine) RenderNamespacedInActiveTheme(c *gin.Context, namespace, fragment, extensionDefaultDir string, data gin.H) error {
	return e.RenderNamespacedInActiveThemeFS(c, namespace, fragment, assets.DirFS(extensionDefaultDir), data)
}

// RenderNamespacedInActiveThemeFS is RenderNamespacedInActiveTheme with the
// extension's default fragments read from defaults, a filesystem rooted at the
// directory that holds <fragment>.tmpl (for example a plugin's
// templates/<namespace> subtree). Theme overrides and the theme's layout and
// partials are read through the active theme's templates filesystem.
func (e *Engine) RenderNamespacedInActiveThemeFS(c *gin.Context, namespace, fragment string, defaults fs.FS, data gin.H) error {
	if !validThemeTemplateComponent(namespace) {
		return fmt.Errorf("render-in-theme: invalid namespace %q", namespace)
	}
	if !validThemeTemplateComponent(fragment) {
		return fmt.Errorf("render-in-theme: invalid fragment %q", fragment)
	}

	t := e.ActiveTheme()
	if t == nil {
		return fmt.Errorf("render-in-theme: no active theme")
	}
	themeFS := coreTheme.TemplatesFS(t)
	if themeFS == nil {
		return fmt.Errorf("render-in-theme: active theme has no templates")
	}
	const layout = "layouts/base.tmpl"
	if _, err := fs.Stat(themeFS, layout); err != nil {
		return fmt.Errorf("render-in-theme: active theme has no layouts/base.tmpl: %w", err)
	}

	// Fragment: theme override wins over the extension's bundled default.
	fragFS, fragName := themeFS, path.Join(namespace, fragment+".tmpl")
	if !assets.Exists(fragFS, fragName) {
		fragFS, fragName = defaults, fragment+".tmpl"
		if !assets.Exists(fragFS, fragName) {
			return fmt.Errorf("render-in-theme: fragment %q not found in theme override or extension defaults", fragment)
		}
	}

	// All partials (optional).
	partials, _ := assets.TemplateNames(themeFS, "partials")

	tmpl, err := assets.ParseFiles(template.New("").Funcs(t.TemplateFuncs()), themeFS, append([]string{layout}, partials...)...)
	if err == nil {
		tmpl, err = assets.ParseFiles(tmpl, fragFS, fragName)
	}
	if err != nil {
		return fmt.Errorf("render-in-theme: parse %q: %w", fragment, err)
	}

	if data == nil {
		data = gin.H{}
	}
	data["Ctx"] = c
	if _, ok := data["Settings"]; !ok && e.Options != nil {
		data["Settings"] = e.Options.All()
	}

	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	return tmpl.ExecuteTemplate(c.Writer, "base", data)
}

// RenderInActiveTheme is the compatibility entry point for extensions written
// before the namespace argument became explicit. It derives the namespace from
// the bundled template directory's final path component, then delegates to the
// namespaced renderer. New integrations should call
// RenderNamespacedInActiveTheme directly.
func (e *Engine) RenderInActiveTheme(c *gin.Context, fragment, extensionDefaultDir string, data gin.H) error {
	namespace := filepath.Base(filepath.Clean(extensionDefaultDir))
	return e.RenderNamespacedInActiveTheme(c, namespace, fragment, extensionDefaultDir, data)
}

// validThemeTemplateComponent confines theme overrides to one predictable
// directory and filename component. Both namespace and fragment are internal
// extension inputs, but rejecting traversal here keeps this shared rendering
// boundary safe even if a caller accidentally forwards request data.
func validThemeTemplateComponent(value string) bool {
	if value == "" || value == "." || value == ".." || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}
