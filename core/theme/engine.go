package theme

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go-press/pkg/logger"
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

// Load compiles all templates in the theme's template directory.
func (e *TemplateEngine) Load() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	dir := e.theme.TemplateDir()
	tmpl := template.New("").Funcs(e.funcMap)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".tmpl" && ext != ".html" {
			return nil
		}

		// Template name is relative path from template dir
		name, _ := filepath.Rel(dir, path)
		name = filepath.ToSlash(name) // normalize to forward slashes

		data, readErr := os.ReadFile(path)
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
	logger.Info("Templates loaded", "theme", e.theme.Name(), "dir", dir)
	return nil
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
