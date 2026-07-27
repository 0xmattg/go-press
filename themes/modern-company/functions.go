package moderncompany

import (
	"regexp"
	"strconv"
	"strings"
)

var reHTMLTags = regexp.MustCompile(`<[^>]*>`)
var reNonDigit = regexp.MustCompile(`\D+`)

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
