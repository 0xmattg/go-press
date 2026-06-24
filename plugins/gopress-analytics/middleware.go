package gopressanalytics

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	visitorCookie = "gp_analytics_vid"
	sessionCookie = "gp_analytics_sid"
)

var excludedPrefixes = []string{
	"/admin", "/api", "/static", "/health", "/swagger", "/sitemap.xml",
	"/robots.txt", "/favicon.ico",
}

func (p *Plugin) analyticsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !p.active.Load() || !p.collectionEnabled() || shouldSkipRequest(c.Request) {
			c.Next()
			return
		}

		now := time.Now().In(p.location)
		visitorID, visitorCookieMissing := cookieOrRandom(c, visitorCookie)
		sessionID, _ := cookieOrRandom(c, sessionCookie)
		secure := requestIsHTTPS(c.Request)
		if visitorCookieMissing {
			setAnalyticsCookie(c, visitorCookie, visitorID, 365*24*60*60, secure)
		}
		// Refresh the 30-minute session on every page view.
		setAnalyticsCookie(c, sessionCookie, sessionID, 30*60, secure)

		started := time.Now()
		c.Next()
		if p.collector == nil || c.Writer.Status() < http.StatusOK || c.Writer.Status() >= http.StatusMultipleChoices {
			return
		}
		contentType := strings.ToLower(c.Writer.Header().Get("Content-Type"))
		if contentType != "" && !strings.Contains(contentType, "text/html") {
			return
		}

		ua := strings.TrimSpace(c.Request.UserAgent())
		if isBotUserAgent(ua) {
			return
		}
		device := parseDevice(ua)
		source, medium, campaign, referrerHost := attribution(c.Request)
		lang := strings.TrimSpace(c.GetString("current_lang"))
		if lang == "" && p.engine != nil && p.engine.Config != nil {
			lang = p.engine.Config.Site.Language
		}
		if lang == "" {
			lang = "und"
		}
		clientIP := normalizeIP(c.ClientIP())
		country := ""
		if p.geoIP != nil {
			country = p.geoIP.LookupCountry(clientIP)
		}

		visitorHash := p.hashValue("visitor|" + visitorID)
		if visitorCookieMissing && visitorID == "" {
			visitorHash = p.hashValue("fallback|" + now.Format("2006-01-02") + "|" + clientIP + "|" + ua)
		}
		normalizedPath := normalizePath(c.Request.URL.Path)
		event := Event{
			EventUUID:      randomToken(18),
			EventType:      "page_view",
			EventName:      "page_view",
			OccurredAt:     now.UTC(),
			Day:            dayStart(now),
			VisitorHash:    visitorHash,
			SessionHash:    p.hashValue("session|" + sessionID),
			NormalizedPath: normalizedPath,
			PathHash:       p.hashValue("path|" + normalizedPath),
			Language:       normalizeLanguage(lang),
			ReferrerHost:   referrerHost,
			Source:         source,
			Medium:         medium,
			Campaign:       campaign,
			IPAddress:      clientIP,
			IPHash:         p.hashValue("ip|" + clientIP),
			Country:        country,
			UserAgent:      ua,
			DeviceType:     device.Type,
			Platform:       device.Platform,
			DeviceVendor:   device.Vendor,
			DeviceModel:    device.Model,
			Browser:        device.Browser,
			OS:             device.OS,
			StatusCode:     c.Writer.Status(),
			DurationMS:     time.Since(started).Milliseconds(),
			Properties:     "{}",
		}
		p.collector.publish(event)
	}
}

func shouldSkipRequest(r *http.Request) bool {
	if r == nil || r.Method != http.MethodGet {
		return true
	}
	path := normalizePath(r.URL.Path)
	for _, prefix := range excludedPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func cookieOrRandom(c *gin.Context, name string) (string, bool) {
	value, err := c.Cookie(name)
	value = strings.TrimSpace(value)
	if err == nil && len(value) >= 16 && len(value) <= 128 {
		return value, false
	}
	return randomToken(24), true
}

func setAnalyticsCookie(c *gin.Context, name, value string, maxAge int, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func requestIsHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

func randomToken(size int) string {
	if size < 16 {
		size = 16
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}

func (p *Plugin) hashValue(value string) string {
	mac := hmac.New(sha256.New, p.hashKey)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizeIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	return ip.String()
}

func normalizePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	if len(value) > 1 {
		value = strings.TrimRight(value, "/")
	}
	if len(value) > 1024 {
		value = value[:1024]
	}
	return value
}

func normalizeLanguage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	if value == "" {
		return "und"
	}
	if len(value) > 16 {
		return value[:16]
	}
	return value
}

func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func attribution(r *http.Request) (source, medium, campaign, referrerHost string) {
	query := r.URL.Query()
	source = cleanDimension(query.Get("utm_source"), 120)
	medium = cleanDimension(query.Get("utm_medium"), 120)
	campaign = cleanDimension(query.Get("utm_campaign"), 255)
	if referer := strings.TrimSpace(r.Referer()); referer != "" {
		if parsed, err := url.Parse(referer); err == nil {
			referrerHost = strings.ToLower(parsed.Hostname())
		}
	}
	if source == "" {
		if referrerHost == "" {
			source, medium = "(direct)", "(none)"
		} else {
			source, medium = referrerHost, "referral"
		}
	}
	return source, medium, campaign, referrerHost
}

func cleanDimension(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		value = value[:max]
	}
	return value
}

func isBotUserAgent(ua string) bool {
	ua = strings.ToLower(ua)
	if ua == "" {
		return true
	}
	for _, marker := range []string{
		"bot", "spider", "crawler", "slurp", "bingpreview", "facebookexternalhit",
		"headlesschrome", "python-requests", "curl/", "wget/", "go-http-client",
	} {
		if strings.Contains(ua, marker) {
			return true
		}
	}
	return false
}

type deviceInfo struct {
	Type     string
	Platform string
	Vendor   string
	Model    string
	Browser  string
	OS       string
}

func parseDevice(ua string) deviceInfo {
	lower := strings.ToLower(ua)
	info := deviceInfo{Type: "desktop", Platform: "web", Browser: "Other", OS: "Other"}

	switch {
	case strings.Contains(lower, "ipad") || strings.Contains(lower, "tablet"):
		info.Type = "tablet"
	case strings.Contains(lower, "mobi") || strings.Contains(lower, "iphone") || strings.Contains(lower, "android"):
		info.Type = "mobile"
	}
	switch {
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") || strings.Contains(lower, "macintosh"):
		info.Vendor = "Apple"
	case strings.Contains(lower, "samsung"):
		info.Vendor = "Samsung"
	case strings.Contains(lower, "pixel"):
		info.Vendor = "Google"
	case strings.Contains(lower, "huawei"):
		info.Vendor = "Huawei"
	case strings.Contains(lower, "xiaomi") || strings.Contains(lower, " mi "):
		info.Vendor = "Xiaomi"
	}
	switch {
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad"):
		info.OS, info.Platform = "iOS", "iOS"
	case strings.Contains(lower, "android"):
		info.OS, info.Platform = "Android", "Android"
	case strings.Contains(lower, "windows"):
		info.OS, info.Platform = "Windows", "Windows"
	case strings.Contains(lower, "mac os") || strings.Contains(lower, "macintosh"):
		info.OS, info.Platform = "macOS", "macOS"
	case strings.Contains(lower, "linux"):
		info.OS, info.Platform = "Linux", "Linux"
	}
	switch {
	case strings.Contains(lower, "edg/"):
		info.Browser = "Edge"
	case strings.Contains(lower, "opr/") || strings.Contains(lower, "opera"):
		info.Browser = "Opera"
	case strings.Contains(lower, "chrome/") || strings.Contains(lower, "crios/"):
		info.Browser = "Chrome"
	case strings.Contains(lower, "firefox/") || strings.Contains(lower, "fxios/"):
		info.Browser = "Firefox"
	case strings.Contains(lower, "safari/"):
		info.Browser = "Safari"
	}
	return info
}
