package commerce

import (
	"context"
	"errors"
	"testing"

	"github.com/0xmattg/go-press/core"
	corecommerce "github.com/0xmattg/go-press/core/commerce"
	"github.com/0xmattg/go-press/core/hook"

	"gorm.io/gorm"
)

// TestNextStatus_Legal covers every allowed transition end to end.
func TestNextStatus_Legal(t *testing.T) {
	cases := []struct {
		from, event, want string
	}{
		{OrderPending, EventPay, OrderProcessing},
		{OrderPending, EventHold, OrderOnHold},
		{OrderPending, EventCancel, OrderCancelled},
		{OrderPending, EventFail, OrderFailed},
		{OrderOnHold, EventPay, OrderProcessing},
		{OrderOnHold, EventCancel, OrderCancelled},
		{OrderOnHold, EventFail, OrderFailed},
		{OrderProcessing, EventShip, OrderCompleted},
		{OrderProcessing, EventCancel, OrderCancelled},
		{OrderProcessing, EventRefund, OrderRefunded},
		{OrderProcessing, EventPartialRefund, OrderProcessing},
		{OrderCompleted, EventRefund, OrderRefunded},
		{OrderCompleted, EventPartialRefund, OrderCompleted},
		{OrderPartiallyRefunded, EventRefund, OrderRefunded},
		{OrderPartiallyRefunded, EventShip, OrderCompleted},
	}
	for _, c := range cases {
		got, ok := nextStatus(c.from, c.event)
		if !ok || got != c.want {
			t.Errorf("nextStatus(%q,%q) = (%q,%v), want (%q,true)", c.from, c.event, got, ok, c.want)
		}
	}
}

// TestNextStatus_Illegal rejects transitions that must not happen.
func TestNextStatus_Illegal(t *testing.T) {
	cases := []struct{ from, event string }{
		{OrderPending, EventShip},        // can't ship before paying
		{OrderPending, EventRefund},      // can't refund an unpaid order
		{OrderCompleted, EventCancel},    // shipped orders aren't cancelled
		{OrderCompleted, EventPay},       // already paid
		{OrderCancelled, EventPay},       // terminal
		{OrderRefunded, EventRefund},     // terminal
		{OrderFailed, EventShip},         // terminal
		{OrderProcessing, EventPay},      // already processing
		{OrderProcessing, EventHold},     // no re-hold after payment
		{"", EventPay},                   // unknown status
		{OrderPending, "nonsense_event"}, // unknown event
	}
	for _, c := range cases {
		if to, ok := nextStatus(c.from, c.event); ok {
			t.Errorf("nextStatus(%q,%q) = (%q,true), want illegal", c.from, c.event, to)
		}
	}
}

func TestTransitionRollbackDoesNotPublishHook(t *testing.T) {
	db := commerceTestDB(t)
	bus := hook.New()
	p := &Plugin{engine: &core.Engine{DB: db, Hooks: bus}}
	order := Order{Number: "ROLLBACK-1", Status: OrderPending, Currency: "USD", GrandTotal: 1000}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	called := 0
	bus.AddAction(hookOrderStatusChanged, func(_ context.Context, _ ...interface{}) { called++ }, 10)

	var rolledBack *OrderStatusChange
	wantRollback := errors.New("force rollback")
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		rolledBack, err = p.orders().Transition(context.Background(), tx, &order, EventHold, "test", "")
		if err != nil {
			return err
		}
		return wantRollback
	})
	if !errors.Is(err, wantRollback) {
		t.Fatalf("transaction error = %v, want rollback", err)
	}
	if rolledBack == nil {
		t.Fatal("transition should return a change snapshot inside transaction")
	}
	if called != 0 {
		t.Fatalf("hook fired before/after rollback: %d", called)
	}
	var saved Order
	if err := db.First(&saved, order.ID).Error; err != nil || saved.Status != OrderPending {
		t.Fatalf("rolled-back status = %q, err=%v; want pending", saved.Status, err)
	}

	// A committed caller explicitly publishes exactly once.
	order.Status = OrderPending
	var committed *OrderStatusChange
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		committed, err = p.orders().Transition(context.Background(), tx, &order, EventHold, "test", "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	p.orders().PublishStatusChange(context.Background(), committed)
	if called != 1 {
		t.Fatalf("post-commit hook count = %d, want 1", called)
	}
}

func TestTransitionRejectsStaleStatusWithoutLostUpdate(t *testing.T) {
	db := commerceTestDB(t)
	p := &Plugin{engine: &core.Engine{DB: db, Hooks: hook.New()}}
	stale := Order{Number: "STALE-1", Status: OrderPending, Currency: "USD", GrandTotal: 1000}
	if err := db.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Order{}).Where("id = ?", stale.ID).Update("status", OrderProcessing).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := p.orders().Transition(context.Background(), db, &stale, EventHold, "test", ""); !errors.Is(err, ErrTransitionConflict) {
		t.Fatalf("stale transition error = %v, want ErrTransitionConflict", err)
	}
	var saved Order
	if err := db.First(&saved, stale.ID).Error; err != nil || saved.Status != OrderProcessing {
		t.Fatalf("status after stale transition = %q, err=%v; want processing", saved.Status, err)
	}
	var notes int64
	if err := db.Model(&OrderNote{}).Where("order_id = ?", stale.ID).Count(&notes).Error; err != nil || notes != 0 {
		t.Fatalf("stale transition notes = %d, err=%v; want none", notes, err)
	}
}

func TestRefundCapacityReservationAndCumulativeStatus(t *testing.T) {
	db := commerceTestDB(t)
	bus := hook.New()
	p := &Plugin{engine: &core.Engine{DB: db, Hooks: bus}}
	order := Order{Number: "REFUND-1", Status: OrderCompleted, Currency: "USD", GrandTotal: 10000}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	payment := Payment{
		OrderID: order.ID, Gateway: "test", TxnID: "capture-1", Status: string(corecommerce.SettlePaid),
		Amount: order.GrandTotal, Currency: order.Currency, IdempotencyKey: "paid:refund-1",
	}
	if err := db.Create(&payment).Error; err != nil {
		t.Fatal(err)
	}
	statusHooks := 0
	bus.AddAction(hookOrderStatusChanged, func(_ context.Context, _ ...interface{}) { statusHooks++ }, 10)

	first, err := p.prepareRefund(order.ID, 6000, "first", "refund:first", true)
	if err != nil {
		t.Fatal(err)
	}
	// The first gateway request is still in flight. Its pending row reserves
	// capacity, so another concurrent phase-one request cannot over-refund.
	if _, err := p.prepareRefund(order.ID, 5000, "too much", "refund:concurrent", true); !errors.Is(err, ErrRefundExceedsRemaining) {
		t.Fatalf("concurrent over-refund error = %v, want ErrRefundExceedsRemaining", err)
	}
	// A network error is ambiguous: keep its amount reserved. A new key cannot
	// double-spend that capacity, while the original key can retry safely.
	if err := p.failRefund(first.Refund.ID, errors.New("timeout after send")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.prepareRefund(order.ID, 5000, "new key", "refund:after-timeout", true); !errors.Is(err, ErrRefundExceedsRemaining) {
		t.Fatalf("new key after ambiguous failure = %v, want reserved capacity rejection", err)
	}
	first, err = p.prepareRefund(order.ID, 6000, "first", "refund:first", true)
	if err != nil || first.Refund.Status != RefundPending {
		t.Fatalf("same-key retry = %+v, err=%v; want pending retry", first, err)
	}
	if err := p.completeRefund(context.Background(), first.Refund.ID, corecommerce.RefundResult{TransactionID: "remote-1"}, "admin"); err != nil {
		t.Fatal(err)
	}
	var afterFirst Order
	if err := db.First(&afterFirst, order.ID).Error; err != nil || afterFirst.Status != OrderCompleted {
		t.Fatalf("status after first refund = %q, err=%v", afterFirst.Status, err)
	}

	second, err := p.prepareRefund(order.ID, 4000, "rest", "refund:second", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.completeRefund(context.Background(), second.Refund.ID, corecommerce.RefundResult{TransactionID: "remote-2"}, "admin"); err != nil {
		t.Fatal(err)
	}
	var afterSecond Order
	if err := db.First(&afterSecond, order.ID).Error; err != nil || afterSecond.Status != OrderRefunded {
		t.Fatalf("status after cumulative full refund = %q, err=%v", afterSecond.Status, err)
	}
	total, err := successfulRefundTotal(db, order.ID)
	if err != nil || total != order.GrandTotal {
		t.Fatalf("successful refund total = %d, err=%v; want %d", total, err, order.GrandTotal)
	}
	// Replaying the same submitted key after the order became terminal is a
	// successful no-op and does not fire a second transition.
	if repeat, err := p.prepareRefund(order.ID, 4000, "rest", "refund:second", true); err != nil || repeat.Refund.Status != RefundSucceeded {
		t.Fatalf("idempotent replay = %+v, err=%v", repeat, err)
	}
	if statusHooks != 1 {
		t.Fatalf("status hooks = %d, want only the real completed→refunded transition", statusHooks)
	}
}

func TestRefundRequiresRegisteredOriginalGateway(t *testing.T) {
	if err := validateRefundGateway(nil); !errors.Is(err, ErrRefundGatewayUnavailable) {
		t.Fatalf("nil gateway error = %v, want ErrRefundGatewayUnavailable", err)
	}
	// A concrete Refund=false gateway is an explicit manual/offline workflow.
	if err := validateRefundGateway(&failingStartGateway{}); err != nil {
		t.Fatalf("registered manual gateway should be accepted: %v", err)
	}
	if err := validateRefundGateway(&legacyAutoRefundGateway{}); !errors.Is(err, ErrRefundIdempotencyRequired) {
		t.Fatalf("legacy automatic gateway error = %v, want ErrRefundIdempotencyRequired", err)
	}
	empty := &emptyResultRefundGateway{}
	if err := validateRefundGateway(empty); err != nil {
		t.Fatalf("idempotent gateway contract rejected: %v", err)
	}
	if err := validateAutomaticRefundResult(empty, corecommerce.RefundResult{}); !errors.Is(err, ErrRefundTransactionMissing) {
		t.Fatalf("empty automatic refund result = %v, want ErrRefundTransactionMissing", err)
	}
}

func TestCompletedRemoteRefundPersistsWhenOrderStatusCannotTransition(t *testing.T) {
	db := commerceTestDB(t)
	bus := hook.New()
	p := &Plugin{engine: &core.Engine{DB: db, Hooks: bus}}
	order := Order{Number: "REFUND-RACE", Status: OrderCancelled, Currency: "USD", GrandTotal: 1000}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	payment := Payment{OrderID: order.ID, Gateway: "test", Status: string(corecommerce.SettlePaid), Amount: 1000, Currency: "USD", IdempotencyKey: "paid:race"}
	if err := db.Create(&payment).Error; err != nil {
		t.Fatal(err)
	}
	key := "refund:race"
	refund := Refund{
		OrderID: order.ID, PaymentID: payment.ID, Amount: 1000, Currency: "USD", Status: RefundPending,
		IdempotencyKey: &key,
	}
	if err := db.Create(&refund).Error; err != nil {
		t.Fatal(err)
	}
	statusHooks := 0
	bus.AddAction(hookOrderStatusChanged, func(_ context.Context, _ ...interface{}) { statusHooks++ }, 10)
	if err := p.completeRefund(context.Background(), refund.ID, corecommerce.RefundResult{TransactionID: "REMOTE-RACE"}, "admin"); !errors.Is(err, ErrRefundStatusSync) {
		t.Fatalf("status sync error = %v, want ErrRefundStatusSync", err)
	}
	var savedRefund Refund
	if err := db.First(&savedRefund, refund.ID).Error; err != nil || savedRefund.Status != RefundSucceeded || savedRefund.GatewayRefundID != "REMOTE-RACE" {
		t.Fatalf("remote refund fact = %+v, err=%v", savedRefund, err)
	}
	var savedOrder Order
	if err := db.First(&savedOrder, order.ID).Error; err != nil || savedOrder.Status != OrderCancelled {
		t.Fatalf("order status = %q, err=%v; want cancelled preserved", savedOrder.Status, err)
	}
	var notes int64
	if err := db.Model(&OrderNote{}).Where("order_id = ?", order.ID).Count(&notes).Error; err != nil || notes != 1 {
		t.Fatalf("reconciliation notes = %d, err=%v; want 1", notes, err)
	}
	if statusHooks != 0 {
		t.Fatalf("status hooks = %d, want none without a transition", statusHooks)
	}
}

func TestAdminRefundAndWebhookCorrelateToSingleRefund(t *testing.T) {
	db := commerceTestDB(t)
	bus := hook.New()
	p := &Plugin{engine: &core.Engine{DB: db, Hooks: bus}}
	order := Order{Number: "REFUND-WEBHOOK", Status: OrderCompleted, PaymentMethod: "paypal", Currency: "USD", GrandTotal: 1000}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	payment := Payment{
		OrderID: order.ID, Gateway: "paypal", TxnID: "capture-1", Status: string(corecommerce.SettlePaid),
		Amount: 1000, Currency: "USD", IdempotencyKey: "paid:webhook",
	}
	if err := db.Create(&payment).Error; err != nil {
		t.Fatal(err)
	}
	key := "refund:admin"
	refund := Refund{
		OrderID: order.ID, PaymentID: payment.ID, Amount: 1000, Currency: "USD", Status: RefundPending,
		IdempotencyKey: &key,
	}
	if err := db.Create(&refund).Error; err != nil {
		t.Fatal(err)
	}
	if err := p.completeRefund(context.Background(), refund.ID, corecommerce.RefundResult{TransactionID: "PAYPAL-REF-1"}, "admin"); err != nil {
		t.Fatal(err)
	}
	settler := orderSettler{p: p}
	if err := settler.Settle(context.Background(), corecommerce.SettleRequest{
		OrderRef: order.Number, Gateway: "paypal", TxnID: "PAYPAL-REF-1",
		Amount: corecommerce.New(1000, "USD"), Status: corecommerce.SettleRefunded,
		IdempotencyKey: "paypal:refund:PAYPAL-REF-1",
	}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&Refund{}).Where("order_id = ?", order.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("refund rows after webhook = %d, err=%v; want exactly 1", count, err)
	}
}

func TestEqualPendingRefundsDoNotGuessWebhookCorrelation(t *testing.T) {
	db := commerceTestDB(t)
	bus := hook.New()
	p := &Plugin{engine: &core.Engine{DB: db, Hooks: bus}}
	order := Order{Number: "REFUND-AMBIGUOUS", Status: OrderCompleted, PaymentMethod: "paypal", Currency: "USD", GrandTotal: 2000}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	payment := Payment{
		OrderID: order.ID, Gateway: "paypal", TxnID: "capture-ambiguous", Status: string(corecommerce.SettlePaid),
		Amount: 2000, Currency: "USD", IdempotencyKey: "paid:ambiguous",
	}
	if err := db.Create(&payment).Error; err != nil {
		t.Fatal(err)
	}
	for i, key := range []string{"refund:first-equal", "refund:second-equal"} {
		status := RefundPending
		if i == 0 {
			// A timeout after sending is result-ambiguous and still reserves
			// capacity; it must participate in webhook correlation just like a
			// currently pending request.
			status = RefundFailed
		}
		candidate := Refund{
			OrderID: order.ID, PaymentID: payment.ID, Amount: 500, Currency: "USD",
			Status: status, Gateway: "paypal", IdempotencyKey: &key,
		}
		if err := db.Create(&candidate).Error; err != nil {
			t.Fatal(err)
		}
	}

	settler := orderSettler{p: p}
	req := corecommerce.SettleRequest{
		OrderRef: order.Number, Gateway: "paypal", TxnID: "REMOTE-EQUAL-2",
		Amount: corecommerce.New(500, "USD"), Status: corecommerce.SettleRefunded,
		IdempotencyKey: "paypal:refund:REMOTE-EQUAL-2",
	}
	if err := settler.Settle(context.Background(), req); !errors.Is(err, ErrRefundCorrelationAmbiguous) {
		t.Fatalf("ambiguous webhook error = %v, want ErrRefundCorrelationAmbiguous", err)
	}
	var succeeded int64
	if err := db.Model(&Refund{}).Where("order_id = ? AND status = ?", order.ID, RefundSucceeded).Count(&succeeded).Error; err != nil || succeeded != 0 {
		t.Fatalf("succeeded refunds after ambiguous webhook = %d, err=%v; want 0", succeeded, err)
	}

	// Once the initiating admin request stores the provider id, a webhook retry
	// correlates by that id and cannot create or claim a second equal row.
	var second Refund
	if err := db.Where("idempotency_key = ?", "refund:second-equal").First(&second).Error; err != nil {
		t.Fatal(err)
	}
	if err := p.completeRefund(context.Background(), second.ID,
		corecommerce.RefundResult{TransactionID: "REMOTE-EQUAL-2"}, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := settler.Settle(context.Background(), req); err != nil {
		t.Fatalf("correlated webhook retry: %v", err)
	}
	total, err := successfulRefundTotal(db, order.ID)
	if err != nil || total != 500 {
		t.Fatalf("successful refund total = %d, err=%v; want 500", total, err)
	}
}

func TestProviderRefundIDCannotCountForTwoLocalAttempts(t *testing.T) {
	db := commerceTestDB(t)
	p := &Plugin{engine: &core.Engine{DB: db, Hooks: hook.New()}}
	order := Order{Number: "REFUND-REMOTE-UNIQUE", Status: OrderCompleted, Currency: "USD", GrandTotal: 2000}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	payment := Payment{OrderID: order.ID, Gateway: "test", TxnID: "capture-unique", Status: string(corecommerce.SettlePaid), Amount: 2000, Currency: "USD", IdempotencyKey: "paid:unique"}
	if err := db.Create(&payment).Error; err != nil {
		t.Fatal(err)
	}
	keys := []string{"refund:unique-a", "refund:unique-b"}
	rows := make([]Refund, 2)
	for i := range rows {
		rows[i] = Refund{OrderID: order.ID, PaymentID: payment.ID, Amount: 500, Currency: "USD", Status: RefundPending, IdempotencyKey: &keys[i]}
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	result := corecommerce.RefundResult{TransactionID: "REMOTE-SINGLE"}
	if err := p.completeRefund(context.Background(), rows[0].ID, result, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := p.completeRefund(context.Background(), rows[1].ID, result, "admin"); !errors.Is(err, ErrRefundIdempotencyConflict) {
		t.Fatalf("duplicate provider id error = %v, want ErrRefundIdempotencyConflict", err)
	}
	total, err := successfulRefundTotal(db, order.ID)
	if err != nil || total != 500 {
		t.Fatalf("successful refund total = %d, err=%v; want 500", total, err)
	}

	// Provider ids are scoped by gateway; another gateway may legitimately use
	// the same short transaction id without colliding with the first one.
	otherOrder := Order{Number: "REFUND-REMOTE-OTHER-GATEWAY", Status: OrderCompleted, Currency: "USD", GrandTotal: 500}
	if err := db.Create(&otherOrder).Error; err != nil {
		t.Fatal(err)
	}
	otherPayment := Payment{OrderID: otherOrder.ID, Gateway: "other-gateway", TxnID: "capture-other", Status: string(corecommerce.SettlePaid), Amount: 500, Currency: "USD", IdempotencyKey: "paid:other-gateway"}
	if err := db.Create(&otherPayment).Error; err != nil {
		t.Fatal(err)
	}
	otherKey := "refund:other-gateway"
	otherRefund := Refund{OrderID: otherOrder.ID, PaymentID: otherPayment.ID, Amount: 500, Currency: "USD", Status: RefundPending, Gateway: "other-gateway", IdempotencyKey: &otherKey}
	if err := db.Create(&otherRefund).Error; err != nil {
		t.Fatal(err)
	}
	if err := p.completeRefund(context.Background(), otherRefund.ID, result, "admin"); err != nil {
		t.Fatalf("same provider id in another gateway should be allowed: %v", err)
	}
}
