package content

import (
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

var (
	richTextPolicyOnce sync.Once
	richTextPolicy     *bluemonday.Policy

	svgPolicyOnce sync.Once
	svgPolicy     *bluemonday.Policy
)

// SanitizeHTML removes executable or otherwise unsafe markup while preserving
// the rich-text structures supported by the editor, including tables.
func SanitizeHTML(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	richTextPolicyOnce.Do(func() {
		richTextPolicy = bluemonday.UGCPolicy()
	})
	return richTextPolicy.Sanitize(value)
}

// SanitizeSVG returns an inline-safe copy of an SVG document: all scripting
// elements and event-handler attributes are stripped, leaving only static
// drawing primitives. It is used before inlining theme/plugin logo assets into
// admin pages, so a (possibly third-party) extension cannot smuggle script into
// the admin origin through its logo.svg. Returns "" for blank input.
func SanitizeSVG(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	svgPolicyOnce.Do(func() {
		p := bluemonday.NewPolicy()
		p.AllowElements(
			"svg", "g", "path", "circle", "rect", "line", "polyline", "polygon",
			"ellipse", "defs", "title", "desc", "clipPath",
		)
		p.AllowAttrs(
			"viewBox", "preserveAspectRatio", "xmlns", "width", "height", "fill",
			"stroke", "stroke-width", "stroke-linecap", "stroke-linejoin",
			"stroke-miterlimit", "stroke-dasharray", "d", "cx", "cy", "r", "rx", "ry",
			"x", "y", "x1", "y1", "x2", "y2", "points", "transform", "fill-rule",
			"clip-rule", "fill-opacity", "stroke-opacity", "opacity", "clip-path",
			"id", "class", "role", "aria-hidden", "focusable",
		).Globally()
		svgPolicy = p
	})
	out := svgPolicy.Sanitize(value)
	// bluemonday lowercases attribute names, but SVG attribute names are
	// case-sensitive: a lowercased "viewbox" is ignored by the renderer and the
	// icon loses its coordinate system. Restore the camelCase attributes our
	// policy allows.
	out = strings.ReplaceAll(out, " viewbox=", " viewBox=")
	out = strings.ReplaceAll(out, " preserveaspectratio=", " preserveAspectRatio=")
	return out
}

func sanitizeContent(item *Content) {
	if item == nil {
		return
	}
	item.Content = SanitizeHTML(item.Content)
}
