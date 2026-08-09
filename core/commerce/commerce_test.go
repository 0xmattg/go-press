package commerce

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/0xmattg/go-press/core/hook"

	"github.com/gin-gonic/gin"
)

func TestDefinitiveStartFailureMarkerSurvivesWrapping(t *testing.T) {
	cause := errors.New("configuration missing")
	marked := DefinitiveStartFailure(cause)
	if !IsDefinitiveStartFailure(marked) || !errors.Is(marked, cause) {
		t.Fatalf("marked error = %v, definitive=%v", marked, IsDefinitiveStartFailure(marked))
	}
	if !IsDefinitiveStartFailure(fmt.Errorf("gateway: %w", marked)) {
		t.Fatal("definitive marker was lost through wrapping")
	}
	if IsDefinitiveStartFailure(errors.New("timeout")) {
		t.Fatal("ordinary gateway errors must remain ambiguous")
	}
}

func TestMoneyArithmetic(t *testing.T) {
	a := New(199, "USD")
	if s, err := a.Add(New(1, "USD")); err != nil || s.Amount != 200 {
		t.Fatalf("Add = %v, %v", s, err)
	}
	if _, err := a.Add(New(1, "EUR")); err != ErrCurrencyMismatch {
		t.Fatalf("cross-currency Add should return ErrCurrencyMismatch, got %v", err)
	}
	if got := a.MulQty(3); got.Amount != 597 {
		t.Fatalf("MulQty(3) = %d, want 597", got.Amount)
	}
	if !New(0, "USD").IsZero() || New(-1, "USD").IsNegative() != true {
		t.Fatal("IsZero/IsNegative wrong")
	}
}

type stubGW struct{ id string }

func (s stubGW) ID() string               { return s.id }
func (stubGW) Title(*gin.Context) string  { return "Stub" }
func (stubGW) Icon() string               { return "" }
func (stubGW) Capabilities() Capabilities { return Capabilities{} }
func (stubGW) StartPayment(*gin.Context, PaymentRequest) (PaymentAction, error) {
	return CompletedAction{}, nil
}
func (stubGW) Refund(*gin.Context, RefundRequest) error { return nil }

type conditionalGW struct {
	stubGW
	available bool
}

func (g conditionalGW) Available(*gin.Context) bool { return g.available }

type currencyGW struct {
	stubGW
	currency string
}

func (g currencyGW) SupportsCurrency(currency string) bool {
	return g.currency == currency
}

func TestGatewayRegistry(t *testing.T) {
	bus := hook.New()
	if got := PaymentGateways(bus); len(got) != 0 {
		t.Fatalf("empty bus should have no gateways, got %d", len(got))
	}
	h := RegisterPaymentGateway(bus, stubGW{id: "a"})
	if got := PaymentGateways(bus); len(got) != 1 || got[0].ID() != "a" {
		t.Fatalf("after register: %v", got)
	}
	bus.RemoveFilter(h)
	if got := PaymentGateways(bus); len(got) != 0 {
		t.Fatalf("after remove should be empty, got %d", len(got))
	}
}

func TestAvailablePaymentGatewaysFiltersRuntimeUnavailable(t *testing.T) {
	bus := hook.New()
	RegisterPaymentGateway(bus, stubGW{id: "always"})
	RegisterPaymentGateway(bus, conditionalGW{stubGW: stubGW{id: "disabled"}, available: false})
	RegisterPaymentGateway(bus, conditionalGW{stubGW: stubGW{id: "ready"}, available: true})

	got := AvailablePaymentGateways(nil, bus)
	if len(got) != 2 || got[0].ID() != "always" || got[1].ID() != "ready" {
		t.Fatalf("available gateways = %v, want always + ready", got)
	}
}

func TestAvailablePaymentGatewaysFiltersCurrency(t *testing.T) {
	bus := hook.New()
	RegisterPaymentGateway(bus, stubGW{id: "agnostic"})
	RegisterPaymentGateway(bus, currencyGW{stubGW: stubGW{id: "usd"}, currency: "USD"})

	got := AvailablePaymentGatewaysForCurrency(nil, bus, "USD")
	if len(got) != 2 {
		t.Fatalf("USD gateways = %d, want 2", len(got))
	}
	got = AvailablePaymentGatewaysForCurrency(nil, bus, "CNY")
	if len(got) != 1 || got[0].ID() != "agnostic" {
		t.Fatalf("CNY gateways = %v, want agnostic only", got)
	}
}

type stubSettler struct{ called bool }

func (s *stubSettler) Settle(context.Context, SettleRequest) error { s.called = true; return nil }

func TestSettlerRoundTrip(t *testing.T) {
	bus := hook.New()
	if GetSettler(bus) != nil {
		t.Fatal("no settler set yet")
	}
	s := &stubSettler{}
	SetSettler(bus, s)
	got := GetSettler(bus)
	if got == nil {
		t.Fatal("settler not returned")
	}
	_ = got.Settle(context.Background(), SettleRequest{})
	if !s.called {
		t.Fatal("Settle not routed to the registered settler")
	}
}

type stubShip struct{ id string }

func (s stubShip) ID() string                                                { return s.id }
func (stubShip) Title(*gin.Context) string                                   { return "Flat" }
func (stubShip) CalculateRates(*gin.Context, ShipmentContext) []ShippingRate { return nil }

func TestShippingRegistry(t *testing.T) {
	bus := hook.New()
	h := RegisterShippingMethod(bus, stubShip{id: "flat"})
	if got := ShippingMethods(bus); len(got) != 1 || got[0].ID() != "flat" {
		t.Fatalf("after register: %v", got)
	}
	bus.RemoveFilter(h)
	if got := ShippingMethods(bus); len(got) != 0 {
		t.Fatalf("after remove should be empty, got %d", len(got))
	}
}

// Compile-time: the four actions form the sealed PaymentAction set.
var (
	_ PaymentAction = RedirectAction{}
	_ PaymentAction = DisplayAction{}
	_ PaymentAction = InlineAction{}
	_ PaymentAction = CompletedAction{}
)
