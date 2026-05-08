package core

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	poweredByOptionKey  = "powered_by_gopress"
	poweredByMarker     = `class="gopress-powered-by"`
	poweredByInlineHTML = `<span class="gopress-powered-by" style="display:inline;color:inherit;font:inherit;opacity:.9;"> · Site Powered by <a href="https://gopress.xyz" target="_blank" rel="noopener" style="color:inherit;text-decoration:underline;text-underline-offset:2px;">GoPress</a></span>`
	poweredByBlockHTML  = `<div class="gopress-powered-by" style="box-sizing:border-box;width:100%;padding:8px 14px;text-align:center;font:12px/1.4 -apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:rgba(100,116,139,.86);background:transparent;">Site Powered by <a href="https://gopress.xyz" target="_blank" rel="noopener" style="color:inherit;text-decoration:underline;text-underline-offset:2px;">GoPress</a></div>`
)

// poweredByMiddleware injects a small core-managed attribution line into
// frontend HTML responses. It runs before page cache so cached and uncached
// responses are handled consistently without any theme or plugin dependency.
func (e *Engine) poweredByMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !e.poweredByEnabled() || skipPoweredByPath(c.Request.URL.Path) || c.Request.Method == http.MethodHead {
			c.Next()
			return
		}

		original := c.Writer
		capture := newPoweredByCapture(original)
		c.Writer = capture
		c.Next()
		c.Writer = original

		status := capture.Status()
		body := capture.body.Bytes()
		contentType := capture.Header().Get("Content-Type")
		shouldInject := status == http.StatusOK &&
			strings.Contains(contentType, "text/html") &&
			len(body) > 0

		if shouldInject {
			body = injectPoweredBy(body)
			original.Header().Del("Content-Length")
		}

		original.WriteHeader(status)
		if len(body) > 0 {
			_, _ = original.Write(body)
		}
	}
}

func (e *Engine) poweredByEnabled() bool {
	if e == nil || e.Options == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(e.Options.Get(poweredByOptionKey))) {
	case "0", "false", "off", "no", "disabled":
		return false
	default:
		return true
	}
}

func skipPoweredByPath(path string) bool {
	return strings.HasPrefix(path, "/admin") ||
		strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/install") ||
		strings.HasPrefix(path, "/static/") ||
		strings.HasPrefix(path, "/swagger/") ||
		path == "/health" ||
		path == "/sitemap.xml" ||
		path == "/robots.txt"
}

func injectPoweredBy(body []byte) []byte {
	if bytes.Contains(body, []byte(poweredByMarker)) || bytes.Contains(body, []byte("Site Powered by GoPress")) {
		return body
	}
	if out, ok := injectPoweredByInline(body); ok {
		return out
	}
	if out, ok := injectPoweredByFooterBottom(body); ok {
		return out
	}
	lower := bytes.ToLower(body)
	if idx := bytes.LastIndex(lower, []byte("</body>")); idx >= 0 {
		out := make([]byte, 0, len(body)+len(poweredByBlockHTML))
		out = append(out, body[:idx]...)
		out = append(out, poweredByBlockHTML...)
		out = append(out, body[idx:]...)
		return out
	}
	return append(append([]byte{}, body...), poweredByBlockHTML...)
}

func injectPoweredByInline(body []byte) ([]byte, bool) {
	lower := bytes.ToLower(body)
	anchor := bytes.LastIndex(lower, []byte("all rights reserved"))
	if anchor < 0 {
		anchor = bytes.LastIndex(lower, []byte("&copy;"))
	}
	if anchor < 0 {
		anchor = bytes.LastIndex(body, []byte("©"))
	}
	if anchor < 0 {
		return nil, false
	}

	insertAt := nearestClosingTag(lower, anchor)
	if insertAt < 0 {
		return nil, false
	}

	out := make([]byte, 0, len(body)+len(poweredByInlineHTML))
	out = append(out, body[:insertAt]...)
	out = append(out, poweredByInlineHTML...)
	out = append(out, body[insertAt:]...)
	return out, true
}

func injectPoweredByFooterBottom(body []byte) ([]byte, bool) {
	lower := bytes.ToLower(body)
	anchor := bytes.LastIndex(lower, []byte("footer-bottom"))
	if anchor < 0 {
		return nil, false
	}
	openEndRel := bytes.IndexByte(lower[anchor:], '>')
	if openEndRel < 0 {
		return nil, false
	}
	openEnd := anchor + openEndRel + 1
	insertAt := nearestClosingTag(lower, openEnd)
	if insertAt < 0 {
		return nil, false
	}

	out := make([]byte, 0, len(body)+len(poweredByInlineHTML))
	out = append(out, body[:insertAt]...)
	out = append(out, poweredByInlineHTML...)
	out = append(out, body[insertAt:]...)
	return out, true
}

func nearestClosingTag(lower []byte, after int) int {
	candidates := [][]byte{
		[]byte("</p>"),
		[]byte("</small>"),
		[]byte("</span>"),
		[]byte("</div>"),
		[]byte("</li>"),
	}
	best := -1
	for _, tag := range candidates {
		if idx := bytes.Index(lower[after:], tag); idx >= 0 {
			pos := after + idx
			if best < 0 || pos < best {
				best = pos
			}
		}
	}
	return best
}

type poweredByCapture struct {
	gin.ResponseWriter
	body    bytes.Buffer
	status  int
	size    int
	written bool
}

func newPoweredByCapture(w gin.ResponseWriter) *poweredByCapture {
	return &poweredByCapture{
		ResponseWriter: w,
		status:         http.StatusOK,
		size:           -1,
	}
}

func (w *poweredByCapture) WriteHeader(code int) {
	if code > 0 && !w.written {
		w.status = code
	}
}

func (w *poweredByCapture) WriteHeaderNow() {
	if !w.written {
		w.written = true
		w.size = 0
	}
}

func (w *poweredByCapture) Write(data []byte) (int, error) {
	w.WriteHeaderNow()
	n, err := w.body.Write(data)
	w.size += n
	return n, err
}

func (w *poweredByCapture) WriteString(s string) (int, error) {
	w.WriteHeaderNow()
	n, err := w.body.WriteString(s)
	w.size += n
	return n, err
}

func (w *poweredByCapture) Status() int {
	return w.status
}

func (w *poweredByCapture) Size() int {
	return w.size
}

func (w *poweredByCapture) Written() bool {
	return w.written
}

func (w *poweredByCapture) Flush() {
	w.WriteHeaderNow()
}
