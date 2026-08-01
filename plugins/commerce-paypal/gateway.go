package commercepaypal

import (
	"errors"
	"net/url"

	"github.com/gin-gonic/gin"

	corecommerce "go-press/core/commerce"
)

// gatewayID is the stable payment-method identifier stored on orders.
const gatewayID = "paypal"

// payPalGateway implements corecommerce.PaymentGateway. It holds a back-pointer
// to the plugin only for config/HTTP access; all commerce interaction is through
// the core contracts (PaymentRequest / PaymentAction / the settler).
type payPalGateway struct{ p *Plugin }

func (payPalGateway) ID() string                { return gatewayID }
func (payPalGateway) Title(*gin.Context) string { return "PayPal" }
func (payPalGateway) Icon() string              { return "🅿" }

func (payPalGateway) Capabilities() corecommerce.Capabilities {
	return corecommerce.Capabilities{Refund: true, PartialRefund: true}
}

// Available implements corecommerce.GatewayAvailability. Registration means
// the plugin is active; checkout availability additionally requires the
// gateway to be enabled and its credentials to be complete.
func (g payPalGateway) Available(*gin.Context) bool {
	return g.p != nil && g.p.loadConfig().ready()
}

// StartPayment creates a PayPal Orders v2 order and returns the buyer approval
// URL as a RedirectAction. The order stays pending until PayPal confirms the
// capture (via the buyer-return route or the webhook), which calls the settler.
func (g payPalGateway) StartPayment(c *gin.Context, req corecommerce.PaymentRequest) (corecommerce.PaymentAction, error) {
	cfg := g.p.loadConfig()
	if !cfg.ready() {
		return nil, corecommerce.DefinitiveStartFailure(errors.New("PayPal 未配置或未启用"))
	}
	// PayPal returns the buyer to our own return route, which captures the order
	// and then forwards them to commerce's ReturnURL (which carries the order
	// access key so a guest can view their confirmation).
	returnURL := g.p.siteBase() + returnPath + "?rt=" + url.QueryEscape(req.ReturnURL)
	_, approveURL, err := g.p.client(cfg).createOrder(c.Request.Context(), req, returnURL, req.CancelURL)
	if err != nil {
		return nil, err
	}
	return corecommerce.RedirectAction{URL: approveURL}, nil
}

// Refund issues a PayPal refund against the capture. commerce passes the capture
// transaction id as RefundRequest.PaymentID.
func (g payPalGateway) Refund(c *gin.Context, req corecommerce.RefundRequest) error {
	_, err := g.RefundWithResult(c, req)
	return err
}

// RefundWithResult implements corecommerce.IdempotentRefunder. The returned
// PayPal refund id lets Commerce correlate the later refund webhook.
func (g payPalGateway) RefundWithResult(c *gin.Context, req corecommerce.RefundRequest) (corecommerce.RefundResult, error) {
	cfg := g.p.loadConfig()
	if !cfg.ready() {
		return corecommerce.RefundResult{}, errors.New("PayPal 未配置")
	}
	return g.p.client(cfg).refund(c.Request.Context(), req)
}

var (
	_ corecommerce.PaymentGateway      = payPalGateway{}
	_ corecommerce.GatewayAvailability = payPalGateway{}
	_ corecommerce.IdempotentRefunder  = payPalGateway{}
)
