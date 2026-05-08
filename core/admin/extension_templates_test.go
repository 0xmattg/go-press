package admin

import (
	"html/template"
	"path/filepath"
	"testing"

	"go-press/core/content"
	"go-press/core/user"
)

func TestExtensionSettingsTemplatesParse(t *testing.T) {
	h := NewHandler(
		&Service{rbac: user.NewRBAC()},
		content.NewRegistry(),
		filepath.Join("templates"),
	)
	layout := filepath.Join("templates", "layouts", "admin.tmpl")
	paths := []string{
		filepath.Join("..", "..", "themes", "atelier-slate", "templates", "admin", "theme_settings.tmpl"),
		filepath.Join("..", "..", "themes", "atelier-slate-gp", "templates", "admin", "theme_settings.tmpl"),
		filepath.Join("..", "..", "themes", "axis-form", "templates", "admin", "theme_settings.tmpl"),
		filepath.Join("..", "..", "themes", "civic-estate", "templates", "admin", "theme_settings.tmpl"),
		filepath.Join("..", "..", "themes", "florafi", "templates", "admin", "theme_settings.tmpl"),
		filepath.Join("..", "..", "themes", "go-press-landing", "templates", "admin", "theme_settings.tmpl"),
		filepath.Join("..", "..", "themes", "modern-company", "templates", "admin", "theme_settings.tmpl"),
		filepath.Join("..", "..", "themes", "terra-trail", "templates", "admin", "theme_settings.tmpl"),
		filepath.Join("..", "..", "plugins", "multilang", "templates", "admin", "settings.tmpl"),
		filepath.Join("..", "..", "plugins", "code-snippets", "templates", "admin", "settings.tmpl"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if _, err := template.New("").Funcs(h.funcMap).ParseFiles(layout, path); err != nil {
				t.Fatalf("parse extension settings template: %v", err)
			}
		})
	}
}
