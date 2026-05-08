// Page bundle loading: composes per-page templates from a shared layout +
// partials directory. This is the canonical way for themes to compile their
// page templates so that the funcmap (T, langPrefixURL, currentLang, etc.)
// stays uniform — the loader uses Theme.TemplateFuncs() (which is
// BaseTheme.BaseFuncMap()), so any helper added to core is immediately
// available to every theme without per-theme handler edits.
//
// Expected directory layout (under TemplateDir()):
//
//	layouts/base.tmpl       — required, defines the {{define "base"}} skeleton
//	partials/*.tmpl         — optional, each defines a named block
//	pages/<page>.tmpl       — one per page name passed to LoadPageBundle
//
// Each page is parsed into its own *template.Template so that pages may
// re-define the same block name (e.g. "content") without colliding.

package theme

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
)

// LoadPageBundle composes a per-page template set for the given theme.
// Returns a map keyed by page name (without ".tmpl") whose values are
// fully-parsed templates ready to be executed via ExecuteTemplate("base", ...).
//
// pages is the list of page slugs to load (e.g. ["home", "about", "blog"]).
// Pages that do not exist on disk are skipped with a non-fatal error in
// the returned map (caller may inspect via missing pages).
func LoadPageBundle(t Theme, pages []string) (map[string]*template.Template, error) {
	tplDir := t.TemplateDir()
	if tplDir == "" {
		return nil, fmt.Errorf("theme %q has no template dir", t.Name())
	}

	layoutPath := filepath.Join(tplDir, "layouts", "base.tmpl")
	if _, err := os.Stat(layoutPath); err != nil {
		return nil, fmt.Errorf("layout missing for theme %q: %w", t.Name(), err)
	}

	// Collect all partials/*.tmpl deterministically.
	partialsDir := filepath.Join(tplDir, "partials")
	partials, err := globTemplates(partialsDir)
	if err != nil {
		return nil, err
	}

	funcMap := t.TemplateFuncs()

	out := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		pagePath := filepath.Join(tplDir, "pages", page+".tmpl")
		if _, err := os.Stat(pagePath); err != nil {
			return nil, fmt.Errorf("page %q missing for theme %q: %w", page, t.Name(), err)
		}

		files := make([]string, 0, 2+len(partials))
		files = append(files, layoutPath)
		files = append(files, partials...)
		files = append(files, pagePath)

		tpl, err := template.New("").Funcs(funcMap).ParseFiles(files...)
		if err != nil {
			return nil, fmt.Errorf("parse page %q for theme %q: %w", page, t.Name(), err)
		}
		out[page] = tpl
	}

	return out, nil
}

func globTemplates(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".tmpl" {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	return files, nil
}
