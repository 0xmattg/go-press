package commerce

import (
	"strings"

	corecommerce "go-press/core/commerce"

	"github.com/gin-gonic/gin"
)

// offlineGatewayID is the built-in bank-transfer gateway. It has zero external
// dependencies so a fresh store can take orders immediately: the buyer is shown
// bank details, the order goes on-hold, and an admin marks it paid once funds
// arrive (which calls the settler like any other gateway).
const offlineGatewayID = "offline"

// offlineGateway is the built-in manual bank-transfer method. StartPayment shows
// the store's bank details (from settings) as a DisplayAction; confirmation is
// the admin "mark paid" action on the order, not an automated callback.
type offlineGateway struct{ p *Plugin }

func (offlineGateway) ID() string { return offlineGatewayID }
func (g offlineGateway) Title(c *gin.Context) string {
	return g.p.t(c, "commerce.offline.title")
}
func (offlineGateway) Icon() string { return "🏦" }

func (offlineGateway) Capabilities() corecommerce.Capabilities {
	// Offline refunds happen out-of-band (bank transfer back); the admin records
	// them manually, so we don't advertise an automated Refund call.
	return corecommerce.Capabilities{Refund: false, PartialRefund: false}
}

// StartPayment returns the bank-transfer instructions. The order is moved to
// on_hold by the checkout orchestration when it receives this DisplayAction.
func (g offlineGateway) StartPayment(c *gin.Context, req corecommerce.PaymentRequest) (corecommerce.PaymentAction, error) {
	return corecommerce.DisplayAction{
		Title: g.p.t(c, "commerce.offline.title"),
		Rows:  g.p.offlineInstructionRows(c, req.OrderRef, req.Amount.Amount, req.Amount.Currency),
	}, nil
}

// offlineInstructionRows builds the bank-transfer detail rows from store
// settings. Shared by the gateway (at checkout) and the order-received page (so
// the buyer can re-read the details), keeping both in sync.
func (p *Plugin) offlineInstructionRows(c *gin.Context, orderRef string, amount int64, currency string) []corecommerce.KV {
	rows := []corecommerce.KV{{Label: p.t(c, "commerce.common.order_number"), Value: orderRef}}
	add := func(label, key string) {
		if v := strings.TrimSpace(p.opt(key, "")); v != "" {
			rows = append(rows, corecommerce.KV{Label: label, Value: v})
		}
	}
	add(p.t(c, "commerce.offline.account_name"), "plugin_commerce_offline_account_name")
	add(p.t(c, "commerce.offline.bank_name"), "plugin_commerce_offline_bank_name")
	add(p.t(c, "commerce.offline.account_number"), "plugin_commerce_offline_account_number")
	rows = append(rows, corecommerce.KV{Label: p.t(c, "commerce.offline.amount_due"), Value: formatPrice(amount) + " " + currency})
	if extra := strings.TrimSpace(p.opt("plugin_commerce_offline_instructions", "")); extra != "" {
		rows = append(rows, corecommerce.KV{Label: p.t(c, "commerce.common.note"), Value: extra})
	}
	return rows
}

// Refund is a no-op: offline refunds are handled out-of-band and recorded by the
// admin. Capabilities reports Refund=false so commerce never calls this.
func (offlineGateway) Refund(*gin.Context, corecommerce.RefundRequest) error { return nil }

var _ corecommerce.PaymentGateway = offlineGateway{}
