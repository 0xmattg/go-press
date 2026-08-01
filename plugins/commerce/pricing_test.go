package commerce

import (
	"errors"
	"math"
	"testing"

	"go-press/core"
	"go-press/core/option"
)

func TestComputeTotals(t *testing.T) {
	cases := []struct {
		name                         string
		lines                        []pricingLine
		shipping, tax, discount      int64
		wantSub, wantDisc, wantGrand int64
	}{
		{
			name:     "subtotal + shipping + tax",
			lines:    []pricingLine{{UnitPrice: 1999, Qty: 2}, {UnitPrice: 500, Qty: 1}},
			shipping: 800, tax: 250, discount: 0,
			wantSub: 4498, wantDisc: 0, wantGrand: 5548,
		},
		{
			name:     "discount applied",
			lines:    []pricingLine{{UnitPrice: 1000, Qty: 3}},
			shipping: 0, tax: 0, discount: 500,
			wantSub: 3000, wantDisc: 500, wantGrand: 2500,
		},
		{
			name:     "discount capped at subtotal",
			lines:    []pricingLine{{UnitPrice: 1000, Qty: 1}},
			shipping: 0, tax: 0, discount: 5000,
			wantSub: 1000, wantDisc: 1000, wantGrand: 0,
		},
		{
			name:     "zero and negative qty ignored",
			lines:    []pricingLine{{UnitPrice: 1000, Qty: 1}, {UnitPrice: 999, Qty: 0}, {UnitPrice: 999, Qty: -3}},
			shipping: 0, tax: 0, discount: 0,
			wantSub: 1000, wantDisc: 0, wantGrand: 1000,
		},
		{
			name:     "empty cart",
			lines:    nil,
			shipping: 800, tax: 0, discount: 0,
			wantSub: 0, wantDisc: 0, wantGrand: 800,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := computeTotals(c.lines, c.shipping, c.tax, c.discount)
			if got.Subtotal != c.wantSub {
				t.Errorf("Subtotal = %d, want %d", got.Subtotal, c.wantSub)
			}
			if got.DiscountTotal != c.wantDisc {
				t.Errorf("DiscountTotal = %d, want %d", got.DiscountTotal, c.wantDisc)
			}
			if got.GrandTotal != c.wantGrand {
				t.Errorf("GrandTotal = %d, want %d", got.GrandTotal, c.wantGrand)
			}
		})
	}
}

func TestFlatShippingRejectsInvalidOrOverflowingConfiguration(t *testing.T) {
	for raw, wantErr := range map[string]bool{
		"12.50":                false,
		"":                     false,
		"-1.00":                true,
		"92233720368547758.08": true,
		"not-a-price":          true,
	} {
		p := &Plugin{engine: &core.Engine{Options: option.NewMemoryStore(map[string]string{
			"plugin_commerce_flat_shipping": raw,
		})}}
		got, err := p.flatShipping()
		if (err != nil) != wantErr {
			t.Errorf("flatShipping(%q) = %d, err=%v", raw, got, err)
		}
	}
}

func TestComputeTotalsCheckedRejectsOverflowAndNegativeAdjustments(t *testing.T) {
	for name, tc := range map[string]struct {
		lines                   []pricingLine
		shipping, tax, discount int64
	}{
		"line multiplication": {lines: []pricingLine{{UnitPrice: math.MaxInt64, Qty: 2}}},
		"subtotal addition":   {lines: []pricingLine{{UnitPrice: math.MaxInt64, Qty: 1}, {UnitPrice: 1, Qty: 1}}},
		"grand addition":      {lines: []pricingLine{{UnitPrice: math.MaxInt64, Qty: 1}}, shipping: 1},
		"negative shipping":   {lines: []pricingLine{{UnitPrice: 100, Qty: 1}}, shipping: -1},
		"negative tax":        {lines: []pricingLine{{UnitPrice: 100, Qty: 1}}, tax: -1},
		"negative discount":   {lines: []pricingLine{{UnitPrice: 100, Qty: 1}}, discount: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := computeTotalsChecked(tc.lines, tc.shipping, tc.tax, tc.discount); !errors.Is(err, ErrInvalidOrderTotals) {
				t.Fatalf("error = %v, want ErrInvalidOrderTotals", err)
			}
		})
	}
}

func TestStockShort(t *testing.T) {
	cases := []struct {
		managed     bool
		onHand, qty int
		want        bool
	}{
		{true, 5, 3, false},
		{true, 3, 3, false},
		{true, 2, 3, true},
		{true, 0, 1, true},
		{false, 0, 100, false}, // unmanaged = unlimited
	}
	for _, c := range cases {
		if got := stockShort(c.managed, c.onHand, c.qty); got != c.want {
			t.Errorf("stockShort(%v,%d,%d) = %v, want %v", c.managed, c.onHand, c.qty, got, c.want)
		}
	}
}
