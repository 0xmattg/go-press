package core

import (
	"bytes"
	"testing"
)

func TestInjectPoweredByBeforeBodyClose(t *testing.T) {
	body := []byte("<html><body><main>Hello</main></body></html>")
	got := injectPoweredBy(body)
	if !bytes.Contains(got, []byte(poweredByMarker)) {
		t.Fatalf("expected powered by marker in output: %s", got)
	}
	if !bytes.Contains(got, []byte(poweredByBlockHTML+"</body>")) {
		t.Fatalf("expected powered by HTML before closing body: %s", got)
	}
}

func TestInjectPoweredByInlineAfterCopyright(t *testing.T) {
	body := []byte("<html><body><footer><p>© Example. All rights reserved.</p></footer></body></html>")
	got := injectPoweredBy(body)
	want := []byte("All rights reserved." + poweredByInlineHTML + "</p>")
	if !bytes.Contains(got, want) {
		t.Fatalf("expected inline powered by HTML after copyright text: %s", got)
	}
	if bytes.Contains(got, []byte(poweredByBlockHTML)) {
		t.Fatalf("did not expect fallback block when copyright line exists: %s", got)
	}
}

func TestInjectPoweredByInlineIntoFooterBottom(t *testing.T) {
	body := []byte(`<html><body><footer><div class="as-footer-bottom"><span>Built for stability. Designed for growth.</span><span>01 / 04</span></div></footer></body></html>`)
	got := injectPoweredBy(body)
	want := []byte("Built for stability. Designed for growth." + poweredByInlineHTML + "</span>")
	if !bytes.Contains(got, want) {
		t.Fatalf("expected inline powered by HTML inside footer-bottom text: %s", got)
	}
	if bytes.Contains(got, []byte(poweredByBlockHTML)) {
		t.Fatalf("did not expect fallback block when footer-bottom exists: %s", got)
	}
}

func TestInjectPoweredBySkipsExistingMarker(t *testing.T) {
	body := []byte(`<html><body><div class="gopress-powered-by">Site Powered by GoPress</div></body></html>`)
	got := injectPoweredBy(body)
	if bytes.Count(got, []byte(poweredByMarker)) != 1 {
		t.Fatalf("expected existing marker to remain unique: %s", got)
	}
}

func TestSkipPoweredByPath(t *testing.T) {
	for _, path := range []string{"/admin", "/admin/settings", "/api/v1/types", "/install", "/install/site", "/static/css/style.css", "/swagger/index.html", "/health", "/sitemap.xml", "/robots.txt"} {
		if !skipPoweredByPath(path) {
			t.Fatalf("expected %s to be skipped", path)
		}
	}
	if skipPoweredByPath("/") {
		t.Fatal("expected public homepage to be eligible")
	}
}
