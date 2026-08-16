package communa

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var reHTMLTags = regexp.MustCompile(`<[^>]*>`)

// stripHTMLTags removes HTML tags from a string and collapses whitespace.
func stripHTMLTags(s string) string {
	return strings.TrimSpace(reHTMLTags.ReplaceAllString(s, " "))
}

// compactExcerpt returns a plain-text summary, preferring the explicit excerpt
// and falling back to the stripped body, trimmed to max runes on a word edge.
func compactExcerpt(excerpt, body string, max int) string {
	text := strings.TrimSpace(excerpt)
	if text == "" {
		text = stripHTMLTags(body)
	}
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if max <= 0 || len(runes) <= max {
		return text
	}
	return strings.TrimSpace(string(runes[:max])) + "…"
}

// settingIntBetween reads an integer option, clamping it to [min, max] and
// falling back to def when the value is missing or unparsable.
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

// atoiOr parses a positive integer, returning def on any failure. Used to read
// count meta fields such as members_count / replies_count for display math.
func atoiOr(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	// Keep only the leading digit run so values like "1.2k" degrade gracefully.
	digits := strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
	if digits == "" {
		return def
	}
	if n, err := strconv.Atoi(digits); err == nil {
		return n
	}
	return def
}

// initials returns up to two uppercase initials for a display name, used as an
// avatar fallback when no image is available.
func initials(name string) string {
	fields := strings.Fields(strings.TrimSpace(name))
	if len(fields) == 0 {
		return "?"
	}
	first := []rune(fields[0])
	if len(fields) == 1 {
		if len(first) == 1 {
			return strings.ToUpper(string(first[0]))
		}
		return strings.ToUpper(string(first[:1]))
	}
	last := []rune(fields[len(fields)-1])
	return strings.ToUpper(string(first[0]) + string(last[0]))
}
