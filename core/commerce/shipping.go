package commerce

import "github.com/gin-gonic/gin"

// ShippingZone matches a destination address to decide whether its methods
// apply (geographic zones).
type ShippingZone interface {
	ID() string
	Match(a Address) bool
}

// ShippingMethod calculates one or more selectable rates for a shipment.
type ShippingMethod interface {
	ID() string
	Title(c *gin.Context) string
	CalculateRates(c *gin.Context, ctx ShipmentContext) []ShippingRate
}

// ShipmentContext is the input to rate calculation.
type ShipmentContext struct {
	Dest     Address
	Items    []ShipItem
	Subtotal Money
}

// ShipItem is one line in a shipment; weight/dims drive carrier pricing.
type ShipItem struct {
	Qty           int
	WeightGrams   int
	LengthMM      int
	WidthMM       int
	HeightMM      int
	ShippingClass string
}

// ShippingRate is one shipping option presented at checkout.
type ShippingRate struct {
	MethodID string
	Label    string
	Cost     Money
	TaxClass string
}
