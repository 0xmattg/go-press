// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Package filetheme runs theme packages: themes without Go code.
//
// A Theme is built from a *themepkg.Package, which only exists for files
// that passed the package check, and serves those files from memory. It is a
// BaseTheme with core's template functions only and the page bundle layout
// only: every page is templates/layouts/base.tmpl plus templates/partials/
// plus one templates/pages/ file, rendered by executing "base". Core
// registers the theme's content types, menu locations and settings from its
// theme.toml, like it does for Go themes.
package filetheme

import (
	"errors"
	"fmt"
	"html/template"

	"github.com/0xmattg/go-press/core/extension/themepkg"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// Theme is a theme loaded from a theme package.
type Theme struct {
	coreTheme.BaseTheme
	info coreTheme.PackageInfo
}

// New builds the theme for pkg. source is coreTheme.PackageSourcePreview or
// coreTheme.PackageSourceInstalled. Every page is parsed here, so a package
// whose templates do not parse with this version of core fails now rather
// than on a request.
func New(app coreTheme.App, pkg *themepkg.Package, source string) (*Theme, error) {
	if pkg == nil {
		return nil, errors.New("filetheme: no package")
	}
	id := pkg.ID()
	t := &Theme{info: coreTheme.PackageInfo{Slug: id.Slug, Version: id.Version, Digest: pkg.Digest(), Source: source}}
	// No theme directory: the package's files are the only files, even
	// outside release mode, where core otherwise prefers a directory on disk.
	t.InitBaseFS(app, "", pkg.FS(), "", nil)
	if t.FileConfig() == nil {
		return nil, fmt.Errorf("filetheme: %s: theme.toml cannot be read", id.Slug)
	}
	pages, err := coreTheme.LoadAllPageBundles(t)
	if err != nil {
		return nil, fmt.Errorf("filetheme: %s: %w", id.Slug, err)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("filetheme: %s: the package has no pages", id.Slug)
	}
	t.PageTemplates = pages
	// Pages the package has no template for render core's generic blocks in
	// its layout. A layout that cannot take them keeps the standalone
	// fallback; the package still loads.
	if fallbacks, err := coreTheme.LoadLayoutFallbacks(t); err == nil {
		t.LayoutFallbacks = fallbacks
	}
	return t, nil
}

// Setup does nothing: core registers everything a theme package declares
// from its theme.toml.
func (t *Theme) Setup(coreTheme.App) {}

// TemplateFuncs returns core's template functions; theme packages cannot
// add their own.
func (t *Theme) TemplateFuncs() template.FuncMap { return t.BaseFuncMap() }

// TemplateDir and StaticDir are empty: the theme's files are read through
// its asset filesystem.
func (t *Theme) TemplateDir() string { return "" }
func (t *Theme) StaticDir() string   { return "" }

// PackageInfo implements coreTheme.PackageInfoProvider.
func (t *Theme) PackageInfo() coreTheme.PackageInfo { return t.info }
