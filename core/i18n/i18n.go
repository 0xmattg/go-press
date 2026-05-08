// Package i18n provides core-level internationalization for GoPress.
//
// This is a SYSTEM-LEVEL package - always available regardless of plugins.
// All theme developers use the same convention:
//
//  1. Provide locales/*.json in the theme directory (default translations)
//  2. Use {{T .Ctx "message_key"}} in templates
//
// The multilang plugin (optional) extends this with:
//   - DB override layer (admin can edit translations without touching files)
//   - Per-request language detection (URL prefix, cookie, Accept-Language)
//   - Multiple active languages
//
// Without multilang, the site uses config.Site.Language as the single language.
package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go-press/pkg/logger"

	"github.com/gin-gonic/gin"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

const (
	// CtxKeyLang is the gin.Context key for the current request language.
	CtxKeyLang = "current_lang"

	// CtxKeyLocalizer is the gin.Context key for the i18n localizer.
	CtxKeyLocalizer = "i18n_localizer"

	// OptMsgPrefix is the internal bundle key prefix for translatable options.
	// This avoids collision between locale-file message IDs and option keys.
	OptMsgPrefix = "_opt."
)

// Manager is the core i18n manager. One instance per Engine.
type Manager struct {
	mu          sync.RWMutex
	bundle      *goi18n.Bundle
	defaultLang string
}

// NewManager creates a new i18n Manager with the given default language.
func NewManager(defaultLang string) *Manager {
	if defaultLang == "" {
		defaultLang = "en"
	}
	m := &Manager{
		defaultLang: defaultLang,
		bundle:      goi18n.NewBundle(language.Make(defaultLang)),
	}
	m.bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	return m
}

// DefaultLang returns the configured default language code.
func (m *Manager) DefaultLang() string {
	return m.defaultLang
}

// Bundle returns the underlying go-i18n Bundle (for plugins to extend).
func (m *Manager) Bundle() *goi18n.Bundle {
	return m.bundle
}

// LoadThemeLocales loads all locale JSON files from a theme's locales/ directory.
// Expected format: locales/en.json, locales/zh.json, etc.
// Each file contains a flat map: {"key": "value", ...}
func (m *Manager) LoadThemeLocales(themeDir string) {
	localesDir := filepath.Join(themeDir, "locales")
	entries, err := os.ReadDir(localesDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		langCode := strings.TrimSuffix(name, ".json")
		filePath := filepath.Join(localesDir, name)

		data, err := os.ReadFile(filePath)
		if err != nil {
			logger.Warn("i18n: failed to read locale file", "file", filePath, "error", err)
			continue
		}

		var flat map[string]string
		if err := json.Unmarshal(data, &flat); err != nil {
			logger.Warn("i18n: failed to parse locale file", "file", filePath, "error", err)
			continue
		}

		msgs := make([]*goi18n.Message, 0, len(flat))
		for k, v := range flat {
			msgs = append(msgs, &goi18n.Message{ID: k, Other: v})
		}

		tag := language.Make(langCode)
		if err := m.bundle.AddMessages(tag, msgs...); err != nil {
			logger.Warn("i18n: failed to add messages", "lang", langCode, "error", err)
			continue
		}
		logger.Info("i18n: loaded theme locale", "lang", langCode, "keys", len(msgs))
	}
}

// AddMessages adds (or overrides) messages for a given language.
// Used by plugins to layer DB overrides on top of file defaults.
func (m *Manager) AddMessages(langCode string, msgs []*goi18n.Message) {
	if len(msgs) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	tag := language.Make(langCode)
	if err := m.bundle.AddMessages(tag, msgs...); err != nil {
		logger.Warn("i18n: failed to add override messages", "lang", langCode, "error", err)
	}
}

// Translate looks up a message for the current request language.
// Falls back to returning msgID if no translation is found.
func (m *Manager) Translate(c *gin.Context, msgID string) string {
	loc, exists := c.Get(CtxKeyLocalizer)
	if exists {
		localizer := loc.(*goi18n.Localizer)
		msg, err := localizer.Localize(&goi18n.LocalizeConfig{MessageID: msgID})
		if err == nil {
			return msg
		}
	}

	// Fallback: try default language localizer
	localizer := goi18n.NewLocalizer(m.bundle, m.defaultLang)
	msg, err := localizer.Localize(&goi18n.LocalizeConfig{MessageID: msgID})
	if err == nil {
		return msg
	}

	return msgID
}

// NewLocalizer creates a localizer for the given language code.
func (m *Manager) NewLocalizer(lang string) *goi18n.Localizer {
	return goi18n.NewLocalizer(m.bundle, lang, m.defaultLang)
}

// TranslateOption returns a translated value for a theme setting key.
// It checks the bundle for an _opt. prefixed override; falls back to defaultValue.
func (m *Manager) TranslateOption(c *gin.Context, key, defaultValue string) string {
	loc, exists := c.Get(CtxKeyLocalizer)
	if !exists {
		return defaultValue
	}
	localizer := loc.(*goi18n.Localizer)
	msg, err := localizer.Localize(&goi18n.LocalizeConfig{MessageID: OptMsgPrefix + key})
	if err == nil && msg != "" {
		return msg
	}
	return defaultValue
}

// TranslateSettings returns a copy of the settings map with translatable keys
// replaced by their translated values for the current request language.
//
// allKeys lists every registered translatable key so that keys absent from
// the settings map (never saved to the Options table) are still checked
// against the i18n bundle. This keeps the "fill missing keys" logic in Core
// rather than requiring each theme to handle it.
func (m *Manager) TranslateSettings(c *gin.Context, settings map[string]string, isTranslatable func(string) bool, allKeys []string) map[string]string {
	if c == nil {
		return settings
	}
	// Ensure every registered translatable key exists in the map so the bundle
	// lookup covers keys that were never saved to the Options table.
	for _, k := range allKeys {
		if _, exists := settings[k]; !exists {
			settings[k] = ""
		}
	}
	result := make(map[string]string, len(settings))
	for k, v := range settings {
		if isTranslatable(k) {
			result[k] = m.TranslateOption(c, k, v)
		} else {
			result[k] = v
		}
	}
	return result
}

// Middleware returns a gin middleware that sets the localizer in the request context.
// Without multilang plugin, it uses the default language for all requests.
// The multilang plugin replaces the localizer with a language-aware one.
func (m *Manager) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, exists := c.Get(CtxKeyLocalizer); !exists {
			localizer := goi18n.NewLocalizer(m.bundle, m.defaultLang)
			c.Set(CtxKeyLocalizer, localizer)
		}
		if _, exists := c.Get(CtxKeyLang); !exists {
			c.Set(CtxKeyLang, m.defaultLang)
		}
		c.Next()
	}
}
