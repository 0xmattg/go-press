// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package themepkg

import (
	"path"
	"regexp"
	"strings"

	"github.com/0xmattg/go-press/core/theme"

	"golang.org/x/net/html"
)

// locate maps an offset in scanned text to a package path and line.
type locate func(offset int) (string, int)

// fileLocate locates offsets in the text of one file.
func fileLocate(name, src string) locate {
	return func(offset int) (string, int) { return name, lineAt(src, offset) }
}

type markupMode int

const (
	markupHTML     markupMode = iota // a static .html file
	markupTemplate                   // template text
	markupSVG                        // a .svg file
)

// maxCommentDepth bounds how deep comments inside comments are scanned.
const maxCommentDepth = 4

// rawTextElements are the elements whose content the tokenizer would read
// as raw text. The scanner reads it as markup instead, so nothing can hide
// in it.
var rawTextElements = map[string]bool{
	"iframe": true, "noembed": true, "noframes": true, "noscript": true, "plaintext": true,
	"script": true, "style": true, "textarea": true, "title": true, "xmp": true,
}

// urlAttributes hold URLs. Their scheme must be an allowed one.
var urlAttributes = map[string]bool{
	"href": true, "xlink:href": true, "src": true, "srcset": true, "imagesrcset": true,
	"action": true, "formaction": true, "poster": true, "background": true, "cite": true,
	"data": true, "ping": true, "longdesc": true, "lowsrc": true, "dynsrc": true,
	"manifest": true, "codebase": true, "icon": true,
}

// navigationSchemes may appear in any URL attribute.
var navigationSchemes = map[string]bool{"http": true, "https": true, "mailto": true, "tel": true, "sms": true}

// checkStaticMarkup scans the package's static HTML, SVG and CSS files.
func (c *checker) checkStaticMarkup() {
	for i := range c.files {
		f := &c.files[i]
		if strings.HasPrefix(f.name, "templates/") {
			continue
		}
		src := string(f.data)
		switch strings.ToLower(path.Ext(f.name)) {
		case ".html":
			c.scanMarkup(src, 0, fileLocate(f.name, src), markupHTML, 0)
		case ".svg":
			c.scanMarkup(src, 0, fileLocate(f.name, src), markupSVG, 0)
		case ".css":
			c.scanCSS(src, fileLocate(f.name, src))
		}
	}
}

// scanMarkup applies the script and external resource rules to HTML, SVG or
// template text. base is the offset of src within the text loc locates.
func (c *checker) scanMarkup(src string, base int, loc locate, mode markupMode, depth int) {
	z := html.NewTokenizer(strings.NewReader(src))
	offset := 0
	inStyle := false
	for {
		tt := z.Next()
		start := offset
		offset += len(z.Raw())
		switch tt {
		case html.ErrorToken:
			return
		case html.CommentToken:
			data := z.Token().Data
			if mode == markupSVG && strings.HasPrefix(data, "?") && !strings.HasPrefix(strings.ToLower(data), "?xml ") {
				p, line := loc(base + start)
				c.problem(RuleScriptSVG, p, line, "XML processing instructions such as xml-stylesheet are not allowed in SVG")
			}
			// Whatever a comment hides is checked too: a template branch
			// could leave the comment unopened at run time.
			if depth < maxCommentDepth {
				c.scanMarkup(data, base+start+4, loc, mode, depth+1)
			}
		case html.DoctypeToken:
			data := strings.ToLower(z.Token().Data)
			if mode == markupSVG && (strings.Contains(data, "[") || strings.Contains(data, "entity")) {
				p, line := loc(base + start)
				c.problem(RuleScriptSVG, p, line, "DTD declarations are not allowed in SVG")
			}
		case html.TextToken:
			if inStyle {
				textStart := base + start
				c.scanCSS(string(z.Raw()), func(i int) (string, int) { return loc(textStart + i) })
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			c.checkTag(tok, mode, loc, base+start)
			if tt == html.StartTagToken {
				if rawTextElements[tok.Data] {
					z.NextIsNotRawText()
				}
				if tok.Data == "style" {
					inStyle = true
				}
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "style" {
				inStyle = false
			}
		}
	}
}

// checkTag applies the element and attribute rules to one start tag.
func (c *checker) checkTag(tok html.Token, mode markupMode, loc locate, offset int) {
	p, line := loc(offset)
	name := tok.Data
	switch name {
	case "script":
		typ := strings.ToLower(strings.TrimSpace(attrValue(tok, "type")))
		if typ != "application/ld+json" || hasAttr(tok, "src") {
			c.problem(RuleScriptTag, p, line, "<script> is not allowed; only inline <script type=\"application/ld+json\"> data is")
		}
	case "object", "embed", "applet":
		c.problem(RuleScriptEmbed, p, line, "<%s> is not allowed", name)
	case "base":
		c.problem(RuleScriptBase, p, line, "<base> is not allowed")
	case "meta":
		if strings.EqualFold(strings.TrimSpace(attrValue(tok, "http-equiv")), "refresh") {
			c.problem(RuleScriptRefresh, p, line, "<meta http-equiv=\"refresh\"> is not allowed")
		}
	case "foreignobject":
		if mode == markupSVG {
			c.problem(RuleScriptSVG, p, line, "<foreignObject> is not allowed in SVG")
		}
	}

	for _, attr := range tok.Attr {
		key := attr.Key
		if attr.Namespace != "" {
			key = attr.Namespace + ":" + key
		}
		switch {
		case len(key) > 2 && strings.HasPrefix(key, "on"):
			c.problem(RuleScriptHandler, p, line, "event handler attribute %s is not allowed", key)
			continue
		case key == "srcdoc":
			c.problem(RuleScriptEmbed, p, line, "the srcdoc attribute is not allowed")
			continue
		case key == "style":
			c.scanCSS(attr.Val, func(int) (string, int) { return p, line })
		}
		if urlAttributes[key] {
			c.checkURLAttribute(tok, key, attr.Val, p, line)
		} else if hasScriptScheme(attr.Val) {
			c.problem(RuleScriptURL, p, line, "attribute %s contains a script URL", key)
		}
	}
}

// checkURLAttribute checks the schemes in a URL attribute and the hosts of
// the resources it loads.
func (c *checker) checkURLAttribute(tok html.Token, key, value, p string, line int) {
	kind, loads := resourceKind(tok, key)
	for _, u := range attributeURLs(key, value) {
		scheme := urlScheme(u)
		switch {
		case scheme == "" || navigationSchemes[scheme]:
		case scheme == "data" && loads && kind == theme.ExternalImage:
		default:
			c.problem(RuleScriptURL, p, line, "URL scheme %q is not allowed in %s", scheme, key)
			continue
		}
		if loads {
			c.checkExternalURL(u, kind, p, line)
		}
	}
}

// resourceKind reports whether an attribute makes the browser load a
// resource and, if so, of what kind. The kind is "" when any declared kind
// will do and "script" when the resource is a script.
func resourceKind(tok html.Token, key string) (string, bool) {
	switch tok.Data {
	case "img":
		return theme.ExternalImage, key == "src" || key == "srcset"
	case "source":
		if key == "srcset" {
			return theme.ExternalImage, true
		}
		return theme.ExternalMedia, key == "src"
	case "video":
		if key == "poster" {
			return theme.ExternalImage, true
		}
		return theme.ExternalMedia, key == "src"
	case "audio", "track":
		return theme.ExternalMedia, key == "src"
	case "iframe", "frame":
		return theme.ExternalFrame, key == "src"
	case "input":
		return theme.ExternalImage, key == "src"
	case "image", "use", "feimage":
		return theme.ExternalImage, key == "href" || key == "xlink:href"
	case "link":
		if key == "imagesrcset" {
			return theme.ExternalImage, true
		}
		if key == "href" {
			return linkKind(tok)
		}
	}
	if key == "background" {
		return theme.ExternalImage, true
	}
	return "", false
}

// linkKind returns what a <link href> loads, from its rel and as.
func linkKind(tok html.Token) (string, bool) {
	for _, rel := range strings.Fields(strings.ToLower(attrValue(tok, "rel"))) {
		switch rel {
		case "stylesheet":
			return theme.ExternalStyle, true
		case "icon", "apple-touch-icon", "apple-touch-icon-precomposed", "mask-icon":
			return theme.ExternalImage, true
		case "modulepreload":
			return "script", true
		case "manifest":
			return "", true
		case "preload", "prefetch":
			switch strings.ToLower(strings.TrimSpace(attrValue(tok, "as"))) {
			case "style":
				return theme.ExternalStyle, true
			case "font":
				return theme.ExternalFont, true
			case "image":
				return theme.ExternalImage, true
			case "audio", "video", "track":
				return theme.ExternalMedia, true
			case "document":
				return theme.ExternalFrame, true
			case "script", "worker", "sharedworker", "serviceworker", "audioworklet", "paintworklet":
				return "script", true
			}
			return "", true
		}
	}
	return "", false
}

// attributeURLs splits an attribute value into the URLs it holds.
func attributeURLs(key, value string) []string {
	switch key {
	case "srcset", "imagesrcset":
		var out []string
		for _, candidate := range strings.Split(value, ",") {
			if fields := strings.Fields(candidate); len(fields) > 0 {
				out = append(out, fields[0])
			}
		}
		return out
	case "ping":
		return strings.Fields(value)
	}
	return []string{value}
}

// urlScheme returns the lowercase scheme of u the way a browser reads it, or
// "" for a relative URL.
func urlScheme(u string) string {
	u = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, strings.TrimFunc(u, func(r rune) bool { return r <= ' ' }))
	colon := strings.IndexByte(u, ':')
	if colon <= 0 {
		return ""
	}
	scheme := strings.ToLower(u[:colon])
	for i := 0; i < len(scheme); i++ {
		ch := scheme[i]
		if !(ch >= 'a' && ch <= 'z') && (i == 0 || !(ch >= '0' && ch <= '9' || ch == '+' || ch == '-' || ch == '.')) {
			return ""
		}
	}
	return scheme
}

// hasScriptScheme reports whether value contains a javascript: or vbscript:
// URL, ignoring whitespace and control characters a browser would skip.
func hasScriptScheme(value string) bool {
	squeezed := strings.ToLower(strings.Map(func(r rune) rune {
		if r <= ' ' {
			return -1
		}
		return r
	}, value))
	return strings.Contains(squeezed, "javascript:") || strings.Contains(squeezed, "vbscript:")
}

// checkExternalURL checks that an absolute URL loading a resource of kind
// points at a declared host.
func (c *checker) checkExternalURL(u, kind, p string, line int) {
	host, secure, ok := externalHost(u)
	if !ok {
		return
	}
	if kind == "script" {
		c.problem(RuleScriptURL, p, line, "loading scripts from %s is not allowed", host)
		return
	}
	if !secure {
		c.warn(RuleExternalInsecure, p, line, "%s is loaded over plain http; browsers block it on HTTPS sites", host)
	}
	if c.manifest == nil {
		return
	}
	entries := c.manifest.ExternalHosts(host)
	if len(entries) == 0 {
		c.problem(RuleExternalUndeclared, p, line, "loads %s from %s, which [[external]] does not declare", kindLabel(kind), host)
		return
	}
	if kind == "" {
		return
	}
	for _, entry := range entries {
		if entry.Allows(kind) {
			return
		}
	}
	c.problem(RuleExternalKind, p, line, "loads %s from %s, but [[external]] does not list kind %q for it", kindLabel(kind), host, kind)
}

func kindLabel(kind string) string {
	switch kind {
	case theme.ExternalStyle:
		return "a stylesheet"
	case theme.ExternalFont:
		return "a font"
	case theme.ExternalImage:
		return "an image"
	case theme.ExternalMedia:
		return "a media file"
	case theme.ExternalFrame:
		return "a frame"
	}
	return "a resource"
}

// externalHost returns the host of an absolute http(s) or scheme-relative
// URL and whether it is https. ok is false for other URLs and for URLs
// whose host a template action supplies.
func externalHost(u string) (host string, secure, ok bool) {
	u = strings.TrimFunc(u, func(r rune) bool { return r <= ' ' })
	lower := strings.ToLower(u)
	var rest string
	switch {
	case strings.HasPrefix(lower, "https://"):
		rest, secure = u[len("https://"):], true
	case strings.HasPrefix(lower, "http://"):
		rest = u[len("http://"):]
	case strings.HasPrefix(lower, "//"):
		rest, secure = u[2:], true
	default:
		return "", false, false
	}
	if end := strings.IndexAny(rest, "/?#\\"); end >= 0 {
		rest = rest[:end]
	}
	if at := strings.LastIndexByte(rest, '@'); at >= 0 {
		rest = rest[at+1:]
	}
	if colon := strings.LastIndexByte(rest, ':'); colon >= 0 && !strings.Contains(rest[colon:], "]") {
		rest = rest[:colon]
	}
	host = strings.TrimSuffix(strings.ToLower(rest), ".")
	if host == "" {
		return "", false, false
	}
	return host, secure, true
}

func attrValue(tok html.Token, key string) string {
	for _, attr := range tok.Attr {
		if attr.Namespace == "" && attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func hasAttr(tok html.Token, key string) bool {
	for _, attr := range tok.Attr {
		if attr.Namespace == "" && attr.Key == key {
			return true
		}
	}
	return false
}

// CSS rules.
var (
	cssComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssEscape   = regexp.MustCompile(`\\([0-9a-fA-F]{1,6})[ \t\r\n\f]?|\\(.)`)
	cssBanned   = regexp.MustCompile(`expression\s*\(|(?:^|[^a-z0-9_-])behavior\s*:|-moz-binding`)
	cssImport   = regexp.MustCompile(`@import\s+(?:url\(\s*)?(?:"([^"]*)"|'([^']*)'|([^\s;)"']+))`)
	cssFontFace = regexp.MustCompile(`@font-face\s*\{`)
	cssURL      = regexp.MustCompile(`url\(\s*(?:"([^"]*)"|'([^']*)'|([^)\s"']*))\s*\)`)
)

// scanCSS applies the CSS script rules and checks the hosts of the
// resources a stylesheet loads.
func (c *checker) scanCSS(css string, loc locate) {
	// Blank out comments, keeping offsets and line breaks.
	css = cssComment.ReplaceAllStringFunc(css, func(comment string) string {
		return strings.Map(func(r rune) rune {
			if r == '\n' {
				return r
			}
			return ' '
		}, comment)
	})
	if strings.Contains(css, "\\") {
		css = unescapeCSS(css)
	}
	lower := strings.ToLower(css)

	for _, m := range cssBanned.FindAllStringIndex(lower, -1) {
		p, line := loc(m[0])
		found := strings.TrimLeftFunc(strings.Join(strings.Fields(lower[m[0]:m[1]]), ""), func(r rune) bool {
			return (r < 'a' || r > 'z') && r != '-'
		})
		c.problem(RuleScriptCSS, p, line, "%q is not allowed in CSS", found)
	}

	var imports [][2]int
	for _, m := range cssImport.FindAllStringSubmatchIndex(css, -1) {
		imports = append(imports, [2]int{m[0], m[1]})
		p, line := loc(m[0])
		c.checkCSSURL(firstGroup(css, m), theme.ExternalStyle, p, line)
	}
	var fontFaces [][2]int
	for _, m := range cssFontFace.FindAllStringIndex(css, -1) {
		end := strings.IndexByte(css[m[1]:], '}')
		if end < 0 {
			end = len(css) - m[1]
		}
		fontFaces = append(fontFaces, [2]int{m[0], m[1] + end})
	}
	for _, m := range cssURL.FindAllStringSubmatchIndex(css, -1) {
		if within(m[0], imports) {
			continue
		}
		kind := theme.ExternalImage
		if within(m[0], fontFaces) {
			kind = theme.ExternalFont
		}
		p, line := loc(m[0])
		c.checkCSSURL(firstGroup(css, m), kind, p, line)
	}
}

// checkCSSURL checks a URL a stylesheet loads: its scheme, and its host when
// it is absolute.
func (c *checker) checkCSSURL(u, kind, p string, line int) {
	switch scheme := urlScheme(u); scheme {
	case "", "http", "https", "data":
		c.checkExternalURL(u, kind, p, line)
	default:
		c.problem(RuleScriptCSS, p, line, "URL scheme %q is not allowed in CSS", scheme)
	}
}

// firstGroup returns the first matched capture group of a submatch index.
func firstGroup(s string, m []int) string {
	for i := 2; i+1 < len(m); i += 2 {
		if m[i] >= 0 {
			return s[m[i]:m[i+1]]
		}
	}
	return ""
}

func within(offset int, ranges [][2]int) bool {
	for _, r := range ranges {
		if offset >= r[0] && offset < r[1] {
			return true
		}
	}
	return false
}

// unescapeCSS decodes CSS escapes such as \65 and \:.
func unescapeCSS(css string) string {
	return cssEscape.ReplaceAllStringFunc(css, func(esc string) string {
		m := cssEscape.FindStringSubmatch(esc)
		if m[1] != "" {
			var r rune
			for _, h := range m[1] {
				r = r*16 + rune(hexValue(byte(h)))
			}
			if r == 0 || r > 0x10ffff {
				return "�"
			}
			return string(r)
		}
		if m[2] == "\n" {
			return ""
		}
		return m[2]
	})
}

func hexValue(ch byte) int {
	switch {
	case ch >= '0' && ch <= '9':
		return int(ch - '0')
	case ch >= 'a' && ch <= 'f':
		return int(ch-'a') + 10
	default:
		return int(ch-'A') + 10
	}
}
