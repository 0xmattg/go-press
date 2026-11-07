// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Page bundle loading: composes per-page templates from a shared layout +
// partials directory. This is the canonical way for themes to compile their
// page templates so that the funcmap (T, langPrefixURL, currentLang, etc.)
// stays uniform — the loader uses Theme.TemplateFuncs() (which is
// BaseTheme.BaseFuncMap()), so any helper added to core is immediately
// available to every theme without per-theme handler edits.
//
// Expected directory layout (under the theme's templates directory):
//
//	layouts/base.tmpl       — required, defines the {{define "base"}} skeleton
//	partials/*.tmpl         — optional, each defines a named block
//	pages/<page>.tmpl       — one per page name passed to LoadPageBundle
//
// Each page is parsed into its own *template.Template so that pages may
// re-define the same block name (e.g. "content") without colliding.

package theme

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/0xmattg/go-press/core/assets"
)

// LoadPageBundle composes a per-page template set for the given theme.
// Returns a map keyed by page name (without ".tmpl") whose values are
// fully-parsed templates ready to be executed via ExecuteTemplate("base", ...).
//
// pages is the list of page slugs to load (e.g. ["home", "about", "blog"]).
// A page without a template file is an error. Files are read through
// TemplatesFS, so themes initialized with InitBaseFS load from their asset
// filesystem and other themes from TemplateDir.
func LoadPageBundle(t Theme, pages []string) (map[string]*template.Template, error) {
	tplFS := TemplatesFS(t)
	if tplFS == nil {
		return nil, fmt.Errorf("theme %q has no template dir", t.Name())
	}

	const layoutPath = "layouts/base.tmpl"
	if _, err := fs.Stat(tplFS, layoutPath); err != nil {
		return nil, fmt.Errorf("layout missing for theme %q: %w", t.Name(), err)
	}

	// Collect all partials/*.tmpl deterministically.
	partials, err := assets.TemplateNames(tplFS, "partials")
	if err != nil {
		return nil, err
	}

	funcMap := t.TemplateFuncs()

	out := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		pagePath := path.Join("pages", page+".tmpl")
		if _, err := fs.Stat(tplFS, pagePath); err != nil {
			return nil, fmt.Errorf("page %q missing for theme %q: %w", page, t.Name(), err)
		}

		files := make([]string, 0, 2+len(partials))
		files = append(files, layoutPath)
		files = append(files, partials...)
		files = append(files, pagePath)

		tpl, err := assets.ParseFiles(template.New("").Funcs(funcMap), tplFS, files...)
		if err != nil {
			return nil, fmt.Errorf("parse page %q for theme %q: %w", page, t.Name(), err)
		}
		out[page] = tpl
	}

	return out, nil
}

// LoadAllPageBundles composes every templates/pages/*.tmpl file for a theme.
//
// It is used by BaseTheme's dynamic archive/single renderer, where page names
// are resolved from registered content type configuration instead of a
// theme-maintained hard-coded route list.
func LoadAllPageBundles(t Theme) (map[string]*template.Template, error) {
	tplFS := TemplatesFS(t)
	if tplFS == nil {
		return nil, fmt.Errorf("theme %q has no template dir", t.Name())
	}

	entries, err := fs.ReadDir(tplFS, "pages")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]*template.Template{}, nil
		}
		return nil, err
	}

	pages := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := path.Ext(e.Name())
		if ext != ".tmpl" && ext != ".html" {
			continue
		}
		pages = append(pages, strings.TrimSuffix(e.Name(), ext))
	}
	sort.Strings(pages)
	if len(pages) == 0 {
		return map[string]*template.Template{}, nil
	}

	return LoadPageBundle(t, pages)
}
