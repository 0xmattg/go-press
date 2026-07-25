package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeSVGStripsScripting(t *testing.T) {
	in := `<svg viewBox="0 0 48 48" xmlns="http://www.w3.org/2000/svg">` +
		`<script>alert(1)</script>` +
		`<circle cx="24" cy="24" r="10" onload="steal()" fill="#fff"/>` +
		`<a href="javascript:evil()"><rect width="10" height="10"/></a>` +
		`</svg>`
	out := SanitizeSVG(in)
	for _, bad := range []string{"script", "alert", "onload", "javascript:", "<a"} {
		if strings.Contains(out, bad) {
			t.Errorf("sanitized SVG still contains %q: %s", bad, out)
		}
	}
	if !strings.Contains(out, "<circle") {
		t.Errorf("sanitized SVG dropped the circle shape: %s", out)
	}
}

func TestSanitizeSVGPreservesViewBoxCase(t *testing.T) {
	out := SanitizeSVG(`<svg viewBox="0 0 48 48" xmlns="http://www.w3.org/2000/svg"><rect width="48" height="48" rx="11" fill="#4f46e5"/></svg>`)
	if !strings.Contains(out, "viewBox=") {
		t.Fatalf("viewBox case was not preserved: %s", out)
	}
	if strings.Contains(out, "viewbox=") {
		t.Fatalf("lowercase viewbox leaked into output: %s", out)
	}
}

func TestSanitizeSVGEmpty(t *testing.T) {
	if got := SanitizeSVG("   "); got != "" {
		t.Fatalf("SanitizeSVG(blank) = %q, want empty", got)
	}
}

// The bundled theme and plugin logos must survive sanitization intact (viewBox
// preserved, shapes kept) so the admin cards render them.
func TestBundledLogosSurviveSanitization(t *testing.T) {
	root := filepath.Join("..", "..")
	dirs := []string{
		"themes/modern-company", "themes/atelier-slate", "themes/atelier-slate-gp",
		"themes/axis-form", "themes/bitcuz-mag", "themes/civic-estate",
		"themes/financial-news", "themes/florafi", "themes/go-press-landing",
		"themes/terra-trail",
		"plugins/code-snippets", "plugins/gopress-analytics", "plugins/multilang",
		"plugins/seo-extras", "plugins/google-identity", "plugins/metamask-identity",
	}
	for _, d := range dirs {
		raw, err := os.ReadFile(filepath.Join(root, d, "static", "logo.svg"))
		if err != nil {
			t.Errorf("%s: missing logo.svg: %v", d, err)
			continue
		}
		out := SanitizeSVG(string(raw))
		if !strings.Contains(out, "<svg") || !strings.Contains(out, "viewBox=") {
			t.Errorf("%s: logo did not survive sanitization: %s", d, out)
		}
	}
}
