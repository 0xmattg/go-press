package commercepaypal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	corecommerce "go-press/core/commerce"
	"go-press/pkg/logger"
)

const (
	webhookPath = "/commerce/paypal/webhook"
	returnPath  = "/commerce/paypal/return"
)

func (p *Plugin) registerRoutes(r *gin.Engine) {
	r.POST(webhookPath, p.handleWebhook)
	r.GET(returnPath, p.handleReturn)
}

// handleReturn is where PayPal sends the buyer after approval. It captures the
// order synchronously (reliable even without a reachable webhook, e.g. local
// sandbox), settles it, then forwards the buyer to commerce's own return URL.
func (p *Plugin) handleReturn(c *gin.Context) {
	cfg := p.loadConfig()
	dest := p.safeReturnURL(c.Query("rt"))
	ppOrderID := c.Query("token") // PayPal appends ?token=<order id>&PayerID=...
	if !cfg.ready() || ppOrderID == "" {
		c.Redirect(http.StatusFound, dest)
		return
	}
	cap, err := p.client(cfg).captureOrder(c.Request.Context(), ppOrderID)
	if err != nil {
		logger.Error("commerce-paypal: capture on return failed", "error", err)
		c.Redirect(http.StatusFound, dest)
		return
	}
	if err := p.settleCapture(c.Request.Context(), cap); err != nil {
		logger.Error("commerce-paypal: settle on return failed", "error", err)
	}
	c.Redirect(http.StatusFound, dest)
}

// handleWebhook verifies the PayPal signature and drives settlement. It is the
// authoritative confirmation path; the buyer-return capture is a fast-path that
// funnels through the same idempotent settler.
func (p *Plugin) handleWebhook(c *gin.Context) {
	cfg := p.loadConfig()
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxRespBody))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if cfg.ClientID == "" || cfg.Secret == "" || cfg.WebhookID == "" {
		logger.Error("commerce-paypal: webhook received but gateway not fully configured")
		c.Status(http.StatusServiceUnavailable)
		return
	}
	ok, err := p.client(cfg).verifyWebhook(c.Request.Context(), c.Request.Header, cfg.WebhookID, raw)
	if err != nil {
		logger.Error("commerce-paypal: webhook signature verification unavailable", "error", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	if !ok {
		logger.Error("commerce-paypal: webhook signature verification failed", "verified", false)
		c.Status(http.StatusBadRequest)
		return
	}
	var evt struct {
		ID        string          `json:"id"`
		EventType string          `json:"event_type"`
		Resource  json.RawMessage `json:"resource"`
	}
	if err := json.Unmarshal(raw, &evt); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if err := p.handleEvent(c.Request.Context(), cfg, evt.ID, evt.EventType, evt.Resource); err != nil {
		// A 5xx asks PayPal to retry transient database/settlement failures. Returning
		// 200 here would acknowledge and permanently lose the event.
		logger.Error("commerce-paypal: webhook handling failed", "event", evt.ID, "type", evt.EventType, "error", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusOK)
}

// handleEvent maps a verified PayPal event to an idempotent settlement.
func (p *Plugin) handleEvent(ctx context.Context, cfg config, eventID, eventType string, resource json.RawMessage) error {
	settler := corecommerce.GetSettler(p.hooks)
	if settler == nil {
		return errors.New("commerce-paypal: commerce settler unavailable")
	}
	switch eventType {
	case "CHECKOUT.ORDER.APPROVED":
		// Buyer approved but may not have returned to the site — capture now.
		var res struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(resource, &res); err != nil || res.ID == "" {
			return errors.New("commerce-paypal: approved order missing id")
		}
		cap, err := p.client(cfg).captureOrder(ctx, res.ID)
		if err != nil {
			return fmt.Errorf("commerce-paypal: webhook capture: %w", err)
		}
		return p.settleCapture(ctx, cap)

	case "PAYMENT.CAPTURE.COMPLETED":
		cap := parseCaptureResource(resource)
		if cap.ID == "" || cap.CustomID == "" {
			return errors.New("commerce-paypal: completed capture missing id or order reference")
		}
		return p.settleCapture(ctx, cap)

	case "PAYMENT.CAPTURE.DENIED", "PAYMENT.CAPTURE.DECLINED":
		cap := parseCaptureResource(resource)
		if cap.CustomID == "" {
			return errors.New("commerce-paypal: denied capture missing order reference")
		}
		return settler.Settle(ctx, corecommerce.SettleRequest{
			OrderRef: cap.CustomID, Gateway: gatewayID, TxnID: cap.ID,
			Amount: parseMoney(cap.Amount), Status: corecommerce.SettleFailed,
			IdempotencyKey: "paypal:denied:" + firstNonEmpty(cap.ID, eventID),
		})

	case "PAYMENT.CAPTURE.REFUNDED":
		// Payments v2 defines this webhook resource as the CAPTURE whose
		// aggregate status became REFUNDED, not as an individual refund. Its id
		// is therefore the capture id and its amount is the original capture
		// amount; treating either as a refund id/delta can double-count partial
		// refunds. Admin/API refunds are recorded authoritatively from the
		// RefundWithResult response (which contains the real refund id). Safely
		// acknowledge this capture-level notification instead of fabricating a
		// second local refund fact.
		var capture struct {
			ID     string  `json:"id"`
			Status string  `json:"status"`
			Amount ppMoney `json:"amount"`
		}
		if err := json.Unmarshal(resource, &capture); err != nil {
			return fmt.Errorf("commerce-paypal: decode refunded capture webhook: %w", err)
		}
		status := strings.ToUpper(strings.TrimSpace(capture.Status))
		if capture.ID == "" || (status != "REFUNDED" && status != "PARTIALLY_REFUNDED") {
			return errors.New("commerce-paypal: refunded capture webhook missing refunded capture state")
		}
		logger.Info("commerce-paypal: acknowledged capture-level refund notification",
			"event", eventID, "capture", capture.ID)
		return nil
	}
	return nil
}

// settleCapture reports a successful capture to commerce's settler. Idempotency
// keys on the capture id so the return path and webhook can't double-advance.
func (p *Plugin) settleCapture(ctx context.Context, cap ppCapture) error {
	settler := corecommerce.GetSettler(p.hooks)
	if settler == nil {
		return errors.New("commerce-paypal: commerce settler unavailable")
	}
	if cap.CustomID == "" || cap.ID == "" {
		return errors.New("commerce-paypal: capture missing id or order reference")
	}
	if err := settler.Settle(ctx, corecommerce.SettleRequest{
		OrderRef:       cap.CustomID,
		Gateway:        gatewayID,
		TxnID:          cap.ID,
		Amount:         parseMoney(cap.Amount),
		Status:         corecommerce.SettlePaid,
		IdempotencyKey: "paypal:capture:" + cap.ID,
	}); err != nil {
		return fmt.Errorf("commerce-paypal: settle order %s: %w", cap.CustomID, err)
	}
	return nil
}

// parseCaptureResource decodes a PAYMENT.CAPTURE.* resource (a capture object).
func parseCaptureResource(resource json.RawMessage) ppCapture {
	var r struct {
		ID       string  `json:"id"`
		Status   string  `json:"status"`
		CustomID string  `json:"custom_id"`
		Amount   ppMoney `json:"amount"`
	}
	_ = json.Unmarshal(resource, &r)
	return ppCapture{ID: r.ID, Status: r.Status, CustomID: r.CustomID, Amount: r.Amount}
}

// safeReturnURL accepts only an absolute URL on our own host (guards against an
// open redirect via the rt parameter), falling back to the site root.
func (p *Plugin) safeReturnURL(raw string) string {
	fallback := p.siteBase() + "/"
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fallback
	}
	site, err := url.Parse(p.siteURL)
	if err != nil || !strings.EqualFold(u.Host, site.Host) {
		return fallback
	}
	return u.String()
}
