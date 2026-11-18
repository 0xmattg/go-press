package commerce

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corecommerce "github.com/0xmattg/go-press/core/commerce"

	"github.com/gin-gonic/gin"
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
		"CompleteURL":     "/checkout/complete/INLINE-1?key=secret",
		"InlineScriptURL": inlinePaymentScriptPath + "?v=1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gateway-widget", "client-token", `<script src="/commerce/assets/payment-inline.js?v=1.0.0"></script>`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("inline payment output missing %q: %s", want, output.String())
		}
	}
	// No inline script: the page must run under a policy that refuses them.
	if strings.Contains(output.String(), "<script>") {
		t.Fatalf("inline payment page has an inline script: %s", output.String())
	}
}

func TestInlinePaymentScriptIsServed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := &Plugin{}
	router := gin.New()
	router.GET(inlinePaymentScriptPath, p.handleInlinePaymentScript)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, inlinePaymentScriptPath+"?v=1", nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("script = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, want := range []string{"gopress-inline-payment", "gopress:payment:inline", "data-complete-url"} {
		if !strings.Contains(body, want) {
			t.Fatalf("script lacks %q", want)
		}
	}
}
