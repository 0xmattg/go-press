package financialnews

import (
	"html/template"
	"strings"
	"time"
)

// DefaultFuncMap returns theme template helper functions.
func DefaultFuncMap() template.FuncMap {
	return template.FuncMap{
		"formatDate": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02 15:04")
		},
		"timeAgo": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			d := time.Since(*t)
			switch {
			case d < time.Minute:
				return "刚刚"
			case d < time.Hour:
				return strings.TrimRight(strings.TrimRight(
					time.Duration(d.Minutes()).String(), "0"), ".") + " 分钟前"
			case d < 24*time.Hour:
				return strings.TrimRight(strings.TrimRight(
					time.Duration(d.Hours()).String(), "0"), ".") + " 小时前"
			default:
				return t.Format("01-02 15:04")
			}
		},
		"truncate": func(s string, n int) string {
			runes := []rune(s)
			if len(runes) <= n {
				return s
			}
			return string(runes[:n]) + "..."
		},
		"lower": strings.ToLower,
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i + 1
			}
			return s
		},
	}
}
