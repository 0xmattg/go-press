package commerce

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/0xmattg/go-press/core"
	corecommerce "github.com/0xmattg/go-press/core/commerce"
	"github.com/0xmattg/go-press/core/hook"

	"github.com/gin-gonic/gin"
)

type testReconcilerGateway struct {
	pending []corecommerce.PendingPayment
}

func (*testReconcilerGateway) ID() string                { return "pull-test" }
func (*testReconcilerGateway) Title(*gin.Context) string { return "Pull Test" }
func (*testReconcilerGateway) Icon() string              { return "" }
func (*testReconcilerGateway) Capabilities() corecommerce.Capabilities {
	return corecommerce.Capabilities{}
}
func (*testReconcilerGateway) StartPayment(*gin.Context, corecommerce.PaymentRequest) (corecommerce.PaymentAction, error) {
	return corecommerce.CompletedAction{}, nil
}
func (*testReconcilerGateway) Refund(*gin.Context, corecommerce.RefundRequest) error { return nil }
func (g *testReconcilerGateway) ReconcilePending(_ context.Context, pending []corecommerce.PendingPayment) []corecommerce.SettleRequest {
	g.pending = append([]corecommerce.PendingPayment(nil), pending...)
	if len(pending) == 0 {
		return nil
	}
	return []corecommerce.SettleRequest{{
		OrderRef: pending[0].OrderRef, Status: corecommerce.SettlePaid,
		Amount: pending[0].Amount, IdempotencyKey: "reconcile:" + pending[0].OrderRef,
	}}
}

func TestReconcilePendingPaymentsUsesGatewayContractAndSettler(t *testing.T) {
	db := commerceTestDB(t)
	bus := hook.New()
	p := &Plugin{engine: &core.Engine{DB: db, Hooks: bus}}
	gateway := &testReconcilerGateway{}
	corecommerce.RegisterPaymentGateway(bus, gateway)
	corecommerce.SetSettler(bus, orderSettler{p: p})

	order := Order{Number: "RECONCILE-1", Status: OrderPending, PaymentMethod: gateway.ID(), Currency: "USD", GrandTotal: 2599}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(storedCheckoutAction{Kind: "inline", ClientData: map[string]any{"payment_id": "remote-1"}})
	if err != nil {
		t.Fatal(err)
	}
	payment := Payment{
		OrderID: order.ID, Gateway: gateway.ID(), Status: OrderPending, Amount: order.GrandTotal,
		Currency: order.Currency, IdempotencyKey: "start:" + order.Number, Raw: string(raw),
	}
	if err := db.Create(&payment).Error; err != nil {
		t.Fatal(err)
	}

	if err := p.reconcilePendingPayments(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(gateway.pending) != 1 || gateway.pending[0].Context["payment_id"] != "remote-1" {
		t.Fatalf("pending handoff = %#v", gateway.pending)
	}
	var saved Order
	if err := db.First(&saved, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Status != OrderProcessing {
		t.Fatalf("order status = %q, want %q", saved.Status, OrderProcessing)
	}
}

func TestDisplayPaymentActionRoundTripsThroughPersistence(t *testing.T) {
	db := commerceTestDB(t)
	p := &Plugin{engine: &core.Engine{DB: db}}
	order := Order{Number: "DISPLAY-1", Status: OrderPending, Currency: "USD", GrandTotal: 1000}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&Payment{
		OrderID: order.ID, Gateway: "display-test", Status: OrderPending, Amount: 1000,
		Currency: "USD", IdempotencyKey: "start:" + order.Number,
	}).Error; err != nil {
		t.Fatal(err)
	}
	want := corecommerce.DisplayAction{
		Title: "Pay by transfer",
		Rows:  []corecommerce.KV{{Label: "Reference", Value: order.Number}},
		QR:    "pay:display-1",
	}
	if err := p.checkout().persistPaymentAction(&order, want); err != nil {
		t.Fatal(err)
	}
	got, ok, reconciling, err := p.checkout().persistedPaymentAction(&order)
	if err != nil || !ok || reconciling {
		t.Fatalf("persisted action = %#v, ok=%v reconciling=%v err=%v", got, ok, reconciling, err)
	}
	display, ok := got.(corecommerce.DisplayAction)
	if !ok || display.Title != want.Title || display.QR != want.QR || len(display.Rows) != 1 {
		t.Fatalf("display action = %#v", got)
	}
}
