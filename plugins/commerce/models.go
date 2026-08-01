package commerce

import (
	"time"

	"go-press/pkg/dbprefix"
)

// tbl resolves a plugin table's full name, e.g. gp_plgn_commerce_orders.
func tbl(name string) string { return dbprefix.PluginTable(pluginSlug, name) }

// tableBaseNames lists every commerce table's unprefixed name for
// core.RegisterPluginTable ownership registration.
func tableBaseNames() []string {
	return []string{
		"product_data", "product_lookup",
		"orders", "order_items", "order_addresses", "payments",
		"carts", "cart_items",
		"order_notes", "refunds", "inventory_ledger",
	}
}

// ProductData holds the e-commerce fields for a product content row, keyed by
// the content row's ID. Money is stored as integer minor units + currency.
type ProductData struct {
	ContentID uint `gorm:"primaryKey" json:"content_id"`
	// Version is advanced by every admin or inventory mutation. Admin forms
	// submit the version they rendered so a stale form can never restore stock
	// that a concurrent checkout has already reserved.
	Version         uint64 `gorm:"not null;default:1" json:"version"`
	Type            string `gorm:"size:20;default:simple" json:"type"` // simple|variable|virtual|downloadable
	SKU             string `gorm:"size:100;index" json:"sku"`
	PriceAmount     int64  `json:"price_amount"` // minor units
	Currency        string `gorm:"size:8" json:"currency"`
	SalePriceAmount *int64 `json:"sale_price_amount,omitempty"`
	TaxClass        string `gorm:"size:50" json:"tax_class"`
	ManageStock     bool   `json:"manage_stock"`
	StockQty        int    `json:"stock_qty"`
	StockStatus     string `gorm:"size:20;default:instock" json:"stock_status"`
	WeightGrams     int    `json:"weight_grams"`
	Virtual         bool   `json:"virtual"`
	Downloadable    bool   `json:"downloadable"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (ProductData) TableName() string { return tbl("product_data") }

// ProductLookup is a denormalized row for fast catalog filtering/sorting.
type ProductLookup struct {
	ContentID    uint `gorm:"primaryKey"`
	CurrentPrice int64
	Currency     string `gorm:"size:8"`
	InStock      bool   `gorm:"index"`
	Sales        int
	Rating       float64
	UpdatedAt    time.Time
}

func (ProductLookup) TableName() string { return tbl("product_lookup") }

// Order is the order header (schema only in P0; order workflow lands in P2).
type Order struct {
	ID     uint   `gorm:"primaryKey"`
	Number string `gorm:"size:40;uniqueIndex"`
	// CheckoutKey is the one-time idempotency token submitted by the checkout
	// form. It is nullable so existing orders can be migrated safely; new
	// storefront orders always set it and the unique index elects exactly one
	// creator when the same form is submitted concurrently.
	CheckoutKey    *string `gorm:"size:64;uniqueIndex"`
	CheckoutCartID *uint   `gorm:"index"`
	// AccessKey is a high-entropy token that authorizes viewing a guest order's
	// received/status page without an account. It closes enumeration of the
	// (short, date-prefixed) order number: the page requires ownership OR this
	// key; guest tracking additionally requires a matching email.
	AccessKey     string `gorm:"size:64;index"`
	Status        string `gorm:"size:20;index;default:pending"`
	UserID        *uint  `gorm:"index"`
	Email         string `gorm:"size:255"`
	Currency      string `gorm:"size:8"`
	Subtotal      int64
	DiscountTotal int64
	ShippingTotal int64
	TaxTotal      int64
	GrandTotal    int64
	PaymentMethod string `gorm:"size:50"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	PaidAt        *time.Time
}

func (Order) TableName() string { return tbl("orders") }

// OrderItem is a line item snapshot (price/name captured at order time).
type OrderItem struct {
	ID               uint `gorm:"primaryKey"`
	OrderID          uint `gorm:"index"`
	ProductContentID uint
	VariationID      *uint
	NameSnapshot     string `gorm:"size:500"`
	UnitPrice        int64
	Qty              int
	LineSubtotal     int64
	LineTax          int64
	LineTotal        int64
}

func (OrderItem) TableName() string { return tbl("order_items") }

// OrderAddress is a billing or shipping address bound to an order.
type OrderAddress struct {
	ID      uint   `gorm:"primaryKey"`
	OrderID uint   `gorm:"index"`
	Type    string `gorm:"size:10"` // billing|shipping
	Name, Company, Line1, Line2, City,
	State, Country, Postcode, Phone, Email string `gorm:"size:255"`
}

func (OrderAddress) TableName() string { return tbl("order_addresses") }

// Payment records a gateway payment attempt/result, deduped by IdempotencyKey.
type Payment struct {
	ID             uint   `gorm:"primaryKey"`
	OrderID        uint   `gorm:"index"`
	Gateway        string `gorm:"size:50"`
	TxnID          string `gorm:"size:191;index"`
	Status         string `gorm:"size:20"`
	Amount         int64
	Currency       string `gorm:"size:8"`
	IdempotencyKey string `gorm:"size:191;uniqueIndex"`
	Raw            string `gorm:"type:text"`
	CreatedAt      time.Time
}

func (Payment) TableName() string { return tbl("payments") }

// OrderNote is an audit/status-log entry on an order. Status transitions append
// an internal note; is_customer_note marks notes surfaced to the buyer.
type OrderNote struct {
	ID             uint   `gorm:"primaryKey"`
	OrderID        uint   `gorm:"index"`
	Author         string `gorm:"size:100"`
	Note           string `gorm:"type:text"`
	IsCustomerNote bool
	CreatedAt      time.Time
}

func (OrderNote) TableName() string { return tbl("order_notes") }

const (
	RefundPending   = "pending"
	RefundSucceeded = "succeeded"
	RefundFailed    = "failed"
)

// Refund records a (partial) refund against an order/payment. A pending row
// reserves refund capacity while the gateway call is in flight; succeeded rows
// count toward the order's cumulative refunded amount. IdempotencyKey is
// nullable so AutoMigrate remains compatible with legacy rows.
type Refund struct {
	ID             uint    `gorm:"primaryKey"`
	OrderID        uint    `gorm:"index"`
	PaymentID      uint    `gorm:"index"`
	Amount         int64   // minor units
	Currency       string  `gorm:"size:8"`
	Reason         string  `gorm:"size:500"`
	Status         string  `gorm:"size:20;index"`
	IdempotencyKey *string `gorm:"size:191;uniqueIndex"`
	// Gateway scopes provider transaction ids: different payment providers may
	// legitimately issue the same short refund id.
	Gateway string `gorm:"size:50;uniqueIndex:idx_commerce_refund_gateway_remote,priority:1,where:gateway_refund_id <> ''"`
	// GatewayRefundID identifies the provider-side refund. Empty values belong
	// to attempts that have not produced a provider result yet. Together with
	// Gateway, the partial unique index makes every non-empty provider fact
	// single-use without assuming ids are globally unique across gateways.
	GatewayRefundID string `gorm:"size:191;uniqueIndex:idx_commerce_refund_gateway_remote,priority:2,where:gateway_refund_id <> ''"`
	Error           string `gorm:"type:text"`
	Raw             string `gorm:"type:text"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (Refund) TableName() string { return tbl("refunds") }

// InventoryLedger is the append-only stock-movement audit trail and the source
// of truth for concurrency. Reason is one of in/out/reserve/release; Delta is
// the signed change to on-hand quantity (reserve is negative, release positive).
type InventoryLedger struct {
	ID         uint   `gorm:"primaryKey"`
	ProductRef uint   `gorm:"index"` // product content id
	Delta      int    // signed change to stock_qty
	Reason     string `gorm:"size:20"` // in|out|reserve|release
	OrderID    *uint  `gorm:"index"`
	CreatedAt  time.Time
}

func (InventoryLedger) TableName() string { return tbl("inventory_ledger") }

// Cart is a guest (random token) or logged-in (user_id) cart. Guest carts carry
// a random Token; user carts leave Token blank. The index is non-unique so
// blank user tokens don't collide (guest tokens are random and effectively
// unique at the app level).
type Cart struct {
	ID    uint   `gorm:"primaryKey"`
	Token string `gorm:"size:64;index"`
	// CheckoutKey identifies the current immutable checkout snapshot. Repeated
	// views of an unchanged cart reuse it; every cart mutation clears it, and a
	// conditional update atomically installs the next random key.
	CheckoutKey *string `gorm:"size:64;uniqueIndex"`
	UserID      *uint   `gorm:"index"`
	Currency    string  `gorm:"size:8"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (Cart) TableName() string { return tbl("carts") }

// CartItem is a line in a cart.
type CartItem struct {
	ID               uint `gorm:"primaryKey"`
	CartID           uint `gorm:"index"`
	ProductContentID uint
	VariationID      *uint
	Qty              int
	UnitPriceCache   int64
}

func (CartItem) TableName() string { return tbl("cart_items") }
