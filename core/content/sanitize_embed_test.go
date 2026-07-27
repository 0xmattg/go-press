package content

import (
	"strings"
	"testing"
)

func TestSanitizeEmbedAllowsIframeStripsScript(t *testing.T) {
	in := `<iframe src="https://example.com/widget?x=1" width="600" height="400" allowfullscreen></iframe><script>alert(1)</script>`
	out := SanitizeEmbed(in)
	if !strings.Contains(out, "<iframe") || !strings.Contains(out, `src="https://example.com/widget?x=1"`) {
		t.Fatalf("iframe with https src should be preserved: %q", out)
	}
	if strings.Contains(strings.ToLower(out), "<script") || strings.Contains(out, "alert(") {
		t.Fatalf("script must be stripped: %q", out)
	}
}

func TestSanitizeEmbedDropsDangerousVectors(t *testing.T) {
	out := SanitizeEmbed(`<iframe src="javascript:alert(1)" onload="steal()"></iframe><object data="x"></object><embed src="y">`)
	low := strings.ToLower(out)
	if strings.Contains(low, "javascript:") {
		t.Fatalf("javascript: URL must be dropped: %q", out)
	}
	if strings.Contains(low, "onload") {
		t.Fatalf("event handler must be dropped: %q", out)
	}
	if strings.Contains(low, "<object") || strings.Contains(low, "<embed") {
		t.Fatalf("object/embed must be dropped: %q", out)
	}
	if SanitizeEmbed("") != "" {
		t.Fatal("empty input should stay empty")
	}
}
