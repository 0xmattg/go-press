// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package content

import (
	"strings"
	"testing"
)

func TestSanitizeHTMLRemovesExecutableMarkup(t *testing.T) {
	input := `<p onclick="alert(1)">Hello</p><script>alert(2)</script>` +
		`<img src="javascript:alert(3)" onerror="alert(4)">` +
		`<a href="javascript:alert(5)">bad</a>`

	got := SanitizeHTML(input)

	for _, forbidden := range []string{"<script", "onclick", "onerror", "javascript:"} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Fatalf("sanitized HTML still contains %q: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, "<p>Hello</p>") {
		t.Fatalf("expected safe paragraph to remain: %s", got)
	}
}

func TestSanitizeHTMLPreservesRichTextTables(t *testing.T) {
	input := `<h2>Requirements</h2><table><thead><tr><th scope="col">Name</th></tr></thead>` +
		`<tbody><tr><td colspan="2">Value</td></tr></tbody></table>`

	got := SanitizeHTML(input)

	for _, expected := range []string{"<h2>Requirements</h2>", "<table>", "<thead>", `scope="col"`, `colspan="2"`} {
		if !strings.Contains(got, expected) {
			t.Fatalf("sanitized HTML lost %q: %s", expected, got)
		}
	}
}

// Core's front-end interaction script acts on data-gp-* attributes. Only
// theme templates may write them: content and embed codes must not be able
// to open dialogs, copy text or drive any other component.
func TestSanitizersRemoveInteractionAttributes(t *testing.T) {
	input := `<p data-gp-toggle="site-nav" data-gp-dismiss="outside">a</p>` +
		`<a href="/x" data-gp-dialog-open="login">b</a><div data-gp-copy="share-url" data-gp-reveal>c</div>` +
		`<span data-gp-is-open data-gp-progress="">d</span><dialog data-gp-dialog id="login" open>e</dialog>`
	for name, sanitize := range map[string]func(string) string{"SanitizeHTML": SanitizeHTML, "SanitizeEmbed": SanitizeEmbed} {
		got := sanitize(input)
		if strings.Contains(got, "data-gp") || strings.Contains(got, "<dialog") {
			t.Fatalf("%s kept interaction markup: %s", name, got)
		}
		if !strings.Contains(got, ">a<") || !strings.Contains(got, ">c<") {
			t.Fatalf("%s dropped the text: %s", name, got)
		}
	}
}
