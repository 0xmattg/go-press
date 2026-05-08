package i18n

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/language"
)

const DefaultUILanguage = "en"

// Catalog provides lightweight file-backed translations for system UI surfaces
// such as the admin panel and installer.
type Catalog struct {
	defaultLang string
	messages    map[string]map[string]string
	fallbacks   []string
}

// NewCatalog creates a catalog from preloaded flat locale messages.
func NewCatalog(defaultLang string, messages map[string]map[string]string) *Catalog {
	defaultLang = NormalizeLanguage(defaultLang, DefaultUILanguage)
	normalized := make(map[string]map[string]string, len(messages))
	for lang, entries := range messages {
		normalized[NormalizeLanguage(lang, defaultLang)] = entries
	}
	return &Catalog{
		defaultLang: defaultLang,
		messages:    normalized,
		fallbacks:   fallbackLanguages(defaultLang, normalized),
	}
}

// LoadFlatMessages loads JSON locale files from an fs.FS directory.
// Each locale file should be a flat {"key": "value"} object named by language,
// for example locales/en.json or locales/zh-CN.json.
func LoadFlatMessages(localeFS fs.FS, dir string) map[string]map[string]string {
	result := map[string]map[string]string{}
	entries, err := fs.ReadDir(localeFS, dir)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		lang := strings.TrimSuffix(entry.Name(), ".json")
		data, err := fs.ReadFile(localeFS, path.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var messages map[string]string
		if err := json.Unmarshal(data, &messages); err != nil {
			continue
		}
		result[NormalizeLanguage(lang, DefaultUILanguage)] = messages
	}
	return result
}

// LoadFlatMessagesDir loads JSON locale files from a real filesystem directory.
func LoadFlatMessagesDir(dir string) map[string]map[string]string {
	return LoadFlatMessages(os.DirFS(dir), ".")
}

func fallbackLanguages(defaultLang string, messages map[string]map[string]string) []string {
	langs := make([]string, 0, len(messages))
	for lang := range messages {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	result := make([]string, 0, len(langs))
	if _, ok := messages[defaultLang]; ok {
		result = append(result, defaultLang)
	}
	for _, lang := range langs {
		if lang != defaultLang {
			result = append(result, lang)
		}
	}
	return result
}

// NormalizeLanguage maps common UI language aliases to GoPress language codes.
func NormalizeLanguage(lang, fallback string) string {
	if normalized := canonicalLanguage(lang); normalized != "" {
		return normalized
	}
	if normalized := canonicalLanguage(fallback); normalized != "" {
		return normalized
	}
	return DefaultUILanguage
}

// NormalizeSupportedLanguage normalizes a language and constrains it to a
// supported language list.
func NormalizeSupportedLanguage(lang string, supported []string, fallback string) string {
	normalized := NormalizeLanguage(lang, fallback)
	for _, candidate := range supported {
		if normalized == NormalizeLanguage(candidate, fallback) {
			return normalized
		}
	}
	normalizedFallback := NormalizeLanguage(fallback, DefaultUILanguage)
	for _, candidate := range supported {
		if normalizedFallback == NormalizeLanguage(candidate, DefaultUILanguage) {
			return normalizedFallback
		}
	}
	if len(supported) > 0 {
		return NormalizeLanguage(supported[0], DefaultUILanguage)
	}
	return normalizedFallback
}

func canonicalLanguage(lang string) string {
	raw := strings.ReplaceAll(strings.TrimSpace(lang), "_", "-")
	switch strings.ToLower(raw) {
	case "zh", "zh-cn", "zh-hans":
		return "zh-CN"
	case "en", "en-us", "en-gb":
		return "en"
	case "":
		return ""
	}
	tag, err := language.Parse(raw)
	if err != nil {
		return raw
	}
	return tag.String()
}

// T returns a localized message with fallback to the catalog default language,
// then to the key itself.
func (c *Catalog) T(lang, key string, args ...interface{}) string {
	if c == nil {
		return FormatMessage(key, args...)
	}
	lang = NormalizeLanguage(lang, c.defaultLang)
	if msg := c.Message(lang, key); msg != "" {
		return FormatMessage(msg, args...)
	}
	return FormatMessage(key, args...)
}

// Message returns a raw localized message without formatting.
func (c *Catalog) Message(lang, key string) string {
	if c == nil {
		return ""
	}
	lang = NormalizeLanguage(lang, c.defaultLang)
	if messages, ok := c.messages[lang]; ok {
		if msg, ok := messages[key]; ok {
			return msg
		}
	}
	if lang != c.defaultLang {
		if messages, ok := c.messages[c.defaultLang]; ok {
			if msg, ok := messages[key]; ok {
				return msg
			}
		}
	}
	for _, fallback := range c.fallbacks {
		if fallback == lang || fallback == c.defaultLang {
			continue
		}
		if messages, ok := c.messages[fallback]; ok {
			if msg, ok := messages[key]; ok {
				return msg
			}
		}
	}
	return ""
}

// FormatMessage replaces simple printf-like placeholders without using
// fmt.Sprintf, so callers can pass dynamic locale keys without triggering vet.
func FormatMessage(msg string, args ...interface{}) string {
	out := msg
	for _, arg := range args {
		replacement := toString(arg)
		if strings.Contains(out, "%q") {
			out = strings.Replace(out, "%q", strconv.Quote(replacement), 1)
			continue
		}
		if strings.Contains(out, "%s") {
			out = strings.Replace(out, "%s", replacement, 1)
			continue
		}
		if strings.Contains(out, "%d") {
			out = strings.Replace(out, "%d", replacement, 1)
			continue
		}
		if strings.Contains(out, "%v") {
			out = strings.Replace(out, "%v", replacement, 1)
			continue
		}
	}
	return out
}

func toString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return ""
		}
		return strings.Trim(string(b), "\"")
	}
}
