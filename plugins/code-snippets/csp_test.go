// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package codesnippets

import (
	"crypto/sha256"
	"encoding/base64"
	"reflect"
	"testing"

	"github.com/0xmattg/go-press/core"
)

func hashOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func TestSnippetCSPDeclaresScripts(t *testing.T) {
	// The Google tag as pasted into the settings textarea, which submits
	// CRLF line breaks; browsers hash the text with LF.
	ga := "<!-- Google tag -->\r\n<script async src=\"https://www.googletagmanager.com/gtag/js?id=G-TEST\"></script>\r\n" +
		"<script>\r\n  window.dataLayer = window.dataLayer || [];\r\n  gtag('config', 'G-TEST');\r\n</script>\r\n"
	inline := "\n  window.dataLayer = window.dataLayer || [];\n  gtag('config', 'G-TEST');\n"
	got := snippetCSP(ga, "", `<noscript><iframe src="https://www.googletagmanager.com/ns.html"></iframe></noscript>`)
	want := cspDeclaration{
		script:  []string{"https://www.googletagmanager.com", hashOf(inline)},
		connect: []string{"https:"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("declaration = %+v, want %+v", got, want)
	}

	cases := map[string]cspDeclaration{
		// Data blocks and other non-script types run nothing.
		`<script type="application/ld+json">{"@type":"Organization"}</script>`: {},
		`<script type="text/plain">notes</script>`:                             {},
		`<meta name="google-site-verification" content="x">`:                   {},
		// Same-origin scripts are covered by 'self'.
		`<script src="/js/local.js"></script>`: {connect: []string{"https:"}},
		// Protocol-relative URLs load over https on an https site.
		`<script src="//cdn.example.com/a.js"></script><script type="module" src="https://CDN.example.com/b.js"></script>`: {
			script: []string{"https://cdn.example.com"}, connect: []string{"https:"},
		},
	}
	for snippet, want := range cases {
		if got := snippetCSP(snippet); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v, want %+v", snippet, got, want)
		}
	}
	if got := snippetCSP("", "  ", ""); !reflect.DeepEqual(got, cspDeclaration{}) {
		t.Fatalf("empty snippets = %+v", got)
	}
	// The hash is of the text as the browser parses it: CRLF and CR become LF.
	if scriptHash("a();\r\nb();\rc();") != hashOf("a();\nb();\nc();") {
		t.Fatal("scriptHash does not normalize line breaks")
	}
}

func TestSnippetCSPFollowsTheSnippets(t *testing.T) {
	var cache cspCache
	first := cache.declaration("<script>a()</script>\x00\x00")
	if len(first.script) != 1 || first.script[0] != hashOf("a()") {
		t.Fatalf("first = %+v", first)
	}
	if again := cache.declaration("<script>a()</script>\x00\x00"); !reflect.DeepEqual(again, first) {
		t.Fatal("the same snippets gave another declaration")
	}
	changed := cache.declaration("<script>b()</script>\x00\x00")
	if changed.script[0] != hashOf("b()") {
		t.Fatalf("changed snippets kept the old hash: %+v", changed)
	}
	if cleared := cache.declaration("\x00\x00"); len(cleared.script) != 0 || len(cleared.connect) != 0 {
		t.Fatalf("cleared snippets still declare %+v", cleared)
	}
}

func TestAddCSPSourcesWithoutEngine(t *testing.T) {
	p := New()
	in := core.CSPSources{Script: []string{"https://kept.example.com"}}
	if got := p.addCSPSources(in); !reflect.DeepEqual(got, in) {
		t.Fatalf("an inactive plugin changed the sources: %+v", got)
	}
}
