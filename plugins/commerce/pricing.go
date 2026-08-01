package commerce

import (
	"errors"
	"math"
)

var ErrInvalidOrderTotals = errors.New("commerce: invalid or overflowing order totals")

// OrderTotals is the money breakdown for an order (all values in minor units of
// the order currency).
type OrderTotals struct {
	Subtotal      int64
	DiscountTotal int64
	ShippingTotal int64
	TaxTotal      int64
	GrandTotal    int64
}

// pricingLine is one priced line fed to computeTotals.
type pricingLine struct {
	UnitPrice int64
	Qty       int
}

// computeTotals sums line subtotals and folds in shipping, tax, and discount to
// produce the grand total. Pure and DB-free so it is unit-testable: grand =
// subtotal − discount + shipping + tax, floored at zero (a discount never yields
// a negative payable).
func computeTotals(lines []pricingLine, shipping, tax, discount int64) OrderTotals {
	totals, _ := computeTotalsChecked(lines, shipping, tax, discount)
	return totals
}

// computeTotalsChecked is the production pricing path. Every multiplication
// and addition is checked before it can wrap int64; negative adjustments are
// rejected rather than turning a malformed shipping/tax setting into a credit.
func computeTotalsChecked(lines []pricingLine, shipping, tax, discount int64) (OrderTotals, error) {
	if shipping < 0 || tax < 0 || discount < 0 {
		return OrderTotals{}, ErrInvalidOrderTotals
	}
	var subtotal int64
	for _, l := range lines {
		if l.Qty < 1 {
			continue
		}
		if l.UnitPrice < 0 || l.UnitPrice > math.MaxInt64/int64(l.Qty) {
			return OrderTotals{}, ErrInvalidOrderTotals
		}
		lineTotal := l.UnitPrice * int64(l.Qty)
		var err error
		subtotal, err = addMoneyChecked(subtotal, lineTotal)
		if err != nil {
			return OrderTotals{}, err
		}
	}
	if discount > subtotal {
		discount = subtotal // never discount below zero goods value
	}
	grand := subtotal - discount
	var err error
	grand, err = addMoneyChecked(grand, shipping)
	if err != nil {
		return OrderTotals{}, err
	}
	grand, err = addMoneyChecked(grand, tax)
	if err != nil {
		return OrderTotals{}, err
	}
	return OrderTotals{
		Subtotal:      subtotal,
		DiscountTotal: discount,
		ShippingTotal: shipping,
		TaxTotal:      tax,
		GrandTotal:    grand,
	}, nil
}

func addMoneyChecked(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, ErrInvalidOrderTotals
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, ErrInvalidOrderTotals
	}
	return a + b, nil
}
