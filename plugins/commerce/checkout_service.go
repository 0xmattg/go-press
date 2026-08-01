package commerce

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	corecommerce "go-press/core/commerce"
	"go-press/core/user"
	"go-press/pkg/logger"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// hookCheckoutValidate lets extensions veto a checkout. Filter value is a
// []string of error messages; a non-empty result aborts the order.
const hookCheckoutValidate = "commerce.checkout.validate"

const (
	checkoutKeyBytes           = 32
	paymentReconciliationState = "reconciliation"
)

var (
	// ErrEmptyCart is returned when checkout is attempted with no items.
	ErrEmptyCart = errors.New("commerce: cart is empty")
	// ErrNoGateway is returned when the chosen payment method isn't registered.
	ErrNoGateway = errors.New("commerce: unknown payment method")
	// ErrInvalidPaymentAction is returned when a gateway violates the action
	// contract. Because a remote payment may already exist, it is reconciled.
	ErrInvalidPaymentAction = errors.New("commerce: invalid payment action")
	// ErrInvalidCheckoutKey rejects missing, malformed, or untrusted checkout
	// submissions before any cart, inventory, order, or gateway side effect.
	ErrInvalidCheckoutKey = errors.New("commerce: invalid checkout key")
	// ErrCheckoutAttemptClosed means an old one-time key belongs to a failed or
	// cancelled order. The retained cart may be retried only with a fresh form.
	ErrCheckoutAttemptClosed = errors.New("commerce: checkout attempt is closed")
	// ErrCheckoutInProgress tells a replay that the elected request has created
	// the order but has not yet durably recorded the gateway's next action.
	ErrCheckoutInProgress = errors.New("commerce: checkout is in progress")
)

// CheckoutInput carries the submitted checkout form.
type CheckoutInput struct {
	Email         string
	Billing       corecommerce.Address
	Shipping      corecommerce.Address
	ShipToBilling bool
	PaymentMethod string
	CheckoutKey   string
}

// CheckoutService orchestrates order placement. It is stateless, built per use.
type CheckoutService struct{ p *Plugin }

func (p *Plugin) checkout() *CheckoutService { return &CheckoutService{p: p} }

// PlaceOrder converts the current cart into an order in a single transaction:
// it snapshots line items, reserves stock under row locks (rolling back on
// oversell), and records a pending payment intent. After commit it clears the
// cart and asks the selected gateway what happens next (StartPayment), advancing
// the order to on_hold for display-type methods. Returns the order and the
// gateway's PaymentAction for the storefront to render.
func (s *CheckoutService) PlaceOrder(c *gin.Context, in CheckoutInput) (*Order, corecommerce.PaymentAction, error) {
	if !validCheckoutKey(in.CheckoutKey) {
		return nil, nil, ErrInvalidCheckoutKey
	}
	cartView := s.p.cart().View(c)
	if existing, err := s.findCheckoutOrder(in.CheckoutKey); err != nil {
		return nil, nil, err
	} else if existing != nil {
		if existing.CheckoutCartID == nil || *existing.CheckoutCartID != cartView.cartID {
			return nil, nil, ErrInvalidCheckoutKey
		}
		return s.reuseCheckoutOrder(existing)
	}
	if cartView.Empty {
		return nil, nil, ErrEmptyCart
	}
	if !checkoutKeysEqual(cartView.checkoutKey, in.CheckoutKey) {
		return nil, nil, ErrInvalidCheckoutKey
	}

	if verrs := s.validate(cartView, in); len(verrs) > 0 {
		return nil, nil, fmt.Errorf("commerce: checkout validation failed: %s", strings.Join(verrs, "; "))
	}

	gateway := s.gatewayByID(c, in.PaymentMethod)
	if gateway == nil {
		return nil, nil, ErrNoGateway
	}

	currency := cartView.Currency
	lines := make([]pricingLine, 0, len(cartView.Lines))
	for _, l := range cartView.Lines {
		lines = append(lines, pricingLine{UnitPrice: l.UnitPrice, Qty: l.Qty})
	}
	shipping, err := s.p.flatShipping()
	if err != nil {
		return nil, nil, err
	}
	totals, err := computeTotalsChecked(lines, shipping, 0, 0)
	if err != nil {
		return nil, nil, err
	}

	number := newOrderNumber()
	checkoutKey := in.CheckoutKey
	checkoutCartID := cartView.cartID
	order := &Order{
		Number: number, AccessKey: newAccessKey(), Status: OrderPending, Email: in.Email, Currency: currency,
		CheckoutKey: &checkoutKey, CheckoutCartID: &checkoutCartID,
		Subtotal: totals.Subtotal, DiscountTotal: totals.DiscountTotal,
		ShippingTotal: totals.ShippingTotal, TaxTotal: totals.TaxTotal, GrandTotal: totals.GrandTotal,
		PaymentMethod: gateway.ID(),
	}
	if u := user.CurrentUser(c); u != nil {
		uid := u.ID
		order.UserID = &uid
		if order.Email == "" && u.Email != nil {
			order.Email = *u.Email
		}
	}

	err = s.p.engine.DB.Transaction(func(tx *gorm.DB) error {
		// Serialize checkout with every cart mutation and revalidate the active
		// key inside the write transaction. A snapshot read before an Add/Set/
		// Remove cannot create an order after that mutation invalidates its key.
		var lockedCart Cart
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedCart, cartView.cartID).Error; err != nil {
			return ErrInvalidCheckoutKey
		}
		if lockedCart.CheckoutKey == nil || !checkoutKeysEqual(*lockedCart.CheckoutKey, in.CheckoutKey) {
			return ErrInvalidCheckoutKey
		}
		checkoutTime := time.Now().UTC()
		lockedLines := append([]CartLine(nil), cartView.Lines...)
		sort.Slice(lockedLines, func(i, j int) bool { return lockedLines[i].ProductID < lockedLines[j].ProductID })
		for _, line := range lockedLines {
			if err := s.p.cart().validateCheckoutLine(tx, line, cartView.Currency, checkoutTime); err != nil {
				return err
			}
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}
		for _, l := range cartView.Lines {
			if err := tx.Create(&OrderItem{
				OrderID: order.ID, ProductContentID: l.ProductID, NameSnapshot: l.Title,
				UnitPrice: l.UnitPrice, Qty: l.Qty, LineSubtotal: l.LineTotal, LineTotal: l.LineTotal,
			}).Error; err != nil {
				return err
			}
			// Reserve under a row lock; oversell rolls the whole order back.
			if err := s.p.inventory().Reserve(tx, l.ProductID, l.Qty, order.ID); err != nil {
				return err
			}
		}
		ship := in.Shipping
		if in.ShipToBilling {
			ship = in.Billing
		}
		if err := tx.Create(orderAddress(order.ID, "billing", in.Billing)).Error; err != nil {
			return err
		}
		if err := tx.Create(orderAddress(order.ID, "shipping", ship)).Error; err != nil {
			return err
		}
		return tx.Create(&Payment{
			OrderID: order.ID, Gateway: gateway.ID(), Status: OrderPending,
			Amount: totals.GrandTotal, Currency: currency,
			IdempotencyKey: "start:" + number, CreatedAt: time.Now().UTC(),
		}).Error
	})
	if err != nil {
		// A competing request using the same one-time key may have committed
		// first. The unique index elects that request as the sole creator; after
		// our transaction rolls back, return its order without touching the
		// gateway. Looking up after any create-transaction error is safe: a row
		// with this globally random key can only represent this checkout attempt.
		if existing, lookupErr := s.findCheckoutOrder(in.CheckoutKey); lookupErr == nil && existing != nil &&
			existing.CheckoutCartID != nil && *existing.CheckoutCartID == cartView.cartID {
			return s.reuseCheckoutOrder(existing)
		}
		return nil, nil, err
	}

	base := strings.TrimRight(s.p.siteURL(), "/")
	action, err := gateway.StartPayment(c, corecommerce.PaymentRequest{
		OrderRef:       number,
		Amount:         corecommerce.New(totals.GrandTotal, currency),
		Customer:       in.Billing,
		IdempotencyKey: "start:" + number,
		// Include the access key so a guest returning from a redirect gateway
		// (PayPal) can view their order-received page without an account.
		ReturnURL: base + "/checkout/complete/" + number + "?key=" + order.AccessKey,
		CancelURL: base + "/checkout",
	})
	if err != nil {
		if corecommerce.IsDefinitiveStartFailure(err) {
			return s.handleDefinitiveStartFailure(c.Request.Context(), cartView, order, err)
		}
		return s.handleAmbiguousStartFailure(c.Request.Context(), cartView, order, err)
	}
	if err := s.persistPaymentAction(order, action); err != nil {
		// The gateway call completed but its response could not be durably
		// accepted. The remote side may nevertheless have created or captured a
		// payment, so releasing inventory would permit a duplicate charge.
		return s.handleAmbiguousStartFailure(c.Request.Context(), cartView, order, err)
	}

	if err := s.postProcess(c, order, gateway.ID(), totals.GrandTotal, currency, action); err != nil {
		// A failure after StartPayment is always ambiguous: even a redirect/display
		// response may correspond to a remote payment object already created.
		if _, completed := action.(corecommerce.CompletedAction); completed {
			// The gateway already reported funds settled. Never expose the original
			// cart as retryable: place the order on reconciliation hold (best effort),
			// consume this snapshot, and route the buyer to the order page.
			if reconciliationErr := s.recordCompletedSettlementFailure(c.Request.Context(), order, err); reconciliationErr != nil {
				logger.Error("commerce: completed payment reconciliation record failed", "order", order.Number, "error", reconciliationErr)
			}
			s.consumeCartSnapshot(cartView, order)
			return order, action, nil
		}
		return s.handleAmbiguousStartFailure(c.Request.Context(), cartView, order, err)
	}
	// Consume only the quantities captured by this checkout snapshot. A cart may
	// have changed in another tab while the gateway was starting; those changes
	// must survive. Once payment start + post-processing succeeded the order is
	// authoritative, so a cleanup failure is logged rather than reported as a
	// checkout failure that could induce a duplicate order/payment retry.
	s.consumeCartSnapshot(cartView, order)
	return order, action, nil
}

func (s *CheckoutService) handleDefinitiveStartFailure(ctx context.Context, cartView CartView, order *Order, startErr error) (*Order, corecommerce.PaymentAction, error) {
	advanced, compensationErr := s.compensateStartFailure(ctx, order, startErr)
	if compensationErr != nil {
		return order, nil, fmt.Errorf("%w（补偿失败：%v）", startErr, compensationErr)
	}
	if advanced {
		s.consumeCartSnapshot(cartView, order)
		return order, corecommerce.CompletedAction{}, nil
	}
	if invalidateErr := invalidateCartCheckoutKey(s.p.engine.DB, cartView.cartID, cartView.checkoutKey); invalidateErr != nil {
		return order, nil, fmt.Errorf("%w（结算令牌失效失败：%v）", startErr, invalidateErr)
	}
	return order, nil, startErr
}

// handleAmbiguousStartFailure is the conservative path for timeouts, transport
// errors, and lost responses. The remote side may already have succeeded, so
// releasing inventory and exposing the cart would permit a second charge. Keep
// the order/payment for reconciliation, consume the submitted snapshot, and
// let a late webhook advance the still-pending order normally.
func (s *CheckoutService) handleAmbiguousStartFailure(ctx context.Context, cartView CartView, order *Order, startErr error) (*Order, corecommerce.PaymentAction, error) {
	if err := s.recordAmbiguousStartFailure(ctx, order, startErr); err != nil {
		logger.Error("commerce: ambiguous payment start reconciliation record failed", "order", order.Number, "error", err)
	}
	s.consumeCartSnapshot(cartView, order)
	return order, corecommerce.CompletedAction{}, nil
}

func (s *CheckoutService) recordAmbiguousStartFailure(ctx context.Context, order *Order, startErr error) error {
	return s.p.engine.DB.Transaction(func(tx *gorm.DB) error {
		var fresh Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fresh, "id = ?", order.ID).Error; err != nil {
			return err
		}
		*order = fresh
		if fresh.Status != OrderPending {
			return nil // a webhook already resolved the ambiguity
		}
		result := tx.Model(&Payment{}).
			Where("order_id = ? AND idempotency_key = ?", fresh.ID, "start:"+fresh.Number).
			Updates(map[string]interface{}{
				"status": paymentReconciliationState,
				"raw": marshalRaw(map[string]any{
					"reconciliation": true,
					"start_error":    startErr.Error(),
				}),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("commerce: payment intent missing")
		}
		return tx.Create(&OrderNote{
			OrderID: fresh.ID, Author: "system",
			Note:      "支付网关返回结果不确定，已保留库存并等待网关确认",
			CreatedAt: time.Now().UTC(),
		}).Error
	})
}

// findCheckoutOrder returns the order claimed by a checkout idempotency key.
// Callers validate the key first; nil means this one-time form has not yet
// created an order.
func (s *CheckoutService) findCheckoutOrder(key string) (*Order, error) {
	var order Order
	err := s.p.engine.DB.Where("checkout_key = ?", key).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// reuseCheckoutOrder is the replay result for an already-claimed form. Failed
// and cancelled attempts deliberately require a fresh key. A pending order is
// only replayable after the winning request durably records the gateway action;
// until then callers receive ErrCheckoutInProgress instead of a fake completed
// action that could strand a hosted-redirect payment.
func (s *CheckoutService) reuseCheckoutOrder(order *Order) (*Order, corecommerce.PaymentAction, error) {
	if order == nil {
		return nil, nil, ErrInvalidCheckoutKey
	}
	switch order.Status {
	case OrderFailed, OrderCancelled:
		return order, nil, ErrCheckoutAttemptClosed
	case OrderPending:
		action, ok, reconciling, err := s.persistedPaymentAction(order)
		if err != nil {
			return order, nil, err
		}
		if !ok {
			if reconciling {
				return order, corecommerce.CompletedAction{}, nil
			}
			return order, nil, ErrCheckoutInProgress
		}
		return order, action, nil
	default:
		return order, corecommerce.CompletedAction{}, nil
	}
}

type storedCheckoutAction struct {
	Kind       string            `json:"kind"`
	URL        string            `json:"url,omitempty"`
	Title      string            `json:"title,omitempty"`
	Rows       []corecommerce.KV `json:"rows,omitempty"`
	QR         string            `json:"qr,omitempty"`
	ExpiresAt  *time.Time        `json:"expires_at,omitempty"`
	ClientData map[string]any    `json:"client_data,omitempty"`
}

// persistPaymentAction validates and stores the gateway's next action before
// the winning request exposes it. Replays can then return the exact redirect or
// inline payload without invoking StartPayment a second time.
func (s *CheckoutService) persistPaymentAction(order *Order, action corecommerce.PaymentAction) error {
	record := storedCheckoutAction{}
	switch typed := action.(type) {
	case corecommerce.RedirectAction:
		record.Kind = "redirect"
		record.URL = strings.TrimSpace(typed.URL)
		if record.URL == "" {
			return ErrInvalidPaymentAction
		}
	case corecommerce.InlineAction:
		if len(typed.ClientData) == 0 {
			return ErrInvalidPaymentAction
		}
		record.Kind = "inline"
		record.ClientData = typed.ClientData
	case corecommerce.DisplayAction:
		if strings.TrimSpace(typed.Title) == "" && len(typed.Rows) == 0 && strings.TrimSpace(typed.QR) == "" {
			return ErrInvalidPaymentAction
		}
		record.Kind = "display"
		record.Title = strings.TrimSpace(typed.Title)
		record.Rows = typed.Rows
		record.QR = strings.TrimSpace(typed.QR)
		record.ExpiresAt = typed.ExpiresAt
	case corecommerce.CompletedAction:
		record.Kind = "completed"
	default:
		return ErrInvalidPaymentAction
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("commerce: encode payment action: %w", err)
	}
	result := s.p.engine.DB.Model(&Payment{}).
		Where("order_id = ? AND idempotency_key = ?", order.ID, "start:"+order.Number).
		Update("raw", string(raw))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("commerce: payment intent missing")
	}
	return nil
}

func (s *CheckoutService) persistedPaymentAction(order *Order) (corecommerce.PaymentAction, bool, bool, error) {
	var payment Payment
	err := s.p.engine.DB.Where("order_id = ? AND idempotency_key = ?", order.ID, "start:"+order.Number).First(&payment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, false, nil
	}
	if err != nil {
		return nil, false, false, err
	}
	if payment.Status == paymentReconciliationState {
		return nil, false, true, nil
	}
	var record storedCheckoutAction
	if payment.Raw == "" || json.Unmarshal([]byte(payment.Raw), &record) != nil {
		return nil, false, false, nil
	}
	switch record.Kind {
	case "redirect":
		if strings.TrimSpace(record.URL) == "" {
			return nil, false, false, nil
		}
		return corecommerce.RedirectAction{URL: record.URL}, true, false, nil
	case "inline":
		if len(record.ClientData) == 0 {
			return nil, false, false, nil
		}
		return corecommerce.InlineAction{ClientData: record.ClientData}, true, false, nil
	case "display":
		if record.Title == "" && len(record.Rows) == 0 && record.QR == "" {
			return nil, false, false, nil
		}
		return corecommerce.DisplayAction{
			Title: record.Title, Rows: record.Rows, QR: record.QR, ExpiresAt: record.ExpiresAt,
		}, true, false, nil
	case "completed":
		return corecommerce.CompletedAction{}, true, false, nil
	default:
		return nil, false, false, nil
	}
}

func (s *CheckoutService) recordCompletedSettlementFailure(ctx context.Context, order *Order, settleErr error) error {
	var change *OrderStatusChange
	err := s.p.engine.DB.Transaction(func(tx *gorm.DB) error {
		var fresh Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fresh, "id = ?", order.ID).Error; err != nil {
			return err
		}
		*order = fresh
		if fresh.Status != OrderPending {
			return nil
		}
		var err error
		change, err = s.p.orders().Transition(ctx, tx, &fresh, EventHold, "system", "支付网关已报告完成，但本地结算失败，等待人工核对")
		if err != nil {
			return err
		}
		*order = fresh
		return tx.Model(&Payment{}).
			Where("order_id = ? AND idempotency_key = ?", fresh.ID, "start:"+fresh.Number).
			Updates(map[string]interface{}{
				"status": "reconciliation",
				"raw":    marshalRaw(map[string]any{"settlement_error": settleErr.Error()}),
			}).Error
	})
	if err != nil {
		return err
	}
	s.p.orders().PublishStatusChange(ctx, change)
	return nil
}

func (s *CheckoutService) consumeCartSnapshot(cartView CartView, order *Order) {
	if err := s.p.cart().consumeSnapshot(cartView); err != nil {
		logger.Error("commerce: checkout cart snapshot cleanup failed", "order", order.Number, "error", err)
	}
}

// compensateStartFailure marks a still-pending order failed and releases every
// reservation in one transaction. The order row lock and status check make the
// operation idempotent and prevent a late settlement from racing the release.
func (s *CheckoutService) compensateStartFailure(ctx context.Context, order *Order, startErr error) (bool, error) {
	var change *OrderStatusChange
	advanced := false
	err := s.p.engine.DB.Transaction(func(tx *gorm.DB) error {
		var fresh Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fresh, "id = ?", order.ID).Error; err != nil {
			return err
		}
		if fresh.Status != OrderPending {
			*order = fresh
			advanced = paymentStartAdvanced(fresh.Status)
			return nil
		}
		var err error
		change, err = s.p.orders().Transition(ctx, tx, &fresh, EventFail, "system", "支付启动失败，已释放库存")
		if err != nil {
			return err
		}
		var items []OrderItem
		if err := tx.Where("order_id = ?", fresh.ID).Find(&items).Error; err != nil {
			return err
		}
		for _, item := range items {
			if err := s.p.inventory().Release(tx, item.ProductContentID, item.Qty, fresh.ID); err != nil {
				return err
			}
		}
		return tx.Model(&Payment{}).
			Where("order_id = ? AND idempotency_key = ?", fresh.ID, "start:"+fresh.Number).
			Updates(map[string]interface{}{
				"status": OrderFailed,
				"raw":    marshalRaw(map[string]any{"error": startErr.Error()}),
			}).Error
	})
	if err != nil {
		return false, err
	}
	s.p.orders().PublishStatusChange(ctx, change)
	if change != nil {
		*order = change.Order
	}
	return advanced, nil
}

func paymentStartAdvanced(status string) bool {
	switch status {
	case OrderOnHold, OrderProcessing, OrderCompleted, OrderPartiallyRefunded, OrderRefunded, OrderReconciliation:
		return true
	default:
		return false
	}
}

// postProcess advances the order based on the gateway's action: display-type
// (offline/crypto) methods await payment → on_hold; a synchronous CompletedAction
// settles immediately; redirect/inline leave the order pending until the gateway
// confirms out of band.
func (s *CheckoutService) postProcess(c *gin.Context, order *Order, gatewayID string, grand int64, currency string, action corecommerce.PaymentAction) error {
	ctx := c.Request.Context()
	switch typed := action.(type) {
	case corecommerce.DisplayAction:
		var change *OrderStatusChange
		err := s.p.engine.DB.Transaction(func(tx *gorm.DB) error {
			var err error
			change, err = s.p.orders().Transition(ctx, tx, order, EventHold, "system", "等待付款")
			return err
		})
		if err != nil {
			return err
		}
		s.p.orders().PublishStatusChange(ctx, change)
	case corecommerce.CompletedAction:
		if settler := corecommerce.GetSettler(s.p.engine.Hooks); settler != nil {
			return settler.Settle(ctx, corecommerce.SettleRequest{
				OrderRef: order.Number, Gateway: gatewayID, Status: corecommerce.SettlePaid,
				Amount: corecommerce.New(grand, currency), IdempotencyKey: "complete:" + order.Number,
			})
		}
		return errors.New("commerce: payment settler unavailable")
	case corecommerce.RedirectAction:
		if strings.TrimSpace(typed.URL) == "" {
			return ErrInvalidPaymentAction
		}
	case corecommerce.InlineAction:
		if len(typed.ClientData) == 0 {
			return ErrInvalidPaymentAction
		}
	default:
		return ErrInvalidPaymentAction
	}
	return nil
}

// validate runs the checkout.validate filter and enforces baseline requirements.
func (s *CheckoutService) validate(cart CartView, in CheckoutInput) []string {
	var errs []string
	if strings.TrimSpace(in.Email) == "" {
		errs = append(errs, "缺少邮箱")
	}
	if strings.TrimSpace(in.Billing.Name) == "" {
		errs = append(errs, "缺少收货人姓名")
	}
	if s.p.engine != nil && s.p.engine.Hooks != nil {
		if v := s.p.engine.Hooks.ApplyFilter(hookCheckoutValidate, errs, cart, in); v != nil {
			if next, ok := v.([]string); ok {
				errs = next
			}
		}
	}
	return errs
}

// gatewayByID finds a registered gateway by its ID (empty picks the first).
func (s *CheckoutService) gatewayByID(c *gin.Context, id string) corecommerce.PaymentGateway {
	gateways := corecommerce.AvailablePaymentGateways(c, s.p.engine.Hooks)
	if len(gateways) == 0 {
		return nil
	}
	if id == "" {
		return gateways[0]
	}
	for _, g := range gateways {
		if g.ID() == id {
			return g
		}
	}
	return nil
}

// registeredGatewayByID is for post-payment operations on historical orders.
// Unlike checkout selection it does not hide a disabled gateway: invoking its
// refund method should surface the concrete configuration error and mark the
// local attempt failed instead of silently recording a manual refund.
func (s *CheckoutService) registeredGatewayByID(id string) corecommerce.PaymentGateway {
	for _, g := range corecommerce.PaymentGateways(s.p.engine.Hooks) {
		if g != nil && g.ID() == id {
			return g
		}
	}
	return nil
}

// flatShipping returns the configured flat shipping cost in minor units (0 when
// unset). P2 ships a single flat rate; zones/methods are a later phase.
func (p *Plugin) flatShipping() (int64, error) {
	amount, err := parsePriceStrict(p.opt("plugin_commerce_flat_shipping", ""))
	if err != nil {
		return 0, fmt.Errorf("%w: flat shipping", ErrInvalidOrderTotals)
	}
	return amount, nil
}

func (p *Plugin) siteURL() string {
	if p.engine != nil && p.engine.Config != nil {
		return p.engine.Config.Site.URL
	}
	return ""
}

func orderAddress(orderID uint, kind string, a corecommerce.Address) *OrderAddress {
	return &OrderAddress{
		OrderID: orderID, Type: kind,
		Name: a.Name, Company: a.Company, Line1: a.Line1, Line2: a.Line2, City: a.City,
		State: a.State, Country: a.Country, Postcode: a.Postcode, Phone: a.Phone, Email: a.Email,
	}
}

func newOrderNumber() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102") + "-" + strings.ToUpper(hex.EncodeToString(b))
}

// newAccessKey returns a 256-bit random token used to authorize viewing a
// guest order without an account (see Order.AccessKey).
func newAccessKey() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// newCheckoutKey generates the 256-bit one-time token rendered into a checkout
// form. Unlike legacy display identifiers, entropy failures are surfaced and
// checkout rendering stops instead of issuing a predictable fallback.
func newCheckoutKey() (string, error) {
	b := make([]byte, checkoutKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validCheckoutKey(key string) bool {
	if len(key) != hex.EncodedLen(checkoutKeyBytes) {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

func checkoutKeysEqual(left, right string) bool {
	if len(left) != hex.EncodedLen(checkoutKeyBytes) || len(right) != len(left) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
