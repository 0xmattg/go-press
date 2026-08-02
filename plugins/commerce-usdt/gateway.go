package commerceusdt

import (
	"errors"
	"math/big"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	corecommerce "go-press/core/commerce"
)

// usdtGateway implements corecommerce.PaymentGateway (display-type / pull
// confirmation). It holds a back-pointer to the plugin only for config/DB/i18n
// access; all commerce interaction is through the core contracts.
type usdtGateway struct{ p *Plugin }

func (usdtGateway) ID() string                    { return gatewayID }
func (g usdtGateway) Title(c *gin.Context) string { return g.p.t(c, "commerce-usdt.title") }
func (usdtGateway) Icon() string                  { return "₮" }

func (usdtGateway) Capabilities() corecommerce.Capabilities {
	// On-chain refunds require spend authority (hot keys) and are out of scope;
	// refunds are recorded manually. Refund=false so commerce never calls Refund.
	return corecommerce.Capabilities{Refund: false, PartialRefund: false}
}

// Available implements corecommerce.GatewayAvailability.
func (g usdtGateway) Available(*gin.Context) bool {
	return g.p != nil && g.p.loadConfig().ready()
}

// StartPayment derives (or reuses) a per-order deposit address, records an
// invoice with the exact expected token amount, and returns a DisplayAction the
// storefront renders. The order is moved to on_hold by commerce; the background
// watcher confirms the on-chain payment and settles.
func (g usdtGateway) StartPayment(c *gin.Context, req corecommerce.PaymentRequest) (corecommerce.PaymentAction, error) {
	cfg := g.p.loadConfig()
	if !cfg.ready() {
		return nil, corecommerce.DefinitiveStartFailure(errors.New("commerce-usdt: gateway not configured"))
	}
	chain := g.p.buildChain(cfg)
	inv, err := g.p.invoiceFor(cfg, chain, req)
	if err != nil {
		// StartPayment performs no external side effect (only a local invoice), so
		// a failure here is safe to treat as definitive: commerce can release the
		// reservation and let the buyer retry or pick another method.
		return nil, corecommerce.DefinitiveStartFailure(err)
	}

	expected, _ := new(big.Int).SetString(inv.ExpectedToken, 10)
	if expected == nil {
		expected = big.NewInt(0)
	}
	symbol := chain.Token().Symbol
	expires := inv.ExpiresAt
	return corecommerce.DisplayAction{
		Title: g.p.t(c, "commerce-usdt.pay.title"),
		Rows: []corecommerce.KV{
			{Label: g.p.t(c, "commerce-usdt.pay.network"), Value: g.p.t(c, cfg.preset.NameKey)},
			{Label: g.p.t(c, "commerce-usdt.pay.asset"), Value: symbol},
			{Label: g.p.t(c, "commerce-usdt.pay.address"), Value: inv.Address},
			{Label: g.p.t(c, "commerce-usdt.pay.amount"), Value: formatToken(expected, inv.TokenDecimals) + " " + symbol},
		},
		QR:        chain.PaymentURI(inv.Address, expected),
		ExpiresAt: &expires,
	}, nil
}

// Refund is never called (Capabilities.Refund == false); it reports the manual
// path for safety if invoked directly.
func (usdtGateway) Refund(*gin.Context, corecommerce.RefundRequest) error {
	return errors.New("commerce-usdt: on-chain refunds must be processed manually")
}

// invoiceFor returns the invoice for a checkout, keyed by the stable checkout
// idempotency key so re-entering checkout reuses the same address and amount
// (never deriving a second address for the same order).
func (p *Plugin) invoiceFor(cfg config, chain *evmChain, req corecommerce.PaymentRequest) (*Invoice, error) {
	key := req.IdempotencyKey
	if key == "" {
		key = "start:" + req.OrderRef
	}
	if inv, err := p.findInvoiceByStartKey(key); err != nil {
		return nil, err
	} else if inv != nil {
		return inv, nil
	}

	expected := usdToToken(req.Amount.Amount, cfg.RateScaled, cfg.decimals)
	var inv *Invoice
	err := p.db.Transaction(func(tx *gorm.DB) error {
		var existing Invoice
		e := tx.Where("start_key = ?", key).First(&existing).Error
		if e == nil {
			inv = &existing
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		idx, err := p.allocHDIndex(tx, chain.ID())
		if err != nil {
			return err
		}
		addr, err := chain.DeriveAddress(idx)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		inv = &Invoice{
			OrderRef: req.OrderRef, StartKey: key, Chain: chain.ID(),
			HDIndex: idx, Address: addr,
			TokenContract: chain.Token().Contract, TokenDecimals: chain.Token().Decimals,
			ExpectedToken: expected.String(), ReceivedToken: "0",
			USDMinor: req.Amount.Amount, RateScaled: cfg.RateScaled,
			Status: invPending, CreatedAt: now,
			ExpiresAt: now.Add(time.Duration(cfg.WindowMinutes) * time.Minute),
		}
		return tx.Create(inv).Error
	})
	if err != nil {
		return nil, err
	}
	return inv, nil
}

var (
	_ corecommerce.PaymentGateway      = usdtGateway{}
	_ corecommerce.GatewayAvailability = usdtGateway{}
)
