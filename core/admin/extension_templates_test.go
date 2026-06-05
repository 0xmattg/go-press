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

	paths := append(globSettingsTemplates(t, filepath.Join("..", "..", "themes", "*", "templates", "admin", "theme_settings.tmpl")),
		globSettingsTemplates(t, filepath.Join("..", "..", "plugins", "*", "templates", "admin", "settings.tmpl"))...)
	if len(paths) == 0 {
		t.Fatal("no extension settings templates found")
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if _, err := template.New("").Funcs(h.funcMap).ParseFiles(layout, path); err != nil {
				t.Fatalf("parse extension settings template: %v", err)
			}
		})
	}
}

func globSettingsTemplates(t *testing.T, pattern string) []string {
	t.Helper()
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob extension settings templates %q: %v", pattern, err)
	}
	return paths
}
