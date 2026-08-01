package core

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
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
func (e *Engine) RenderNamespacedInActiveTheme(c *gin.Context, namespace, fragment, extensionDefaultDir string, data gin.H) error {
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
	tplDir := t.TemplateDir()
	layout := filepath.Join(tplDir, "layouts", "base.tmpl")
	if _, err := os.Stat(layout); err != nil {
		return fmt.Errorf("render-in-theme: active theme has no layouts/base.tmpl: %w", err)
	}

	// Fragment: theme override wins over the extension's bundled default.
	fragPath := filepath.Join(tplDir, namespace, fragment+".tmpl")
	if _, err := os.Stat(fragPath); err != nil {
		fragPath = filepath.Join(extensionDefaultDir, fragment+".tmpl")
		if _, err2 := os.Stat(fragPath); err2 != nil {
			return fmt.Errorf("render-in-theme: fragment %q not found in theme override or extension defaults", fragment)
		}
	}

	// All partials (optional).
	var partials []string
	if entries, err := os.ReadDir(filepath.Join(tplDir, "partials")); err == nil {
		for _, en := range entries {
			if !en.IsDir() && filepath.Ext(en.Name()) == ".tmpl" {
				partials = append(partials, filepath.Join(tplDir, "partials", en.Name()))
			}
		}
		sort.Strings(partials)
	}

	files := make([]string, 0, 2+len(partials))
	files = append(files, layout)
	files = append(files, partials...)
	files = append(files, fragPath)

	tmpl, err := template.New("").Funcs(t.TemplateFuncs()).ParseFiles(files...)
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
