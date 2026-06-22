package atelierslate

import (
	"html/template"
	"regexp"
	"strings"
	"time"
)

var reHTMLTags = regexp.MustCompile(`<[^>]*>`)
var reNonDigit = regexp.MustCompile(`\D+`)

type socialLink struct {
	Label    string
	URL      string
	Title    string
	External bool
	Icon     template.HTML
}

func atelierSocialLinks(settings map[string]string) []socialLink {
	if settings == nil {
		return nil
	}

	defs := []struct {
		key   string
		label string
		icon  template.HTML
	}{
		{"social_linkedin", "LinkedIn", socialIconLinkedIn},
		{"social_x", "X", socialIconX},
		{"social_facebook", "Facebook", socialIconFacebook},
		{"social_youtube", "YouTube", socialIconYouTube},
		{"social_wechat", "WeChat", socialIconWeChat},
		{"social_slack", "Slack", socialIconSlack},
		{"social_discord", "Discord", socialIconDiscord},
		{"social_github", "GitHub", socialIconGitHub},
	}

	links := make([]socialLink, 0, len(defs))
	for _, def := range defs {
		value := strings.TrimSpace(settings[def.key])
		if value == "" {
			continue
		}
		link := socialLink{
			Label: def.label,
			URL:   value,
			Icon:  def.icon,
		}
		if def.key == "social_wechat" && !isSocialURL(value) {
			link.URL = "#"
			link.Title = value
		} else {
			link.External = isExternalSocialURL(value)
		}
		links = append(links, link)
	}
	return links
}

func isSocialURL(value string) bool {
	return strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "/") ||
		strings.HasPrefix(value, "#")
}

func isExternalSocialURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

const (
	socialIconLinkedIn = template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4.98 3.5C4.98 4.88 3.86 6 2.5 6S0 4.88 0 3.5 1.12 1 2.5 1s2.48 1.12 2.48 2.5ZM.32 8.08h4.35V23H.32V8.08Zm7.18 0h4.17v2.04h.06c.58-1.1 2-2.26 4.12-2.26 4.4 0 5.22 2.9 5.22 6.67V23h-4.35v-7.51c0-1.79-.03-4.1-2.5-4.1-2.5 0-2.88 1.95-2.88 3.97V23H7.5V8.08Z"/></svg>`)
	socialIconX        = template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18.24 2.25h3.31l-7.23 8.26 8.5 11.24h-6.65l-5.21-6.82-5.97 6.82H1.68l7.73-8.84L1.25 2.25h6.83l4.71 6.23 5.45-6.23Zm-1.16 17.52h1.83L7.08 4.13H5.12l11.96 15.64Z"/></svg>`)
	socialIconFacebook = template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M24 12.07C24 5.4 18.63 0 12 0S0 5.4 0 12.07c0 6.02 4.39 11.01 10.13 11.93v-8.44H7.08v-3.49h3.05V9.41c0-3.02 1.79-4.69 4.53-4.69 1.31 0 2.69.24 2.69.24v2.97h-1.51c-1.49 0-1.96.93-1.96 1.88v2.26h3.33l-.53 3.49h-2.8V24C19.61 23.08 24 18.09 24 12.07Z"/></svg>`)
	socialIconYouTube  = template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M23.5 6.2a3.01 3.01 0 0 0-2.12-2.14C19.5 3.55 12 3.55 12 3.55s-7.5 0-9.38.51A3.01 3.01 0 0 0 .5 6.2C0 8.09 0 12.03 0 12.03s0 3.94.5 5.83a3.01 3.01 0 0 0 2.12 2.14c1.88.51 9.38.51 9.38.51s7.5 0 9.38-.51a3.01 3.01 0 0 0 2.12-2.14c.5-1.89.5-5.83.5-5.83s0-3.94-.5-5.83ZM9.55 15.6V8.46l6.27 3.57-6.27 3.57Z"/></svg>`)
	socialIconWeChat   = template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9.1 4.2c-4.2 0-7.6 2.82-7.6 6.3 0 1.98 1.1 3.73 2.82 4.89l-.71 2.15 2.5-1.25c.9.31 1.9.49 2.99.49h.35a5.72 5.72 0 0 1-.25-1.67c0-3.38 3.19-6.12 7.11-6.12h.28c-.82-2.76-3.84-4.79-7.49-4.79Zm-2.55 3.15c.59 0 1.06.43 1.06.97 0 .53-.47.96-1.06.96-.58 0-1.05-.43-1.05-.96 0-.54.47-.97 1.05-.97Zm5.1 0c.58 0 1.05.43 1.05.97 0 .53-.47.96-1.05.96s-1.06-.43-1.06-.96c0-.54.48-.97 1.06-.97Zm4.66 2.84c-3.39 0-6.14 2.2-6.14 4.92 0 2.72 2.75 4.92 6.14 4.92.86 0 1.69-.14 2.44-.4l2.02 1.01-.57-1.74c1.38-.91 2.25-2.27 2.25-3.79 0-2.72-2.75-4.92-6.14-4.92Zm-2.05 2.46c.47 0 .85.34.85.77 0 .42-.38.76-.85.76s-.85-.34-.85-.76c0-.43.38-.77.85-.77Zm4.1 0c.47 0 .85.34.85.77 0 .42-.38.76-.85.76s-.85-.34-.85-.76c0-.43.38-.77.85-.77Z"/></svg>`)
	socialIconSlack    = template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9.64 1.5a2.27 2.27 0 0 0 0 4.54h2.27V3.77A2.27 2.27 0 0 0 9.64 1.5Zm0 6.05H3.77a2.27 2.27 0 0 0 0 4.54h5.87V7.55Zm12.86 2.09a2.27 2.27 0 0 0-4.54 0v2.27h2.27a2.27 2.27 0 0 0 2.27-2.27Zm-6.05 0V3.77a2.27 2.27 0 0 0-4.54 0v5.87h4.54Zm-2.09 12.86a2.27 2.27 0 0 0 0-4.54h-2.27v2.27a2.27 2.27 0 0 0 2.27 2.27Zm0-6.05h5.87a2.27 2.27 0 0 0 0-4.54h-5.87v4.54ZM1.5 14.36a2.27 2.27 0 0 0 4.54 0v-2.27H3.77a2.27 2.27 0 0 0-2.27 2.27Zm6.05 0v5.87a2.27 2.27 0 0 0 4.54 0v-5.87H7.55Z"/></svg>`)
	socialIconDiscord  = template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20.32 4.37A19.78 19.78 0 0 0 15.36 2.8a13.57 13.57 0 0 0-.64 1.32 18.37 18.37 0 0 0-5.5 0c-.19-.45-.41-.89-.65-1.32a19.74 19.74 0 0 0-4.96 1.57C.48 9.02-.36 13.56.07 18.03a19.9 19.9 0 0 0 6.08 3.08c.49-.66.92-1.37 1.29-2.11a12.9 12.9 0 0 1-2.03-.98l.5-.39c3.92 1.82 8.18 1.82 12.05 0l.5.39c-.65.39-1.33.72-2.03.98.37.74.8 1.45 1.29 2.11a19.86 19.86 0 0 0 6.09-3.08c.5-5.17-.86-9.67-3.49-13.66ZM8.02 15.28c-1.18 0-2.14-1.08-2.14-2.41 0-1.34.94-2.42 2.14-2.42 1.19 0 2.16 1.09 2.14 2.42 0 1.33-.95 2.41-2.14 2.41Zm7.92 0c-1.18 0-2.14-1.08-2.14-2.41 0-1.34.95-2.42 2.14-2.42 1.2 0 2.16 1.09 2.14 2.42 0 1.33-.94 2.41-2.14 2.41Z"/></svg>`)
	socialIconGitHub   = template.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 .5a12 12 0 0 0-3.79 23.39c.6.11.82-.26.82-.58v-2.04c-3.34.73-4.04-1.41-4.04-1.41-.55-1.39-1.34-1.76-1.34-1.76-1.09-.75.08-.73.08-.73 1.21.08 1.85 1.24 1.85 1.24 1.07 1.84 2.82 1.31 3.51 1 .11-.78.42-1.31.76-1.61-2.67-.3-5.47-1.33-5.47-5.92 0-1.31.47-2.38 1.24-3.22-.12-.3-.54-1.52.12-3.18 0 0 1.01-.32 3.3 1.23a11.5 11.5 0 0 1 6.01 0c2.29-1.55 3.3-1.23 3.3-1.23.66 1.66.24 2.88.12 3.18.77.84 1.24 1.91 1.24 3.22 0 4.6-2.81 5.62-5.49 5.92.43.37.82 1.11.82 2.24v3.32c0 .32.22.7.83.58A12 12 0 0 0 12 .5Z"/></svg>`)
)

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
		"socialLinks": atelierSocialLinks,
	}
}
