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
	}
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
