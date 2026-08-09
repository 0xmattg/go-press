package commercepaypal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	corecommerce "github.com/0xmattg/go-press/core/commerce"
)

// maxRespBody caps how much of a PayPal response we read, guarding against a
// misbehaving/hostile upstream.
const maxRespBody = 1 << 20

// payPalClient is a thin PayPal REST v2 client. It is stateless per call
// (fetches a fresh OAuth token each request) so there is no shared mutable
// state to synchronize — fine for the low request volume of checkout/refund.
type payPalClient struct {
	http   *http.Client
	base   string
	id     string
	secret string
}

func (p *Plugin) client(cfg config) *payPalClient {
	return &payPalClient{http: p.httpClient, base: cfg.apiBase(), id: cfg.ClientID, secret: cfg.Secret}
}

// ---- shared response shapes ----

type ppMoney struct {
	CurrencyCode string `json:"currency_code"`
	Value        string `json:"value"`
}

// ppCapture is the normalized result of a completed capture.
type ppCapture struct {
	ID       string // PayPal capture id (the settlement txn)
	CustomID string // our order number (from purchase_unit custom_id)
	Status   string
	Amount   ppMoney
}

type ppOrderResponse struct {
	ID            string   `json:"id"`
	Status        string   `json:"status"`
	Links         []ppLink `json:"links"`
	PurchaseUnits []struct {
		CustomID    string `json:"custom_id"`
		ReferenceID string `json:"reference_id"`
		Payments    struct {
			Captures []struct {
				ID       string  `json:"id"`
				Status   string  `json:"status"`
				CustomID string  `json:"custom_id"`
				Amount   ppMoney `json:"amount"`
			} `json:"captures"`
		} `json:"payments"`
	} `json:"purchase_units"`
}

type ppLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

func (o ppOrderResponse) firstCapture() (ppCapture, bool) {
	for _, pu := range o.PurchaseUnits {
		if len(pu.Payments.Captures) == 0 {
			continue
		}

		cp := pu.Payments.Captures[0]
		custom := cp.CustomID
		if custom == "" {
			custom = pu.CustomID
		}
		return ppCapture{ID: cp.ID, CustomID: custom, Status: cp.Status, Amount: cp.Amount}, true
	}
	return ppCapture{}, false
}

// ---- auth + transport ----

func (c *payPalClient) token(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/oauth2/token",
		strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.id, c.secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxRespBody))
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("paypal auth failed (%d): %s", res.StatusCode, snippet(body))
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", errors.New("paypal auth: empty access token")
	}
	return out.AccessToken, nil
}

// do performs an authenticated JSON request, decoding into out (when non-nil).
func (c *payPalClient) do(ctx context.Context, method, path string, payload, out interface{}) (int, []byte, error) {
	return c.doWithRequestID(ctx, method, path, payload, out, "")
}

// doWithRequestID additionally sets PayPal-Request-Id for mutation retries.
// PayPal uses this key to return the original result instead of creating a
// second order/refund when a response is lost or a request is retried.
func (c *payPalClient) doWithRequestID(ctx context.Context, method, path string, payload, out interface{}, requestID string) (int, []byte, error) {
	tok, err := c.token(ctx)
	if err != nil {
		return 0, nil, err
	}
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if requestID = strings.TrimSpace(requestID); requestID != "" {
		req.Header.Set("PayPal-Request-Id", requestID)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, maxRespBody))
	if out != nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, out)
	}
	return res.StatusCode, raw, nil
}

// ---- operations ----

// createOrder creates a CAPTURE-intent order and returns its id + approval URL.
func (c *payPalClient) createOrder(ctx context.Context, req corecommerce.PaymentRequest, returnURL, cancelURL string) (orderID, approveURL string, err error) {
	payload := map[string]interface{}{
		"intent": "CAPTURE",
		"purchase_units": []map[string]interface{}{{
			"reference_id": req.OrderRef,
			"custom_id":    req.OrderRef,
			"amount":       ppMoney{CurrencyCode: req.Amount.Currency, Value: decimalString(req.Amount.Amount)},
		}},
		"application_context": map[string]interface{}{
			"return_url":          returnURL,
			"cancel_url":          cancelURL,
			"user_action":         "PAY_NOW",
			"shipping_preference": "NO_SHIPPING",
		},
	}
	var out ppOrderResponse
	status, raw, err := c.doWithRequestID(ctx, http.MethodPost, "/v2/checkout/orders", payload, &out, req.IdempotencyKey)
	if err != nil {
		return "", "", err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return "", "", fmt.Errorf("paypal create order failed (%d): %s", status, snippet(raw))
	}
	for _, l := range out.Links {
		if l.Rel == "approve" {
			approveURL = l.Href
		}
	}
	if out.ID == "" || approveURL == "" {
		return "", "", errors.New("paypal create order: missing id or approval link")
	}
	return out.ID, approveURL, nil
}

// captureOrder captures an approved order, returning the resulting capture. If
// the order was already captured (e.g. the webhook beat the buyer return), it
// reads the existing capture instead of failing, so both paths are idempotent.
func (c *payPalClient) captureOrder(ctx context.Context, ppOrderID string) (ppCapture, error) {
	var out ppOrderResponse
	status, raw, err := c.doWithRequestID(ctx, http.MethodPost,
		"/v2/checkout/orders/"+url.PathEscape(ppOrderID)+"/capture", map[string]interface{}{}, &out, "capture:"+ppOrderID)
	if err != nil {
		return ppCapture{}, err
	}
	if status == http.StatusCreated || status == http.StatusOK {
		if cap, ok := out.firstCapture(); ok {
			return cap, nil
		}
		return ppCapture{}, errors.New("paypal capture: no capture in response")
	}
	if strings.Contains(string(raw), "ORDER_ALREADY_CAPTURED") {
		return c.getOrderCapture(ctx, ppOrderID)
	}
	return ppCapture{}, fmt.Errorf("paypal capture failed (%d): %s", status, snippet(raw))
}

// getOrderCapture reads an order's existing capture (used when a capture attempt
// reports the order was already captured).
func (c *payPalClient) getOrderCapture(ctx context.Context, ppOrderID string) (ppCapture, error) {
	var out ppOrderResponse
	status, raw, err := c.do(ctx, http.MethodGet, "/v2/checkout/orders/"+url.PathEscape(ppOrderID), nil, &out)
	if err != nil {
		return ppCapture{}, err
	}
	if status != http.StatusOK {
		return ppCapture{}, fmt.Errorf("paypal get order failed (%d): %s", status, snippet(raw))
	}
	if cap, ok := out.firstCapture(); ok {
		return cap, nil
	}
	return ppCapture{}, errors.New("paypal get order: no capture found")
}

// refund issues a (partial) refund against a capture and returns PayPal's refund
// id. PayPal-Request-Id makes retries with the same Commerce key idempotent.
func (c *payPalClient) refund(ctx context.Context, req corecommerce.RefundRequest) (corecommerce.RefundResult, error) {
	if req.PaymentID == "" {
		return corecommerce.RefundResult{}, errors.New("paypal refund: missing capture id")
	}
	payload := map[string]interface{}{
		"amount": ppMoney{CurrencyCode: req.Amount.Currency, Value: decimalString(req.Amount.Amount)},
	}
	var out struct {
		ID string `json:"id"`
	}
	status, raw, err := c.doWithRequestID(ctx, http.MethodPost,
		"/v2/payments/captures/"+url.PathEscape(req.PaymentID)+"/refund", payload, &out, req.IdempotencyKey)
	if err != nil {
		return corecommerce.RefundResult{}, err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return corecommerce.RefundResult{}, fmt.Errorf("paypal refund failed (%d): %s", status, snippet(raw))
	}
	if out.ID == "" {
		return corecommerce.RefundResult{}, errors.New("paypal refund: missing refund id")
	}
	return corecommerce.RefundResult{TransactionID: out.ID}, nil
}

// verifyWebhook asks PayPal to verify a webhook's signature against the
// configured webhook id. Returns true only on verification_status == SUCCESS.
func (c *payPalClient) verifyWebhook(ctx context.Context, h http.Header, webhookID string, rawEvent []byte) (bool, error) {
	payload := map[string]interface{}{
		"auth_algo":         h.Get("Paypal-Auth-Algo"),
		"cert_url":          h.Get("Paypal-Cert-Url"),
		"transmission_id":   h.Get("Paypal-Transmission-Id"),
		"transmission_sig":  h.Get("Paypal-Transmission-Sig"),
		"transmission_time": h.Get("Paypal-Transmission-Time"),
		"webhook_id":        webhookID,
		"webhook_event":     json.RawMessage(rawEvent),
	}
	var out struct {
		VerificationStatus string `json:"verification_status"`
	}
	status, raw, err := c.do(ctx, http.MethodPost, "/v1/notifications/verify-webhook-signature", payload, &out)
	if err != nil {
		return false, err
	}
	if status != http.StatusOK {
		return false, fmt.Errorf("paypal verify failed (%d): %s", status, snippet(raw))
	}
	return out.VerificationStatus == "SUCCESS", nil
}

// ---- money helpers ----

// decimalString renders integer minor units to a 2-decimal string ("1999" ->
// "19.99"), matching commerce's 2-decimal currency scope (v1).
func decimalString(minor int64) string {
	sign := ""
	var magnitude uint64
	if minor < 0 {
		sign = "-"
		// -(MinInt64) cannot be represented as int64. Convert through the
		// adjacent value so every possible integer formats without wrapping.
		magnitude = uint64(-(minor + 1)) + 1
	} else {
		magnitude = uint64(minor)
	}
	return sign + strconv.FormatUint(magnitude/100, 10) + "." + fmt.Sprintf("%02d", magnitude%100)
}

// parseMinor converts a PayPal decimal string ("19.99") to integer minor units.
func parseMinor(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	negative := strings.HasPrefix(v, "-")
	if negative || strings.HasPrefix(v, "+") {
		v = v[1:]
	}
	whole, frac := v, "0"
	if i := strings.IndexByte(v, '.'); i >= 0 {
		if strings.IndexByte(v[i+1:], '.') >= 0 {
			return 0
		}
		whole, frac = v[:i], v[i+1:]
	}
	if len(frac) > 2 {
		return 0
	}
	if whole == "" {
		whole = "0"
	}
	if !asciiDigits(whole) || (frac != "" && !asciiDigits(frac)) {
		return 0
	}
	units, err := strconv.ParseUint(whole, 10, 64)
	if err != nil {
		return 0
	}
	fraction := (frac + "00")[:2]
	cents, err := strconv.ParseUint(fraction, 10, 64)
	if err != nil {
		return 0
	}
	limit := uint64(math.MaxInt64)
	if negative {
		limit++ // absolute value of math.MinInt64
	}
	if units > limit/100 || (units == limit/100 && cents > limit%100) {
		return 0
	}
	magnitude := units*100 + cents
	if negative {
		if magnitude == uint64(math.MaxInt64)+1 {
			return math.MinInt64
		}
		return -int64(magnitude)
	}
	return int64(magnitude)
}

func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseMoney(m ppMoney) corecommerce.Money {
	return corecommerce.New(parseMinor(m.Value), m.CurrencyCode)
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
