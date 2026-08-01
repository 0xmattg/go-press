package commerce

import (
	"context"
	"errors"
	"testing"

	"go-press/core"
	corecommerce "go-press/core/commerce"
	"go-press/core/hook"
)

func TestSettleEvent(t *testing.T) {
	const total = 5000
	cases := []struct {
		status    corecommerce.SettleStatus
		amount    int64
		wantEvent string
		wantOK    bool
	}{
		{corecommerce.SettlePaid, total, EventPay, true},
		{corecommerce.SettleOverpaid, total + 100, EventPay, true},
		{corecommerce.SettleUnderpaid, total - 100, EventHold, true},
		{corecommerce.SettleExpired, 0, EventCancel, true},
		{corecommerce.SettleFailed, 0, EventFail, true},
		{corecommerce.SettleRefunded, total, EventRefund, true},            // full refund
		{corecommerce.SettleRefunded, total - 1, EventPartialRefund, true}, // partial
		{corecommerce.SettleStatus("weird"), 0, "", false},
	}
	for _, c := range cases {
		got, ok := settleEvent(c.status, c.amount, total)
		if ok != c.wantOK || got != c.wantEvent {
			t.Errorf("settleEvent(%q,%d,%d) = (%q,%v), want (%q,%v)",
				c.status, c.amount, total, got, ok, c.wantEvent, c.wantOK)
		}
	}
}

func TestValidateSettlementBindsGatewayCurrencyAndAmount(t *testing.T) {
	order := &Order{PaymentMethod: "paypal", Currency: "USD", GrandTotal: 5000}
	valid := corecommerce.SettleRequest{
		Gateway: "paypal", Status: corecommerce.SettlePaid, Amount: corecommerce.New(5000, "USD"),
	}
	if err := validateSettlement(order, valid); err != nil {
		t.Fatalf("valid paid settlement rejected: %v", err)
	}
	for name, mutate := range map[string]func(*corecommerce.SettleRequest){
		"gateway":  func(r *corecommerce.SettleRequest) { r.Gateway = "other" },
		"currency": func(r *corecommerce.SettleRequest) { r.Amount.Currency = "EUR" },
		"zero":     func(r *corecommerce.SettleRequest) { r.Amount.Amount = 0 },
		"amount":   func(r *corecommerce.SettleRequest) { r.Amount.Amount = 4999 },
	} {
		t.Run(name, func(t *testing.T) {
			req := valid
			mutate(&req)
			if err := validateSettlement(order, req); err == nil {
				t.Fatal("invalid settlement accepted")
			}
		})
	}
}

func TestLatePaidSettlementMovesClosedOrderToReconciliation(t *testing.T) {
	db := commerceTestDB(t)
	p := &Plugin{engine: &core.Engine{DB: db, Hooks: hook.New()}}
	order := Order{
		Number: "LATE-PAID", Status: OrderCancelled, PaymentMethod: "paypal",
		Currency: "USD", GrandTotal: 2500,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	req := corecommerce.SettleRequest{
		OrderRef: order.Number, Gateway: "paypal", TxnID: "capture-late",
		Status: corecommerce.SettlePaid, Amount: corecommerce.New(2500, "USD"),
		IdempotencyKey: "paypal:capture:capture-late",
	}
	if err := (orderSettler{p: p}).Settle(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&order, order.ID).Error; err != nil || order.Status != OrderReconciliation {
		t.Fatalf("late-paid order status = %q, err=%v", order.Status, err)
	}
	if err := (orderSettler{p: p}).Settle(context.Background(), req); err != nil {
		t.Fatalf("exact replay should dedupe: %v", err)
	}
	conflict := req
	conflict.TxnID = "different-capture"
	if err := (orderSettler{p: p}).Settle(context.Background(), conflict); !errors.Is(err, ErrSettlementIdempotencyConflict) {
		t.Fatalf("conflicting replay error = %v, want ErrSettlementIdempotencyConflict", err)
	}
}
