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
