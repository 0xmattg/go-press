package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	corecommerce "go-press/core/commerce"
	"go-press/pkg/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// orderSettler is the real PaymentSettler (replacing the P0 stub). Every payment
// confirmation — offline "mark paid", a PayPal webhook, a crypto reconcile —
// funnels through Settle, which is idempotent (deduped by SettleRequest
// IdempotencyKey) and the single place order status + inventory advance.
type orderSettler struct{ p *Plugin }

var ErrSettlementIdempotencyConflict = errors.New("commerce: settlement idempotency payload conflict")

// settleEvent maps a gateway settlement outcome to an order event. Pure and
// unit-testable. Refund resolves to full/partial by comparing the refunded
// amount to the order total.
func settleEvent(status corecommerce.SettleStatus, amount, orderTotal int64) (string, bool) {
	switch status {
	case corecommerce.SettlePaid, corecommerce.SettleOverpaid:
		return EventPay, true
	case corecommerce.SettleUnderpaid:
		return EventHold, true
	case corecommerce.SettleExpired:
		return EventCancel, true
	case corecommerce.SettleFailed:
		return EventFail, true
	case corecommerce.SettleRefunded:
		if amount >= orderTotal {
			return EventRefund, true
		}
		return EventPartialRefund, true
	}
	return "", false
}

// Settle applies a gateway settlement to an order in a single transaction:
// dedup by idempotency key, update/record the payment, advance the order state
// machine, and apply inventory side effects (commit on pay, release on
// cancel/fail). Safe to call repeatedly with the same IdempotencyKey.
func (s orderSettler) Settle(ctx context.Context, req corecommerce.SettleRequest) error {
	if s.p == nil || s.p.engine == nil || s.p.engine.DB == nil {
		return errors.New("commerce: settler not wired")
	}
	if req.IdempotencyKey == "" {
		return errors.New("commerce: settle requires an idempotency key")
	}
	var change *OrderStatusChange
	err := s.p.engine.DB.Transaction(func(tx *gorm.DB) error {
		var order Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("number = ?", req.OrderRef).First(&order).Error; err != nil {
			return err
		}
		if err := validateSettlement(&order, req); err != nil {
			return err
		}

		// Dedup: this exact settlement event was already recorded.
		var dup Payment
		if err := tx.Where("idempotency_key = ?", req.IdempotencyKey).First(&dup).Error; err == nil {
			if dup.OrderID != order.ID || dup.Gateway != req.Gateway || dup.TxnID != req.TxnID ||
				dup.Status != string(req.Status) || dup.Amount != req.Amount.Amount ||
				!strings.EqualFold(dup.Currency, req.Amount.Currency) {
				return ErrSettlementIdempotencyConflict
			}
			logger.Info("commerce: settle deduped", "order", req.OrderRef, "key", req.IdempotencyKey)
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		// Record the settlement onto the pending payment intent (or a fresh row).
		var pay Payment
		if err := tx.Where("order_id = ? AND status IN ?", order.ID, []string{OrderPending, paymentReconciliationState}).
			Order("id desc").First(&pay).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			pay = Payment{OrderID: order.ID, CreatedAt: time.Now().UTC()}
		} else if err != nil {
			return err
		}
		pay.Gateway = req.Gateway
		pay.TxnID = req.TxnID
		pay.Status = string(req.Status)
		pay.Amount = req.Amount.Amount
		pay.Currency = req.Amount.Currency
		pay.IdempotencyKey = req.IdempotencyKey
		pay.Raw = marshalRaw(req.Raw)
		if err := tx.Save(&pay).Error; err != nil {
			return err
		}

		event, ok := settleEvent(req.Status, req.Amount.Amount, order.GrandTotal)
		if !ok {
			return nil // unknown status — payment recorded, no auto transition
		}
		if event == EventRefund || event == EventPartialRefund {
			var err error
			event, err = s.recordGatewayRefund(tx, &order, &pay, req)
			if err != nil {
				return err
			}
			if event == EventPartialRefund {
				// Partial refund rows are the financial source of truth; fulfillment
				// remains processing/completed and no no-op status hook is emitted.
				return nil
			}
		}
		if settlementCarriesFunds(req.Status) && (order.Status == OrderCancelled || order.Status == OrderFailed) {
			// A supposedly closed order received money after inventory was released.
			// Preserve the payment fact, move the order to an explicit reconciliation
			// state, and never pretend the released reservation is still fulfillable.
			var err error
			change, err = s.p.orders().Transition(ctx, tx, &order, EventReconcile,
				"gateway:"+req.Gateway, "订单关闭后收到付款，需人工核对履约或退款")
			return err
		}
		if _, allowed := nextStatus(order.Status, event); !allowed {
			// Order already advanced past this point; recording the payment is enough.
			return nil
		}
		var err error
		change, err = s.p.orders().Transition(ctx, tx, &order, event, "gateway:"+req.Gateway, "")
		if err != nil {
			return err
		}

		switch event {
		case EventPay:
			if err := s.applyInventory(tx, order.ID, s.p.inventory().Commit); err != nil {
				return err
			}
		case EventCancel, EventFail:
			if err := s.applyInventory(tx, order.ID, s.p.inventory().Release); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.p.orders().PublishStatusChange(ctx, change)
	return nil
}

func settlementCarriesFunds(status corecommerce.SettleStatus) bool {
	return status == corecommerce.SettlePaid || status == corecommerce.SettleUnderpaid || status == corecommerce.SettleOverpaid
}

// validateSettlement binds a gateway report to the order it is allowed to
// mutate and rejects impossible money/status combinations before any payment
// row or state transition is written.
func validateSettlement(order *Order, req corecommerce.SettleRequest) error {
	if order == nil || strings.TrimSpace(req.Gateway) == "" || req.Gateway != order.PaymentMethod {
		return errors.New("commerce: settlement gateway does not match order")
	}
	requireMoney := func() error {
		if req.Amount.Amount <= 0 || !strings.EqualFold(strings.TrimSpace(req.Amount.Currency), strings.TrimSpace(order.Currency)) {
			return errors.New("commerce: settlement money does not match order currency")
		}
		return nil
	}
	switch req.Status {
	case corecommerce.SettlePaid:
		if err := requireMoney(); err != nil {
			return err
		}
		if req.Amount.Amount != order.GrandTotal {
			return fmt.Errorf("commerce: paid amount %d does not equal order total %d", req.Amount.Amount, order.GrandTotal)
		}
	case corecommerce.SettleOverpaid:
		if err := requireMoney(); err != nil {
			return err
		}
		if req.Amount.Amount <= order.GrandTotal {
			return errors.New("commerce: overpaid settlement is not over order total")
		}
	case corecommerce.SettleUnderpaid:
		// A positive token transfer can be worth less than one order-currency
		// minor unit. Preserve that fact as a zero-minor underpayment; the exact
		// provider amount remains in Raw and the order stays on hold.
		if req.Amount.Amount < 0 || !strings.EqualFold(strings.TrimSpace(req.Amount.Currency), strings.TrimSpace(order.Currency)) {
			return errors.New("commerce: underpaid settlement money does not match order currency")
		}
		if req.Amount.Amount >= order.GrandTotal {
			return errors.New("commerce: underpaid settlement is not below order total")
		}
	case corecommerce.SettleRefunded:
		if err := requireMoney(); err != nil {
			return err
		}
		if strings.TrimSpace(req.TxnID) == "" {
			return errors.New("commerce: refunded settlement requires a provider refund id")
		}
		return nil
	}
	return nil
}

// recordGatewayRefund records a provider-confirmed refund and returns the order
// event derived from the cumulative succeeded total. It runs with the order row
// locked by Settle, serializing it with admin refunds.
func (s orderSettler) recordGatewayRefund(tx *gorm.DB, order *Order, pay *Payment, req corecommerce.SettleRequest) (string, error) {
	if req.Amount.Amount <= 0 {
		return "", errors.New("commerce: invalid gateway refund amount")
	}
	// A provider id is the durable identity of the money movement. Look it up
	// before amount-based correlation so an admin response and a later webhook
	// converge on one local fact even when their event idempotency keys differ.
	var existing Refund
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("gateway = ? AND gateway_refund_id = ?", req.Gateway, req.TxnID).First(&existing).Error
	if err == nil {
		if existing.OrderID != order.ID || existing.Amount != req.Amount.Amount ||
			!strings.EqualFold(existing.Currency, req.Amount.Currency) {
			return "", ErrRefundIdempotencyConflict
		}
		already, totalErr := successfulRefundTotal(tx, order.ID)
		if totalErr != nil {
			return "", totalErr
		}
		return refundEvent(already, order.GrandTotal), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}

	already, err := successfulRefundTotal(tx, order.ID)
	if err != nil {
		return "", err
	}
	if req.Amount.Amount > order.GrandTotal-already {
		return "", errors.New("commerce: gateway refund exceeds remaining order total")
	}

	// A webhook can race the admin request immediately after PayPal replies but
	// before Commerce stores the provider refund id. Claim the matching pending
	// capacity reservation rather than inserting a duplicate row.
	var candidates []Refund
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("order_id = ? AND amount = ? AND currency = ? AND status IN ? AND gateway_refund_id = '' AND (gateway = ? OR gateway = '')",
			order.ID, req.Amount.Amount, req.Amount.Currency, []string{RefundPending, RefundFailed}, req.Gateway).
		Order("id asc").Limit(2).Find(&candidates).Error
	if err != nil {
		return "", err
	}
	if len(candidates) > 1 {
		// Two equal in-flight refunds cannot be distinguished by amount. Ask the
		// provider to retry after the initiating request has stored its remote id
		// instead of attaching the webhook to an arbitrary row.
		return "", ErrRefundCorrelationAmbiguous
	}
	var refund Refund
	if len(candidates) == 0 {
		key := req.IdempotencyKey
		refund = Refund{
			OrderID: order.ID, PaymentID: pay.ID, Amount: req.Amount.Amount,
			Currency: req.Amount.Currency, Reason: "gateway", Status: RefundSucceeded, Gateway: req.Gateway,
			IdempotencyKey: &key, GatewayRefundID: req.TxnID, Raw: marshalRaw(req.Raw),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := tx.Create(&refund).Error; err != nil {
			return "", err
		}
	} else {
		refund = candidates[0]
		if err := tx.Model(&refund).Updates(map[string]interface{}{
			"status": RefundSucceeded, "gateway": req.Gateway, "gateway_refund_id": req.TxnID,
			"raw": marshalRaw(req.Raw), "error": "", "updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return "", err
		}
	}

	return refundEvent(already+req.Amount.Amount, order.GrandTotal), nil
}

func refundEvent(refunded, orderTotal int64) string {
	if refunded >= orderTotal {
		return EventRefund
	}
	return EventPartialRefund
}

// successfulRefundTotal includes legacy rows with an empty status so schema
// migration does not make earlier refunds disappear from the cumulative limit.
func successfulRefundTotal(db *gorm.DB, orderID uint) (int64, error) {
	var total int64
	err := db.Model(&Refund{}).Where("order_id = ? AND (status = ? OR status = '' OR status IS NULL)", orderID, RefundSucceeded).
		Select("COALESCE(SUM(amount), 0)").Scan(&total).Error
	return total, err
}

// reservedRefundTotal includes both in-flight and failed/unknown attempts. A
// gateway error may mean the provider completed the refund but its response was
// lost, so capacity is released only by retrying the same idempotency row.
func reservedRefundTotal(db *gorm.DB, orderID uint, excludeID uint) (int64, error) {
	var total int64
	q := db.Model(&Refund{}).Where("order_id = ? AND status IN ?", orderID, []string{RefundPending, RefundFailed})
	if excludeID != 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Select("COALESCE(SUM(amount), 0)").Scan(&total).Error
	return total, err
}

// applyInventory runs fn(productID, qty, orderID) for every line of the order,
// used to commit or release reservations transactionally with settlement.
func (s orderSettler) applyInventory(tx *gorm.DB, orderID uint, fn func(*gorm.DB, uint, int, uint) error) error {
	var items []OrderItem
	if err := tx.Where("order_id = ?", orderID).Find(&items).Error; err != nil {
		return err
	}
	for _, it := range items {
		if err := fn(tx, it.ProductContentID, it.Qty, orderID); err != nil {
			return err
		}
	}
	return nil
}

// marshalRaw serializes a gateway's opaque payload for the payments.raw column.
func marshalRaw(raw map[string]any) string {
	if len(raw) == 0 {
		return ""
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(b)
}

var _ corecommerce.PaymentSettler = orderSettler{}
