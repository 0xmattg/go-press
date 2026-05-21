package theme

import (
	"html/template"
	"net/url"
	"reflect"
	"strings"
	"time"

	"go-press/core/menu"

	"github.com/gin-gonic/gin"
)

// CommonFuncMap returns template functions shared across all themes.
// Themes can extend this with their own functions via TemplateFuncs().
func CommonFuncMap() template.FuncMap {
	return template.FuncMap{
		"formatDate": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("Jan 2, 2006")
		},
		"formatDateTime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02 15:04")
		},
		"truncate": func(s string, n int) string {
			runes := []rune(s)
			if len(runes) <= n {
				return s
			}
			return string(runes[:n]) + "..."
		},
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
		"settingOr": func(m map[string]string, key, def string) string {
			if m != nil {
				if v, ok := m[key]; ok && v != "" {
					return v
				}
			}
			return def
		},
		"responsiveImage": func(src, alt, className, sizes, loading string) template.HTML {
			return renderResponsiveImage(nil, src, alt, imageAttrs{
				Class:   className,
				Sizes:   sizes,
				Loading: loading,
			})
		},
		"responsiveImagePriority": func(src, alt, className, sizes string) template.HTML {
			return renderResponsiveImage(nil, src, alt, imageAttrs{
				Class:         className,
				Sizes:         sizes,
				Loading:       "eager",
				FetchPriority: "high",
			})
		},
		"responsiveImagePreload": func(src, sizes string) template.HTML {
			return renderResponsivePreload(nil, src, sizes)
		},
		"T": func(c *gin.Context, msgID string) string {
			return msgID
		},
		"currentLang": func(c *gin.Context) string {
			return "en"
		},
		"langPrefixURL": func(c *gin.Context, path string) string {
			return path
		},
		"isMenuURLActive": isMenuURLActive,
		"buildURL": func(contentType, slug string) string {
			if slug == "" {
				return "/"
			}
			if contentType == "" {
				return "/" + strings.TrimPrefix(slug, "/")
			}
			return "/" + strings.Trim(contentType, "/") + "/" + strings.TrimPrefix(slug, "/")
		},
		"archiveURL": func(contentType string) string {
			if contentType == "" {
				return "/"
			}
			return "/" + strings.Trim(contentType, "/")
		},
		"contentURL": func(item interface{}, fallbackType string) string {
			if url := stringField(item, "URL"); url != "" {
				return url
			}
			slug := stringField(item, "Slug")
			if slug == "" {
				return "/"
			}
			contentType := stringField(item, "Type")
			if contentType == "" {
				contentType = fallbackType
			}
			if contentType == "" {
				return "/" + strings.TrimPrefix(slug, "/")
			}
			return "/" + strings.Trim(contentType, "/") + "/" + strings.TrimPrefix(slug, "/")
		},
		"renderHook": func(name string, data interface{}) template.HTML {
			return ""
		},
		"seoHeadFor": func(data interface{}) template.HTML {
			return ""
		},
		"pageTitleFor": func(data interface{}, fallback string) string {
			if title := pageTitleFromData(data); title != "" {
				return title
			}
			return fallback
		},
		"menuByLocation": func(location string) *menu.Menu {
			return nil
		},
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i + 1
			}
			return s
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
				m := int(d.Minutes())
				return strings.TrimRight(strings.TrimRight(
					time.Duration(m).String(), "0"), ".") + " 分钟前"
			case d < 24*time.Hour:
				h := int(d.Hours())
				return strings.TrimRight(strings.TrimRight(
					time.Duration(h).String(), "0"), ".") + " 小时前"
			default:
				return t.Format("01-02 15:04")
			}
		},
		// isMenuActive is kept for compatibility with older themes that only
		// pass a semantic ActivePage value. New request-aware themes should use
		// isMenuURLActive so active state follows configured rewrite URLs.
		"isMenuActive": func(activePage, url string) bool {
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
		},
	}
}

func isMenuURLActive(c *gin.Context, menuURL string) bool {
	if c == nil || c.Request == nil || menuURL == "" {
		return false
	}

	current := comparableMenuPath(c.Request.URL.Path, "")
	target := comparableMenuPath(menuURL, c.Request.Host)
	if current == "" || target == "" {
		return false
	}
	if target == "/" {
		return current == "/"
	}
	return current == target || strings.HasPrefix(current, target+"/")
}

func comparableMenuPath(raw, currentHost string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err == nil {
		if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
			return ""
		}
		if parsed.Host != "" && currentHost != "" && !strings.EqualFold(parsed.Host, currentHost) {
			return ""
		}
		raw = parsed.Path
	}

	if raw == "" {
		raw = "/"
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}

	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		raw = "/"
	}
	return stripMenuLanguagePrefix(raw)
}

func stripMenuLanguagePrefix(path string) string {
	if path == "/" {
		return path
	}
	trimmed := strings.TrimPrefix(path, "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts[0]) != 2 || !isASCIILetters(parts[0]) {
		return path
	}
	if len(parts) == 1 || parts[1] == "" {
		return "/"
	}
	return "/" + strings.TrimRight(parts[1], "/")
}

func isASCIILetters(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

func pageTitleFromData(data interface{}) string {
	if data == nil {
		return ""
	}
	v := reflect.ValueOf(data)
	v = unwrapTemplateValue(v)
	if !v.IsValid() {
		return ""
	}

	var meta reflect.Value
	switch v.Kind() {
	case reflect.Map:
		meta = v.MapIndex(reflect.ValueOf("SEO"))
	case reflect.Struct:
		meta = v.FieldByName("SEO")
	default:
		return ""
	}
	return titleFromMetaValue(meta)
}

func titleFromMetaValue(v reflect.Value) string {
	v = unwrapTemplateValue(v)
	if !v.IsValid() {
		return ""
	}

	var title reflect.Value
	switch v.Kind() {
	case reflect.Map:
		title = v.MapIndex(reflect.ValueOf("Title"))
	case reflect.Struct:
		title = v.FieldByName("Title")
	default:
		return ""
	}
	title = unwrapTemplateValue(title)
	if !title.IsValid() || title.Kind() != reflect.String {
		return ""
	}
	return strings.TrimSpace(title.String())
}

func unwrapTemplateValue(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Ptr) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func stringField(item interface{}, name string) string {
	if item == nil {
		return ""
	}
	v := reflect.ValueOf(item)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Map:
		mv := v.MapIndex(reflect.ValueOf(name))
		if !mv.IsValid() && name == "URL" {
			mv = v.MapIndex(reflect.ValueOf("Url"))
		}
		if !mv.IsValid() {
			return ""
		}
		if mv.Kind() == reflect.Interface {
			mv = mv.Elem()
		}
		if mv.Kind() == reflect.String {
			return mv.String()
		}
	case reflect.Struct:
		fv := v.FieldByName(name)
		if !fv.IsValid() && name == "URL" {
			fv = v.FieldByName("Url")
		}
		if !fv.IsValid() || fv.Kind() != reflect.String {
			return ""
		}
		return fv.String()
	}
	return ""
}

// MergeFuncMap merges multiple FuncMaps, later maps override earlier ones.
func MergeFuncMap(maps ...template.FuncMap) template.FuncMap {
	result := make(template.FuncMap)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}
