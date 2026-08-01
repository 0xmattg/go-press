package commerce

import (
	"errors"
	"fmt"
)

// Money is an exact monetary amount in a single currency, stored as an integer
// number of the currency's minor units (e.g. cents) — never a float. All
// commerce arithmetic uses Money to avoid binary-float rounding drift.
type Money struct {
	Amount   int64  // minor units; 199 with Currency "USD" == $1.99
	Currency string // ISO 4217, e.g. "USD", "EUR", "USDT"
}

// ErrCurrencyMismatch is returned when combining Money of different currencies.
var ErrCurrencyMismatch = errors.New("commerce: currency mismatch")

// New builds a Money value from minor units and a currency code.
func New(minorUnits int64, currency string) Money {
	return Money{Amount: minorUnits, Currency: currency}
}

// Add returns m+o, or ErrCurrencyMismatch when currencies differ.
func (m Money) Add(o Money) (Money, error) {
	if m.Currency != o.Currency {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{Amount: m.Amount + o.Amount, Currency: m.Currency}, nil
}

// Sub returns m-o, or ErrCurrencyMismatch when currencies differ.
func (m Money) Sub(o Money) (Money, error) {
	if m.Currency != o.Currency {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{Amount: m.Amount - o.Amount, Currency: m.Currency}, nil
}

// MulQty scales the amount by an integer quantity (line total = unit × qty).
func (m Money) MulQty(qty int) Money {
	return Money{Amount: m.Amount * int64(qty), Currency: m.Currency}
}

// IsZero reports whether the amount is exactly zero.
func (m Money) IsZero() bool { return m.Amount == 0 }

// IsNegative reports whether the amount is below zero (e.g. an adjustment).
func (m Money) IsNegative() bool { return m.Amount < 0 }

// String is a debug representation such as "1.99 USD" assuming two minor
// digits. Storefront display must format per request locale and per-currency
// exponent instead of using this.
func (m Money) String() string {
	sign, a := "", m.Amount
	if a < 0 {
		sign, a = "-", -a
	}
	return fmt.Sprintf("%s%d.%02d %s", sign, a/100, a%100, m.Currency)
}
