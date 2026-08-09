package commercepaypal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corecommerce "github.com/0xmattg/go-press/core/commerce"
	"github.com/0xmattg/go-press/core/hook"

	"github.com/gin-gonic/gin"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingSettler struct{ err error }

func (s failingSettler) Settle(context.Context, corecommerce.SettleRequest) error { return s.err }

type testOptions map[string]string

func (o testOptions) Get(key string) string { return o[key] }
func (o testOptions) GetDefault(key, fallback string) string {
	if value := o[key]; value != "" {
		return value
	}
	return fallback
}
func (o testOptions) Set(key, value string) error { o[key] = value; return nil }

func TestDecimalString(t *testing.T) {
	cases := map[int64]string{
		0:             "0.00",
		5:             "0.05",
		199:           "1.99",
		1000:          "10.00",
		123456:        "1234.56",
		-250:          "-2.50",
		math.MaxInt64: "92233720368547758.07",
		math.MinInt64: "-92233720368547758.08",
	}
	for minor, want := range cases {
		if got := decimalString(minor); got != want {
			t.Errorf("decimalString(%d) = %q, want %q", minor, got, want)
		}
	}
}

func TestParseMinorRoundTrip(t *testing.T) {
	for _, minor := range []int64{0, 5, 199, 1000, 123456} {
		if got := parseMinor(decimalString(minor)); got != minor {
			t.Errorf("parseMinor(decimalString(%d)) = %d", minor, got)
		}
	}
	// Tolerant parsing of assorted PayPal-shaped strings.
	for in, want := range map[string]int64{"19.9": 1990, "7": 700, " 3.05 ": 305, "0.5": 50} {
		if got := parseMinor(in); got != want {
			t.Errorf("parseMinor(%q) = %d, want %d", in, got, want)
		}
	}
	for _, invalid := range []string{
		"92233720368547758.08", "-92233720368547758.09", "999999999999999999999999",
		"not-money", "1.2.3", "1.234", "1e6",
	} {
		if got := parseMinor(invalid); got != 0 {
			t.Errorf("parseMinor(%q) = %d, want safe zero", invalid, got)
		}
	}
	if got := parseMinor("-92233720368547758.08"); got != math.MinInt64 {
		t.Errorf("parseMinor(MinInt64) = %d, want %d", got, int64(math.MinInt64))
	}
}

func TestConfigReadyAndBase(t *testing.T) {
	c := config{Enabled: true, ClientID: "id", Secret: "sec", Sandbox: true}
	if !c.ready() {
		t.Fatal("fully-configured gateway should be ready")
	}
	if c.apiBase() != "https://api-m.sandbox.paypal.com" {
		t.Errorf("sandbox base = %q", c.apiBase())
	}
	c.Sandbox = false
	if c.apiBase() != "https://api-m.paypal.com" {
		t.Errorf("live base = %q", c.apiBase())
	}
	if (config{Enabled: true, ClientID: "id"}).ready() {
		t.Error("missing secret should not be ready")
	}
	if (config{ClientID: "id", Secret: "sec"}).ready() {
		t.Error("disabled gateway should not be ready")
	}
}

func TestPayPalGatewayUnavailableUntilFullyConfigured(t *testing.T) {
	opts := testOptions{optEnabled: "1", optClientID: "client"}
	gateway := payPalGateway{p: &Plugin{options: opts}}
	if gateway.Available(nil) {
		t.Fatal("gateway with a missing secret must not appear at checkout")
	}
	opts[optSecret] = "secret"
	if !gateway.Available(nil) {
		t.Fatal("fully configured enabled gateway should be available")
	}
	opts[optEnabled] = "0"
	if gateway.Available(nil) {
		t.Fatal("disabled gateway must not appear at checkout")
	}
}

func TestPayPalUnconfiguredStartFailureIsDefinitive(t *testing.T) {
	gateway := payPalGateway{p: &Plugin{options: testOptions{}}}
	_, err := gateway.StartPayment(nil, corecommerce.PaymentRequest{})
	if err == nil || !corecommerce.IsDefinitiveStartFailure(err) {
		t.Fatalf("unconfigured StartPayment error = %v, definitive=%v", err, corecommerce.IsDefinitiveStartFailure(err))
	}
}

func TestFirstCapture(t *testing.T) {
	tests := []struct {
		name       string
		response   string
		wantOK     bool
		wantID     string
		wantCustom string
		wantAmount int64
	}{
		{
			name: "falls back to purchase unit custom id",
			response: `{"purchase_units":[{"custom_id":"ORDER-1","payments":{"captures":[` +
				`{"id":"CAP-1","status":"COMPLETED","amount":{"currency_code":"USD","value":"10.00"}}]}}]}`,
			wantOK:     true,
			wantID:     "CAP-1",
			wantCustom: "ORDER-1",
			wantAmount: 1000,
		},
		{
			name: "prefers capture custom id",
			response: `{"purchase_units":[{"custom_id":"ORDER-1","payments":{"captures":[` +
				`{"id":"CAP-1","custom_id":"CAPTURE-ORDER-1","status":"COMPLETED"}]}}]}`,
			wantOK:     true,
			wantID:     "CAP-1",
			wantCustom: "CAPTURE-ORDER-1",
		},
		{
			name: "skips purchase units without captures",
			response: `{"purchase_units":[{"custom_id":"EMPTY","payments":{"captures":[]}},` +
				`{"custom_id":"ORDER-2","payments":{"captures":[{"id":"CAP-2"}]}}]}`,
			wantOK:     true,
			wantID:     "CAP-2",
			wantCustom: "ORDER-2",
		},
		{
			name: "returns first capture",
			response: `{"purchase_units":[{"custom_id":"ORDER-1","payments":{"captures":[` +
				`{"id":"CAP-1"},{"id":"CAP-2"}]}}]}`,
			wantOK:     true,
			wantID:     "CAP-1",
			wantCustom: "ORDER-1",
		},
		{
			name:     "reports missing captures",
			response: `{"purchase_units":[{"payments":{"captures":[]}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var o ppOrderResponse
			if err := json.Unmarshal([]byte(tt.response), &o); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			capture, ok := o.firstCapture()
			if ok != tt.wantOK || capture.ID != tt.wantID || capture.CustomID != tt.wantCustom {
				t.Fatalf("firstCapture() = %+v, ok=%v; want id=%q, custom_id=%q, ok=%v",
					capture, ok, tt.wantID, tt.wantCustom, tt.wantOK)
			}
			if amount := parseMoney(capture.Amount).Amount; amount != tt.wantAmount {
				t.Errorf("capture amount = %d, want %d", amount, tt.wantAmount)
			}
		})
	}
}

func TestPayPalMutationsCarryIdempotencyKeys(t *testing.T) {
	seen := map[string]string{}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := ""
		switch r.URL.Path {
		case "/v1/oauth2/token":
			body = `{"access_token":"token"}`
		case "/v2/checkout/orders":
			seen["create"] = r.Header.Get("PayPal-Request-Id")
			status = http.StatusCreated
			body = `{"id":"PP-ORDER","links":[{"rel":"approve","href":"https://paypal.test/approve"}]}`
		case "/v2/checkout/orders/PP-ORDER/capture":
			seen["capture"] = r.Header.Get("PayPal-Request-Id")
			status = http.StatusCreated
			body = `{"purchase_units":[{"custom_id":"ORDER-1","payments":{"captures":[{"id":"CAP-1","status":"COMPLETED","amount":{"currency_code":"USD","value":"10.00"}}]}}]}`
		case "/v2/payments/captures/CAP-1/refund":
			seen["refund"] = r.Header.Get("PayPal-Request-Id")
			status = http.StatusCreated
			body = `{"id":"REF-1"}`
		default:
			status = http.StatusNotFound
		}
		return &http.Response{
			StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r,
		}, nil
	})

	client := &payPalClient{http: &http.Client{Transport: transport}, base: "https://api.test", id: "id", secret: "secret"}
	req := corecommerce.PaymentRequest{
		OrderRef: "ORDER-1", Amount: corecommerce.New(1000, "USD"), IdempotencyKey: "start:ORDER-1",
	}
	if _, _, err := client.createOrder(context.Background(), req, "https://shop.test/return", "https://shop.test/cancel"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.captureOrder(context.Background(), "PP-ORDER"); err != nil {
		t.Fatal(err)
	}
	result, err := client.refund(context.Background(), corecommerce.RefundRequest{
		PaymentID: "CAP-1", Amount: corecommerce.New(500, "USD"), IdempotencyKey: "refund:abc",
	})
	if err != nil || result.TransactionID != "REF-1" {
		t.Fatalf("refund result = %+v, err=%v", result, err)
	}
	if seen["create"] != "start:ORDER-1" || seen["capture"] != "capture:PP-ORDER" || seen["refund"] != "refund:abc" {
		t.Fatalf("PayPal-Request-Id headers = %#v", seen)
	}
}

func TestWebhookEventPropagatesSettlementFailureAndValidatesRefundedCapture(t *testing.T) {
	want := errors.New("database temporarily unavailable")
	bus := hook.New()
	corecommerce.SetSettler(bus, failingSettler{err: want})
	p := &Plugin{hooks: bus}

	err := p.handleEvent(context.Background(), config{}, "event-1", "PAYMENT.CAPTURE.COMPLETED", json.RawMessage(
		`{"id":"CAP-1","custom_id":"ORDER-1","amount":{"currency_code":"USD","value":"10.00"}}`,
	))
	if !errors.Is(err, want) {
		t.Fatalf("settlement error = %v, want propagated database error", err)
	}
	err = p.handleEvent(context.Background(), config{}, "event-2", "PAYMENT.CAPTURE.REFUNDED", json.RawMessage(
		`{"id":"","status":"PARTIALLY_REFUNDED","custom_id":"ORDER-1","amount":{"currency_code":"USD","value":"10.00"}}`,
	))
	if err == nil {
		t.Fatal("capture refund webhook without a capture id must be rejected")
	}
	// A capture-level REFUNDED event is acknowledged without calling Settle:
	// resource.id is a capture id and amount is the original capture total, not
	// an individual refund id/delta.
	if err := p.handleEvent(context.Background(), config{}, "event-3", "PAYMENT.CAPTURE.REFUNDED", json.RawMessage(
		`{"id":"CAP-1","status":"REFUNDED","custom_id":"ORDER-1","amount":{"currency_code":"USD","value":"10.00"}}`,
	)); err != nil {
		t.Fatalf("valid refunded capture notification: %v", err)
	}
	if err := p.handleEvent(context.Background(), config{}, "event-4", "PAYMENT.CAPTURE.REFUNDED", json.RawMessage(
		`{"id":"CAP-1","status":"PARTIALLY_REFUNDED","amount":{"currency_code":"USD","value":"10.00"}}`,
	)); err != nil {
		t.Fatalf("valid partially-refunded capture notification: %v", err)
	}
}

func TestWebhookVerificationTransportErrorReturns5xxButInvalidSignatureReturns4xx(t *testing.T) {
	opts := testOptions{
		optEnabled: "1", optClientID: "client", optSecret: "secret", optWebhookID: "webhook",
	}
	wantTransport := errors.New("paypal unavailable")
	p := &Plugin{options: opts, httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, wantTransport
	})}}
	router := gin.New()
	router.POST(webhookPath, p.handleWebhook)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, webhookPath, strings.NewReader(`{}`)))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("verification transport status = %d, want 500", recorder.Code)
	}

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"access_token":"token"}`
		if r.URL.Path == "/v1/notifications/verify-webhook-signature" {
			body = `{"verification_status":"FAILURE"}`
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r,
		}, nil
	})
	p.httpClient = &http.Client{Transport: transport}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, webhookPath, strings.NewReader(`{}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid signature status = %d, want 400", recorder.Code)
	}
}
