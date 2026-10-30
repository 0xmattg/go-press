// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

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
// TestBundledLogosSurviveSanitization checks every logo shipped by a theme or
// plugin in this checkout. Logos are discovered by glob rather than listed, so
// core does not hard-code module names and builds that omit some modules
// (for example the public repository) still pass.
func TestBundledLogosSurviveSanitization(t *testing.T) {
	root := filepath.Join("..", "..")
	var logos []string
	for _, pattern := range []string{"themes/*/static/logo.svg", "plugins/*/static/logo.svg"} {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			t.Fatal(err)
		}
		logos = append(logos, matches...)
	}
	if len(logos) == 0 {
		t.Fatal("no bundled theme or plugin logos found")
	}
	for _, path := range logos {
		name, _ := filepath.Rel(root, path)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		out := SanitizeSVG(string(raw))
		if !strings.Contains(out, "<svg") || !strings.Contains(out, "viewBox=") {
			t.Errorf("%s: logo did not survive sanitization: %s", name, out)
		}
	}
}

func TestShopStarterLogoKeepsVisibleBackgroundAfterSanitization(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "themes", "shop-starter", "static", "logo.svg"))
	if err != nil {
		t.Fatalf("read Shop Starter logo: %v", err)
	}

	out := SanitizeSVG(string(raw))
	if !strings.Contains(out, `fill="#6657e8"`) {
		t.Fatalf("Shop Starter logo lost its visible background during sanitization: %s", out)
	}
	if strings.Contains(out, "url(#") {
		t.Fatalf("Shop Starter logo must not depend on sanitizer-stripped paint servers: %s", out)
	}
}
