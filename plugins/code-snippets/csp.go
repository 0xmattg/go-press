// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package codesnippets

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/0xmattg/go-press/core"
)

// While a theme package is active, front-end pages carry a
// Content-Security-Policy that runs no script it does not know. The snippets
// are the administrator's own code, so the plugin declares them through the
// frontend.csp filter, one by one rather than allowing inline scripts in
// general:
//
//   - <script src> adds its host to script-src;
//   - an inline <script> adds the SHA-256 hash of its text;
//   - when any snippet runs a script, connect-src gets https:, since
//     analytics and widgets send data to hosts the plugin cannot know.
//
// Inline event handler attributes (onclick=...) and code that needs eval do
// not run under the policy, nor do scripts that a snippet's script loads from
// other hosts. Themes built into GoPress have no policy, so none of this
// applies there.

// cspDeclaration is what the current snippets need.
type cspDeclaration struct {
	script  []string
	connect []string
}

// cspCache remembers the declaration for the last snippets seen.
type cspCache struct {
	mu    sync.Mutex
	ready bool
	key   string
	decl  cspDeclaration
}

// addCSPSources implements the frontend.csp filter.
func (p *Plugin) addCSPSources(value interface{}, args ...interface{}) interface{} {
	sources, ok := value.(core.CSPSources)
	if !ok || p.engine == nil || p.engine.Options == nil || !p.engine.PluginManager.IsActive(PluginName) {
		return value
	}
	snippets := []string{
		p.engine.Options.Get(optHeadSnippet),
		p.engine.Options.Get(optBodyOpenSnippet),
		p.engine.Options.Get(optFooterSnippet),
	}
	decl := p.csp.declaration(strings.Join(snippets, "\x00"))
	sources.Script = append(sources.Script, decl.script...)
	sources.Connect = append(sources.Connect, decl.connect...)
	return sources
}

func (c *cspCache) declaration(snippets string) cspDeclaration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ready || c.key != snippets {
		c.ready, c.key = true, snippets
		c.decl = snippetCSP(strings.Split(snippets, "\x00")...)
	}
	return c.decl
}

// snippetCSP returns the sources the snippets' scripts need.
func snippetCSP(snippets ...string) cspDeclaration {
	var decl cspDeclaration
	seen := map[string]bool{}
	add := func(list *[]string, src string) {
		if src != "" && !seen[src] {
			seen[src] = true
			*list = append(*list, src)
		}
	}
	runs := false
	for _, snippet := range snippets {
		if strings.TrimSpace(snippet) == "" {
			continue
		}
		z := html.NewTokenizer(strings.NewReader(snippet))
		for {
			tt := z.Next()
			if tt == html.ErrorToken {
				break
			}
			if tt != html.StartTagToken {
				continue
			}
			name, hasAttr := z.TagName()
			if atom.Lookup(name) != atom.Script {
				continue
			}
			var src, typ string
			for hasAttr {
				var key, val []byte
				key, val, hasAttr = z.TagAttr()
				switch string(key) {
				case "src":
					src = string(val)
				case "type":
					typ = string(val)
				}
			}
			if !executableScriptType(typ) {
				continue
			}
			runs = true
			if src != "" {
				add(&decl.script, scriptSourceHost(src))
				continue
			}
			// The script's text follows as one raw-text token.
			if z.Next() == html.TextToken {
				add(&decl.script, scriptHash(string(z.Text())))
			} else {
				add(&decl.script, scriptHash(""))
			}
		}
	}
	if runs {
		decl.connect = []string{"https:"}
	}
	return decl
}

// executableScriptType reports whether a <script type> runs as script. Data
// blocks such as application/ld+json do not, and need no source.
func executableScriptType(typ string) bool {
	typ = strings.ToLower(strings.TrimSpace(typ))
	if i := strings.Index(typ, ";"); i >= 0 {
		typ = strings.TrimSpace(typ[:i])
	}
	switch typ {
	case "", "module", "text/javascript", "application/javascript", "application/ecmascript",
		"text/ecmascript", "application/x-javascript", "application/x-ecmascript", "text/jscript":
		return true
	}
	return false
}

// scriptSourceHost returns the CSP host source of a script URL: its scheme
// and host ("https://www.googletagmanager.com"). Same-origin and relative
// URLs need nothing, as 'self' covers them.
func scriptSourceHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	return u.Scheme + "://" + strings.ToLower(u.Host)
}

// scriptHash returns the CSP hash source of an inline script. Browsers hash
// the text after HTML parsing, which turns CRLF and CR line breaks into LF;
// textareas submit CRLF, so the stored snippet is normalized the same way.
func scriptHash(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	sum := sha256.Sum256([]byte(text))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}
