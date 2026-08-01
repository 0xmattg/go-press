package commerce

import "github.com/gin-gonic/gin"

// PromotionRule applies discounts/adjustments to a cart (coupons, cart-level
// promotions). Multiple rules compose; each returns the adjustments it makes.
type PromotionRule interface {
	Apply(c *gin.Context, cart CartView) []Adjustment
}

// CartView is the read-only cart snapshot promotion rules evaluate against.
// The full cart type lives in the commerce engine; rules only need this view.
type CartView struct {
	Currency string
	Subtotal Money
	Items    []CartLineView
	Coupons  []string
}

// CartLineView is one cart line as seen by a promotion rule.
type CartLineView struct {
	Ref       string
	Qty       int
	UnitPrice Money
}

// Adjustment is a discount (negative Amount) or surcharge applied to the cart.
type Adjustment struct {
	Code   string
	Label  string
	Amount Money // negative for a discount
}
