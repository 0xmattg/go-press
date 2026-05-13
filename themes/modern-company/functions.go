package moderncompany

import (
	"html/template"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var reHTMLTags = regexp.MustCompile(`<[^>]*>`)
var reNonDigit = regexp.MustCompile(`\D+`)

// DefaultFuncMap is retained only for the NewWithDB() shortcut path.
// The full theme entrypoint (New) routes funcs through core BaseFuncMap +
// InitBase extras, so this map should stay narrowly scoped.
func DefaultFuncMap() template.FuncMap {
	return template.FuncMap{
		"formatDate": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("Jan 2, 2006")
		},
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "..."
		},
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"lower": strings.ToLower,
		"stripTags": func(s string) string {
			return strings.TrimSpace(reHTMLTags.ReplaceAllString(s, " "))
		},
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i + 1
			}
			return s
		},
		"settingOr": func(m map[string]string, key, def string) string {
			if m != nil {
				if v, ok := m[key]; ok && v != "" {
					return v
				}
			}
			return def
		},
		"settingIntBetween": settingIntBetween,
		"isMenuActive":      isMenuActive,
	}
}

// isMenuActive reports whether the active page matches a menu URL,
// accounting for an optional 2-letter language prefix (e.g. "/zh/blog").
func isMenuActive(activePage, url string) bool {
	if activePage == "" || url == "" {
		return false
	}
	url = strings.TrimRight(url, "/")
	if url == "" {
		return activePage == "home"
	}
	path := strings.TrimPrefix(url, "/")
	parts := strings.SplitN(path, "/", 3)
	seg := parts[0]
	if len(seg) == 2 && len(parts) > 1 {
		seg = parts[1]
	}
	if seg == "" || (len(parts[0]) == 2 && len(parts) == 1) {
		return activePage == "home"
	}
	return seg == activePage
}

func settingIntBetween(m map[string]string, key string, def, min, max int) int {
	if min > max {
		min, max = max, min
	}
	v := def
	if m != nil {
		if raw := strings.TrimSpace(m[key]); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				v = parsed
			}
		}
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
