package content

import (
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

var (
	richTextPolicyOnce sync.Once
	richTextPolicy     *bluemonday.Policy
)

// SanitizeHTML removes executable or otherwise unsafe markup while preserving
// the rich-text structures supported by the editor, including tables.
func SanitizeHTML(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	richTextPolicyOnce.Do(func() {
		richTextPolicy = bluemonday.UGCPolicy()
	})
	return richTextPolicy.Sanitize(value)
}

func sanitizeContent(item *Content) {
	if item == nil {
		return
	}
	item.Content = SanitizeHTML(item.Content)
}
