package commerce

import (
	"bytes"
	"html/template"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corecommerce "go-press/core/commerce"
)

func TestPaymentActionTemplatesRender(t *testing.T) {
	funcs := template.FuncMap{
		"renderHook": func(string, interface{}) template.HTML { return template.HTML(`<div id="gateway-widget"></div>`) },
		"T":          func(_ interface{}, key string) string { return key },
	}
	expires := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)

	received, err := template.New("").Funcs(funcs).ParseFiles(filepath.Join("templates", "commerce", "order-received.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = received.ExecuteTemplate(&output, "content", map[string]interface{}{
		"Heading": "Order received",
		"Order":   Order{Number: "DISPLAY-1"},
		"PayAction": corecommerce.DisplayAction{
			Title: "Pay by transfer", Rows: []corecommerce.KV{{Label: "Reference", Value: "DISPLAY-1"}},
			QR: "pay:display-1", ExpiresAt: &expires,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Pay by transfer", "DISPLAY-1", "pay:display-1", "2026-08-01"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("display payment output missing %q: %s", want, output.String())
		}
	}

	inline, err := template.New("").Funcs(funcs).ParseFiles(filepath.Join("templates", "commerce", "payment-inline.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	err = inline.ExecuteTemplate(&output, "content", map[string]interface{}{
		"Order": Order{Number: "INLINE-1"}, "ClientDataJSON": `{"token":"client-token"}`,
		"CompleteURL": "/checkout/complete/INLINE-1?key=secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gateway-widget", "gopress:payment:inline", "client-token"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("inline payment output missing %q: %s", want, output.String())
		}
	}
}
