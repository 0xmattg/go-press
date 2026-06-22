package theme

import (
	"bytes"
	"strings"
	"testing"
)

func TestFallbackSingleTemplateSanitizesContent(t *testing.T) {
	var out bytes.Buffer
	data := map[string]interface{}{
		"Item": map[string]interface{}{
			"Title":   "Example",
			"Content": `<p>Safe</p><script>alert(1)</script>`,
		},
	}

	if err := FallbackSingleTemplate().Execute(&out, data); err != nil {
		t.Fatalf("execute fallback template: %v", err)
	}
	got := out.String()
	if strings.Contains(strings.ToLower(got), "<script") {
		t.Fatalf("fallback rendered executable markup: %s", got)
	}
	if !strings.Contains(got, "<p>Safe</p>") {
		t.Fatalf("fallback lost safe markup: %s", got)
	}
}
