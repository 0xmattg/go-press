package commerce

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// Order statuses. The happy path is pending → processing → completed; the rest
// are terminal/side branches.
const (
	OrderPending           = "pending"
	OrderProcessing        = "processing"
	OrderCompleted         = "completed"
	OrderOnHold            = "on_hold"
	OrderCancelled         = "cancelled"
	OrderFailed            = "failed"
	OrderRefunded          = "refunded"
	OrderPartiallyRefunded = "partially_refunded"
	OrderReconciliation    = "reconciliation"
)

// Order lifecycle events driving the state machine.
const (
	EventPay           = "pay"
	EventShip          = "ship"
	EventCancel        = "cancel"
	EventFail          = "fail"
	EventHold          = "hold"
	EventRefund        = "refund"
	EventPartialRefund = "partial_refund"
	EventReconcile     = "reconcile"
)

var (
	// ErrIllegalTransition is returned when an event is not valid for a status.
	ErrIllegalTransition = errors.New("commerce: illegal order transition")
	// ErrTransitionConflict means another transaction changed the order after
	// the caller read it. The optimistic status predicate prevents lost updates.
	ErrTransitionConflict = errors.New("commerce: concurrent order transition")
)

// orderTransitions is the pure transition table: from-status → event →
// to-status. Kept data-only so the state machine is unit-testable without a DB.
var orderTransitions = map[string]map[string]string{
	OrderPending: {
		EventPay:    OrderProcessing,
		EventHold:   OrderOnHold,
		EventCancel: OrderCancelled,
		EventFail:   OrderFailed,
	},
	OrderOnHold: {
		EventPay:    OrderProcessing,
		EventCancel: OrderCancelled,
		EventFail:   OrderFailed,
	},
	OrderProcessing: {
		EventShip:   OrderCompleted,
		EventCancel: OrderCancelled,
		EventRefund: OrderRefunded,
		// Partial refunds are a financial fact, not a fulfillment state.
		EventPartialRefund: OrderProcessing,
	},
	OrderCompleted: {
		EventRefund:        OrderRefunded,
		EventPartialRefund: OrderCompleted,
	},
	// Legacy orders may already carry this combined state. Keep them operable
	// while new partial refunds remain in Processing/Completed.
	OrderPartiallyRefunded: {
		EventRefund:        OrderRefunded,
		EventPartialRefund: OrderPartiallyRefunded,
		EventShip:          OrderCompleted,
	},
	OrderCancelled: {
		EventReconcile: OrderReconciliation,
	},
	OrderFailed: {
		EventReconcile: OrderReconciliation,
	},
	OrderReconciliation: {
		EventRefund:        OrderRefunded,
		EventPartialRefund: OrderReconciliation,
	},
}

// nextStatus resolves (from, event) to the destination status, reporting whether
// the transition is allowed. Pure function — the DB-free core of the machine.
func nextStatus(from, event string) (string, bool) {
	to, ok := orderTransitions[from][event]
	return to, ok
}

// OrderService owns the order state machine, notes, and status notifications.
// It is stateless and built per use from the plugin.
type OrderService struct{ p *Plugin }

func (p *Plugin) orders() *OrderService { return &OrderService{p: p} }

// OrderStatusChange is an immutable snapshot of a persisted transition. Callers
// publish it only after their surrounding transaction successfully commits.
// This prevents email/analytics hooks from observing data that later rolls back.
type OrderStatusChange struct {
	Order                Order
	OldStatus, NewStatus string
}

// Transition applies event to order within db (which may be a *gorm.DB or a
// transaction), persists the status and appends an audit note. It deliberately
// does not fire hooks: the caller must call PublishStatusChange only after the
// transaction has committed. Inventory remains orchestrated by the caller so it
// participates in the same transaction. Transition rejects illegal events.
func (s *OrderService) Transition(_ context.Context, db *gorm.DB, order *Order, event, author, note string) (*OrderStatusChange, error) {
	old := order.Status
	to, ok := nextStatus(old, event)
	if !ok {
		return nil, ErrIllegalTransition
	}

	updates := map[string]interface{}{"status": to, "updated_at": time.Now().UTC()}
	if to == OrderProcessing && order.PaidAt == nil {
		now := time.Now().UTC()
		updates["paid_at"] = now
		order.PaidAt = &now
	}
	result := db.Model(&Order{}).Where("id = ? AND status = ?", order.ID, old).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrTransitionConflict
	}
	order.Status = to

	logNote := note
	if logNote == "" {
		logNote = "状态：" + old + " → " + to
	}
	if err := db.Create(&OrderNote{
		OrderID: order.ID, Author: author, Note: logNote, CreatedAt: time.Now().UTC(),
	}).Error; err != nil {
		return nil, err
	}
	return &OrderStatusChange{Order: *order, OldStatus: old, NewStatus: to}, nil
}

// PublishStatusChange fires the post-commit status notification. Keeping this
// separate from Transition makes the transaction boundary explicit at every
// call site and avoids pre-commit emails on rollback.
func (s *OrderService) PublishStatusChange(ctx context.Context, change *OrderStatusChange) {
	if change == nil || change.OldStatus == change.NewStatus || s.p == nil || s.p.engine == nil || s.p.engine.Hooks == nil {
		return
	}
	order := change.Order
	s.p.engine.Hooks.DoAction(ctx, hookOrderStatusChanged, &order, change.OldStatus, change.NewStatus)
}

// AddNote appends a note to an order without changing status.
func (s *OrderService) AddNote(db *gorm.DB, orderID uint, author, note string, customer bool) error {
	return db.Create(&OrderNote{
		OrderID: orderID, Author: author, Note: note, IsCustomerNote: customer, CreatedAt: time.Now().UTC(),
	}).Error
}

// hookOrderStatusChanged fires on every order status transition with args
// (order *Order, oldStatus, newStatus string). Confirmation email / analytics
// listen here; the mapping stays decoupled from the machine.
const hookOrderStatusChanged = "commerce.order.status_changed"

// Formatted-money accessors for templates (mirrors CartLine.*Str), so views
// don't depend on a custom template FuncMap when rendered in the theme shell.
func (o Order) SubtotalStr() string      { return formatPrice(o.Subtotal) }
func (o Order) DiscountTotalStr() string { return formatPrice(o.DiscountTotal) }
func (o Order) ShippingTotalStr() string { return formatPrice(o.ShippingTotal) }
func (o Order) TaxTotalStr() string      { return formatPrice(o.TaxTotal) }
func (o Order) GrandTotalStr() string    { return formatPrice(o.GrandTotal) }

func (i OrderItem) UnitPriceStr() string { return formatPrice(i.UnitPrice) }
func (i OrderItem) LineTotalStr() string { return formatPrice(i.LineTotal) }
