package gopresslanding

import (
	"html/template"
	"regexp"
	"strings"
	"time"
)

var reHTMLTags = regexp.MustCompile(`<[^>]*>`)

// DefaultFuncMap returns template helper functions for the landing theme.
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
			return s[:n] + "…"
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
				s[i] = i
			}
			return s
		},
		"settingOr": func(m map[string]string, key, def string) string {
			if v, ok := m[key]; ok && v != "" {
				return v
			}
			return def
		},
	}
}
