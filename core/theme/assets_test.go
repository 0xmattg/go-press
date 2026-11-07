// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"bytes"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

const fsTestThemeTOML = "[theme]\nname = \"fs-test\"\nversion = \"1.0.0\"\n"

type fsTestTheme struct {
	BaseTheme
}

func (t *fsTestTheme) Setup(App)                       {}
func (t *fsTestTheme) TemplateFuncs() template.FuncMap { return template.FuncMap{} }
func (t *fsTestTheme) TemplateDir() string             { return filepath.Join(t.ThemeDir, "templates") }
func (t *fsTestTheme) StaticDir() string               { return filepath.Join(t.ThemeDir, "static") }

// preferDiskApp is an App that asks for the disk directory, like the engine
// outside release mode.
type preferDiskApp struct{ App }

func (preferDiskApp) PreferDiskAssets() bool { return true }

func themeFiles(label string) fstest.MapFS {
	return fstest.MapFS{
		"templates/layouts/base.tmpl":    {Data: []byte(`{{define "base"}}[{{template "header" .}}|{{template "content" .}}]{{end}}`)},
		"templates/partials/header.tmpl": {Data: []byte(`{{define "header"}}header{{end}}`)},
		"templates/pages/home.tmpl":      {Data: []byte(`{{define "content"}}` + label + `{{end}}`)},
		"templates/single.tmpl":          {Data: []byte(`single`)},
		"static/logo.svg":                {Data: []byte(`<svg>` + label + `</svg>`)},
		"static/css/style.css":           {Data: []byte(`body{}`)},
	}
}

func writeThemeFiles(t *testing.T, dir string, files fstest.MapFS) {
	t.Helper()
	for name, file := range files {
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, file.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func renderHome(t *testing.T, th Theme) string {
	t.Helper()
	pages, err := LoadAllPageBundles(th)
	if err != nil {
		t.Fatalf("LoadAllPageBundles: %v", err)
	}
	home, ok := pages["home"]
	if !ok {
		t.Fatalf("home page missing, got %v", pages)
	}
	var out bytes.Buffer
	if err := home.ExecuteTemplate(&out, "base", nil); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestInitBaseFSReadsEveryThemeFileFromBuiltinFiles(t *testing.T) {
	// The theme directory does not exist: everything must come from builtin.
	th := &fsTestTheme{}
	th.InitBaseFS(nil, filepath.Join(t.TempDir(), "missing"), themeFiles("builtin"), fsTestThemeTOML, nil)

	if AssetFSOf(th) == nil {
		t.Fatal("InitBaseFS theme must expose its asset filesystem")
	}
	if got := renderHome(t, th); got != "[header|builtin]" {
		t.Fatalf("home = %q, want page bundle from built-in files", got)
	}
	engine := NewTemplateEngine(th)
	if err := engine.Load(); err != nil {
		t.Fatalf("TemplateEngine.Load: %v", err)
	}
	if engine.Resolve([]string{"single.tmpl"}) == nil || engine.Resolve([]string{"partials/header.tmpl"}) == nil {
		t.Fatal("hierarchy templates must load by slash-separated path from built-in files")
	}
	if got := th.LogoSVG(); got != "<svg>builtin</svg>" {
		t.Fatalf("LogoSVG = %q, want built-in logo", got)
	}
	if data, err := fs.ReadFile(StaticFS(th), "css/style.css"); err != nil || string(data) != "body{}" {
		t.Fatalf("StaticFS css = %q, %v", data, err)
	}
}

func TestInitBaseFSReadsThemeDirectoryWhenAppPrefersDisk(t *testing.T) {
	dir := t.TempDir()
	writeThemeFiles(t, dir, themeFiles("disk"))

	th := &fsTestTheme{}
	th.InitBaseFS(preferDiskApp{}, dir, themeFiles("builtin"), fsTestThemeTOML, nil)
	if got := renderHome(t, th); got != "[header|disk]" {
		t.Fatalf("home = %q, want the directory on disk outside release mode", got)
	}
	if got := th.LogoSVG(); got != "<svg>disk</svg>" {
		t.Fatalf("LogoSVG = %q, want disk logo", got)
	}

	release := &fsTestTheme{}
	release.InitBaseFS(nil, dir, themeFiles("builtin"), fsTestThemeTOML, nil)
	if got := renderHome(t, release); got != "[header|builtin]" {
		t.Fatalf("home = %q, want built-in files in release mode", got)
	}
}

func TestInitBaseKeepsReadingTheThemeDirectories(t *testing.T) {
	dir := t.TempDir()
	writeThemeFiles(t, dir, themeFiles("legacy"))

	th := &fsTestTheme{}
	th.InitBase(nil, dir, fsTestThemeTOML, nil)
	if AssetFSOf(th) != nil {
		t.Fatal("InitBase theme must not report an asset filesystem")
	}
	if got := renderHome(t, th); got != "[header|legacy]" {
		t.Fatalf("home = %q, want TemplateDir on disk", got)
	}
	if got := th.LogoSVG(); got != "<svg>legacy</svg>" {
		t.Fatalf("LogoSVG = %q, want ThemeDir logo", got)
	}
	if !fsExists(StaticFS(th), "css/style.css") {
		t.Fatal("StaticFS must read StaticDir for InitBase themes")
	}
}

func TestLoadPageBundleReportsMissingLayoutAndPage(t *testing.T) {
	files := themeFiles("builtin")
	delete(files, "templates/layouts/base.tmpl")
	th := &fsTestTheme{}
	th.InitBaseFS(nil, "", files, fsTestThemeTOML, nil)
	if _, err := LoadPageBundle(th, []string{"home"}); err == nil {
		t.Fatal("missing layout must fail")
	}

	th = &fsTestTheme{}
	th.InitBaseFS(nil, "", themeFiles("builtin"), fsTestThemeTOML, nil)
	if _, err := LoadPageBundle(th, []string{"about"}); err == nil {
		t.Fatal("missing page must fail")
	}
}

func fsExists(fsys fs.FS, name string) bool {
	if fsys == nil {
		return false
	}
	_, err := fs.Stat(fsys, name)
	return err == nil
}
