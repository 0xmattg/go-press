package monojournal

import (
	"html/template"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func themeFuncMap(location *time.Location) template.FuncMap {
	if location == nil {
		location = time.Local
	}
	return template.FuncMap{
		"journalDate": func(value interface{}) string {
			var date time.Time
			switch v := value.(type) {
			case time.Time:
				date = v
			case *time.Time:
				if v == nil {
					return ""
				}
				date = *v
			default:
				return ""
			}
			return date.In(location).Format("Jan 02, 2006")
		},
		"readingTime": readingTime,
		"plainText": func(value string) string {
			return strings.Join(strings.Fields(htmlTagPattern.ReplaceAllString(value, " ")), " ")
		},
		"journalPalette": journalPalette,
		"authorInitials": authorInitials,
		"profileRoleKey": func(role string) string {
			switch strings.TrimSpace(role) {
			case "super_admin", "editor", "author", "contributor", "subscriber":
				return "profile.role_" + strings.TrimSpace(role)
			default:
				return "profile.role_member"
			}
		},
		"currentYear": func() int { return time.Now().In(location).Year() },
		"journalPageURL": func(c *gin.Context, page int) string {
			if c == nil || c.Request == nil {
				return "/"
			}
			if page < 1 {
				page = 1
			}
			query := c.Request.URL.Query()
			query.Set("page", strconv.Itoa(page))
			return c.Request.URL.Path + "?" + query.Encode()
		},
		"previousPage": func(page int) int { return page - 1 },
		"nextPage":     func(page int) int { return page + 1 },
	}
}

func readingTime(value string) int {
	plain := strings.Join(strings.Fields(htmlTagPattern.ReplaceAllString(value, " ")), " ")
	words := len(strings.Fields(plain))
	if words == 0 {
		return 1
	}
	minutes := (words + 219) / 220
	cjkCount := 0
	for _, r := range plain {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			cjkCount++
		}
	}
	if cjkMinutes := (cjkCount + 499) / 500; cjkMinutes > minutes {
		minutes = cjkMinutes
	}
	if minutes < 1 {
		return 1
	}
	return minutes
}

func journalPalette(value string) string {
	switch strings.TrimSpace(value) {
	case "cobalt", "forest", "clay":
		return strings.TrimSpace(value)
	default:
		return "ink"
	}
}

func authorInitials(value string) string {
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return "MJ"
	}
	initials := make([]rune, 0, 2)
	for _, part := range parts {
		r, _ := utf8.DecodeRuneInString(part)
		if r != utf8.RuneError {
			initials = append(initials, r)
		}
		if len(initials) == 2 {
			break
		}
	}
	return strings.ToUpper(string(initials))
}
