package moderncompany

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// TestThemeRunsFromEmbeddedFiles is the S3a pilot check for this theme: with
// no theme directory on disk, every page template, the hierarchy templates,
// the static files, the settings page and the demo seed still load.
func TestThemeRunsFromEmbeddedFiles(t *testing.T) {
	theme := NewWithDB(nil, filepath.Join(t.TempDir(), "no-theme-dir"))
	if err := theme.handler.LoadPageTemplates(theme); err != nil {
		t.Fatalf("page templates from embedded files: %v", err)
	}
	if len(theme.handler.templates) != len(pageNames) {
		t.Fatalf("loaded %d page templates, want %d", len(theme.handler.templates), len(pageNames))
	}
	theme.LoadTemplates(theme)
	if theme.Templates == nil || theme.Templates.Get() == nil || theme.Templates.Resolve([]string{"layouts/base.tmpl"}) == nil {
		t.Fatal("hierarchy templates must load from embedded files")
	}
	if len(theme.PageTemplates) == 0 {
		t.Fatal("page bundles for dynamic content must load from embedded files")
	}
	if !strings.Contains(theme.LogoSVG(), "<svg") {
		t.Fatal("logo must come from embedded static/logo.svg")
	}
	fsys := coreTheme.AssetFSOf(theme)
	for _, name := range []string{
		coreTheme.SettingsTemplateName,
		coreTheme.DemoSeedName,
		"static/css/style.css",
		"static/js/main.js",
		"locales/en.json",
		"locales/zh.json",
		"locales/admin/en.json",
		"locales/admin/zh-CN.json",
	} {
		if _, err := fs.Stat(fsys, name); err != nil {
			t.Fatalf("%s missing from the theme files: %v", name, err)
		}
	}
}

// TestEmbeddedFilesMatchThemeDirectory guards the go:embed list: every runtime
// file in the theme directory must be embedded. Dot files such as .DS_Store
// are deliberately left out by go:embed.
func TestEmbeddedFilesMatchThemeDirectory(t *testing.T) {
	for _, dir := range []string{"templates", "static", "locales", "demo"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			name := filepath.ToSlash(path)
			if strings.HasPrefix(d.Name(), "_") {
				t.Errorf("%s starts with _ and is not embedded without the all: prefix", name)
				return nil
			}
			if _, err := fs.Stat(themeFiles, name); err != nil {
				t.Errorf("%s is not embedded: %v", name, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The installer lists the theme from the theme.toml registered with it.
func TestRegisteredWithEmbeddedThemeTOML(t *testing.T) {
	for _, rt := range core.RegisteredThemes() {
		if rt.Slug == "modern-company" {
			if rt.Source.TOML != themeTOML {
				t.Fatal("modern-company must register its embedded theme.toml")
			}
			return
		}
	}
	t.Fatal("modern-company is not registered")
}
