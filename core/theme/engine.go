// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/0xmattg/go-press/pkg/logger"
)

// TemplateEngine manages template loading and rendering.
type TemplateEngine struct {
	mu        sync.RWMutex
	theme     Theme
	templates *template.Template
	funcMap   template.FuncMap
}

// NewTemplateEngine creates a new TemplateEngine for the given theme.
func NewTemplateEngine(t Theme) *TemplateEngine {
	return &TemplateEngine{
		theme:   t,
		funcMap: t.TemplateFuncs(),
	}
}

// adminTemplateDir is the subdirectory of a theme's templates that holds
// back-office pages such as SettingsTemplateName. Those are rendered by the
// admin with the admin layout and its own template functions, so they are
// not part of the front-end template set.
const adminTemplateDir = "admin"

// Load compiles all templates in the theme's templates directory, read
// through TemplatesFS. Template names are slash-separated paths relative to
// that directory, for example "single-post.tmpl" or "partials/header.tmpl".
// The admin/ subdirectory is skipped: its templates call admin-only functions
// and define blocks such as "content" that must not leak into the front end.
func (e *TemplateEngine) Load() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	tplFS := TemplatesFS(e.theme)
	if tplFS == nil {
		return fmt.Errorf("theme %q has no template dir", e.theme.Name())
	}
	tmpl := template.New("").Funcs(e.funcMap)

	err := fs.WalkDir(tplFS, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name == adminTemplateDir {
				return fs.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(path.Ext(name))
		if ext != ".tmpl" && ext != ".html" {
			return nil
		}

		data, readErr := fs.ReadFile(tplFS, name)
		if readErr != nil {
			return readErr
		}
		_, parseErr := tmpl.New(name).Parse(string(data))
		if parseErr != nil {
			logger.Warn("Failed to parse template", "name", name, "error", parseErr)
			return nil // skip bad templates
		}
		return nil
	})
	if err != nil {
		return err
	}

	e.templates = tmpl
	logger.Info("Templates loaded", "theme", e.theme.Name(), "source", templateSource(e.theme))
	return nil
}

// templateSource describes where a theme's templates come from, for logs.
func templateSource(t Theme) string {
	if AssetFSOf(t) != nil {
		return "asset filesystem"
	}
	return t.TemplateDir()
}

// Get returns the compiled template set.
func (e *TemplateEngine) Get() *template.Template {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.templates
}

// Resolve finds the first available template from a hierarchy list.
func (e *TemplateEngine) Resolve(candidates []string) *template.Template {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.templates == nil {
		return nil
	}
	for _, name := range candidates {
		if t := e.templates.Lookup(name); t != nil {
			return t
		}
	}
	return nil
}

// ActiveTheme returns the current theme.
func (e *TemplateEngine) ActiveTheme() Theme {
	return e.theme
}
