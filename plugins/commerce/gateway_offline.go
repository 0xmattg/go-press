package commerce

import (
	"errors"
	"strings"

	corecommerce "github.com/0xmattg/go-press/core/commerce"

	"github.com/gin-gonic/gin"
)

// offlineGatewayID is the built-in bank-transfer gateway. It has zero external
// dependencies, so a store can take orders as soon as the admin enters its bank
// details: the buyer is shown those details, the order goes on-hold, and an
// admin marks it paid once funds arrive (which calls the settler like any other
// gateway).
const offlineGatewayID = "offline"

// Option keys holding the bank-transfer details shown to the buyer.
const (
	optOfflineAccountName   = "plugin_commerce_offline_account_name"
	optOfflineBankName      = "plugin_commerce_offline_bank_name"
	optOfflineAccountNumber = "plugin_commerce_offline_account_number"
	optOfflineInstructions  = "plugin_commerce_offline_instructions"
)

// errOfflineNotConfigured is a local configuration failure raised before any
// remote side effect exists, so it is safe to mark as definitive.
var errOfflineNotConfigured = errors.New("commerce: bank transfer details are not configured")

// offlineGateway is the built-in manual bank-transfer method. StartPayment shows
// the store's bank details (from settings) as a DisplayAction; confirmation is
// the admin "mark paid" action on the order, not an automated callback.
type offlineGateway struct{ p *Plugin }

func (offlineGateway) ID() string { return offlineGatewayID }
func (g offlineGateway) Title(c *gin.Context) string {
	return g.p.t(c, "commerce.offline.title")
}
func (offlineGateway) Icon() string { return "🏦" }

// Available implements corecommerce.GatewayAvailability. Bank transfer is only
// offered once the buyer can actually pay with it, i.e. the payee account name
// and account number are both configured. Without the name or the number no
// transfer can be made in any banking system, whereas the bank name is
// optional: an IBAN or a domestic card/account number already identifies the
// bank, and routing/SWIFT codes go in the free-form instructions.
func (g offlineGateway) Available(*gin.Context) bool {
	return g.p != nil && g.p.offlineConfigured()
}

// offlineConfigured reports whether the bank-transfer details required by
// offlineGateway.Available are present. Whitespace-only values count as empty.
func (p *Plugin) offlineConfigured() bool {
	return strings.TrimSpace(p.opt(optOfflineAccountName, "")) != "" &&
		strings.TrimSpace(p.opt(optOfflineAccountNumber, "")) != ""
}

func (offlineGateway) Capabilities() corecommerce.Capabilities {
	// Offline refunds happen out-of-band (bank transfer back); the admin records
	// them manually, so we don't advertise an automated Refund call.
	return corecommerce.Capabilities{Refund: false, PartialRefund: false}
}

// StartPayment returns the bank-transfer instructions. The order is moved to
// on_hold by the checkout orchestration when it receives this DisplayAction.
// Checkout already filters on Available; the re-check covers an admin clearing
// the details between gateway selection and this call, so the order fails and
// its stock is released instead of awaiting a transfer nobody can make.
func (g offlineGateway) StartPayment(c *gin.Context, req corecommerce.PaymentRequest) (corecommerce.PaymentAction, error) {
	if !g.Available(c) {
		return nil, corecommerce.DefinitiveStartFailure(errOfflineNotConfigured)
	}
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
	add(p.t(c, "commerce.offline.account_name"), optOfflineAccountName)
	add(p.t(c, "commerce.offline.bank_name"), optOfflineBankName)
	add(p.t(c, "commerce.offline.account_number"), optOfflineAccountNumber)
	rows = append(rows, corecommerce.KV{Label: p.t(c, "commerce.offline.amount_due"), Value: formatPrice(amount) + " " + currency})
	if extra := strings.TrimSpace(p.opt(optOfflineInstructions, "")); extra != "" {
		rows = append(rows, corecommerce.KV{Label: p.t(c, "commerce.common.note"), Value: extra})
	}
	return rows
}

// Refund is a no-op: offline refunds are handled out-of-band and recorded by the
// admin. Capabilities reports Refund=false so commerce never calls this.
func (offlineGateway) Refund(*gin.Context, corecommerce.RefundRequest) error { return nil }

var (
	_ corecommerce.PaymentGateway      = offlineGateway{}
	_ corecommerce.GatewayAvailability = offlineGateway{}
)
