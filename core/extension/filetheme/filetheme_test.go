// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package filetheme

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/extension/themepkg"
	"github.com/0xmattg/go-press/core/hook"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/mail"
	coreMedia "github.com/0xmattg/go-press/core/media"
	"github.com/0xmattg/go-press/core/menu"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/rewrite"
	"github.com/0xmattg/go-press/core/taxonomy"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

const sampleDir = "../themepkg/testdata/minimal"

// testApp has every engine service BaseFuncMap looks for, so the sample's
// templates parse.
type testApp struct{}

func (testApp) Database() *gorm.DB                 { return nil }
func (testApp) ContentRepo() *content.Repository   { return nil }
func (testApp) TaxonomyRepo() *taxonomy.Repository { return nil }
func (testApp) ContentRegistry() *content.Registry { return nil }
func (testApp) OptionsStore() *option.Store        { return nil }
func (testApp) RewriteEngine() *rewrite.Engine     { return &rewrite.Engine{} }
func (testApp) SEOBuilder() *rewrite.SEOBuilder    { return &rewrite.SEOBuilder{} }
func (testApp) MenuStore() *menu.Store             { return &menu.Store{} }
func (testApp) MediaRepo() *coreMedia.Repository   { return &coreMedia.Repository{} }
func (testApp) I18nManager() *coreI18n.Manager     { return &coreI18n.Manager{} }
func (testApp) HookBus() *hook.Bus                 { return &hook.Bus{} }
func (testApp) MailSender() mail.Sender            { return nil }
func (testApp) SiteLocation() *time.Location       { return time.UTC }

// preferDiskApp asks extensions to read their directory on disk, as the
// engine does outside release mode.
type preferDiskApp struct{ testApp }

func (preferDiskApp) PreferDiskAssets() bool { return true }

// samplePackage reads the sample package, with changes applied to a copy.
func samplePackage(t *testing.T, changes map[string]string) *themepkg.Package {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(sampleDir)); err != nil {
		t.Fatal(err)
	}
	for name, content := range changes {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pkg, report, err := themepkg.ReadDir(os.DirFS(dir), themepkg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if pkg == nil {
		t.Fatalf("sample package fails the check: %v", report.Problems)
	}
	return pkg
}

func TestNewBuildsThemeFromPackage(t *testing.T) {
	pkg := samplePackage(t, nil)
	th, err := New(testApp{}, pkg, coreTheme.PackageSourcePreview)
	if err != nil {
		t.Fatal(err)
	}
	if th.Name() != "Minimal Sample" || th.Version() != "1.0.0" {
		t.Fatalf("name %q version %q", th.Name(), th.Version())
	}
	info, ok := coreTheme.PackageInfoOf(th)
	if !ok || info.Slug != "minimal-sample" || info.Version != "1.0.0" || info.Digest != pkg.Digest() || info.Source != coreTheme.PackageSourcePreview {
		t.Fatalf("package info = %+v, %v", info, ok)
	}
	for _, page := range []string{"home", "single"} {
		if th.PageTemplates[page] == nil {
			t.Fatalf("page %q not loaded; have %v", page, th.PageTemplates)
		}
	}
	if th.Templates != nil {
		t.Fatal("theme packages must not load classic templates")
	}
	if th.TemplateDir() != "" || th.StaticDir() != "" || th.AssetFS() == nil {
		t.Fatal("files must come from the package only")
	}
	if _, ok := th.TemplateFuncs()["contentList"]; !ok {
		t.Fatal("core template functions missing")
	}
	if th.FileConfig() == nil || len(th.FileConfig().External) != 2 {
		t.Fatalf("theme.toml not parsed: %+v", th.FileConfig())
	}
}

func TestNewIgnoresThemeDirectoryOnDisk(t *testing.T) {
	// Outside release mode core prefers a theme's directory on disk. A
	// theme package has none, even when themes/<slug> happens to exist.
	wd := t.TempDir()
	disk := filepath.Join(wd, "themes", "minimal-sample", "static", "css")
	if err := os.MkdirAll(disk, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disk, "style.css"), []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg := samplePackage(t, nil)
	t.Chdir(wd)
	th, err := New(preferDiskApp{}, pkg, coreTheme.PackageSourceInstalled)
	if err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(coreTheme.StaticFS(th), "css/style.css")
	if err != nil || bytes.Equal(data, []byte("disk")) {
		t.Fatalf("style.css = %q, %v", data, err)
	}
}

func TestNewRejectsMissingPackage(t *testing.T) {
	if _, err := New(testApp{}, nil, coreTheme.PackageSourceInstalled); err == nil {
		t.Fatal("nil package accepted")
	}
}
