package commerce

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	corecontent "go-press/core/content"

	"github.com/gin-gonic/gin"
)

func TestExtractID(t *testing.T) {
	type view struct{ ID uint }
	cases := []struct {
		name string
		in   interface{}
		want uint
	}{
		{"map uint", map[string]interface{}{"ID": uint(42)}, 42},
		{"map int", map[string]interface{}{"ID": 7}, 7},
		{"struct", view{ID: 9}, 9},
		{"ptr struct", &view{ID: 5}, 5},
		{"nil", nil, 0},
		{"no id key", map[string]interface{}{"X": 1}, 0},
	}
	for _, c := range cases {
		if got := extractID(c.in); got != c.want {
			t.Errorf("%s: extractID = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestCartViewFormatting(t *testing.T) {
	l := CartLine{UnitPrice: 1999, LineTotal: 3998, Qty: 2}
	if l.UnitPriceStr() != "19.99" || l.LineTotalStr() != "39.98" {
		t.Fatalf("line fmt: %s / %s", l.UnitPriceStr(), l.LineTotalStr())
	}
	if (CartView{Subtotal: 3998}).SubtotalStr() != "39.98" {
		t.Fatal("subtotal fmt wrong")
	}
}

func TestBuildAddToCartHTML(t *testing.T) {
	// In stock, no sale.
	got := string(buildAddToCartHTML(7, &ProductData{PriceAmount: 1999, Currency: "USD", StockStatus: "instock"}, "Add to cart", "Out of stock"))
	for _, want := range []string{`name="product_id" value="7"`, "19.99 USD", "Add to cart"} {
		if !strings.Contains(got, want) {
			t.Errorf("in-stock missing %q:\n%s", want, got)
		}
	}
	// Sale price struck through, effective price shown.
	sale := int64(1450)
	got = string(buildAddToCartHTML(7, &ProductData{PriceAmount: 1999, SalePriceAmount: &sale, Currency: "USD", StockStatus: "instock"}, "Add to cart", "Out of stock"))
	if !strings.Contains(got, "<del") || !strings.Contains(got, "14.50 USD") {
		t.Errorf("sale rendering wrong:\n%s", got)
	}
	// Managed out-of-stock => no add-to-cart form.
	got = string(buildAddToCartHTML(7, &ProductData{PriceAmount: 1999, Currency: "USD", ManageStock: true, StockStatus: "outofstock"}, "Add to cart", "Out of stock"))
	if strings.Contains(got, "Add to cart") || !strings.Contains(got, "Out of stock") {
		t.Errorf("out-of-stock should hide the form:\n%s", got)
	}
}

func TestValidateProductContentRequiresPublishedProduct(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)

	for _, tc := range []struct {
		name string
		item *corecontent.Content
		want error
	}{
		{name: "published product", item: &corecontent.Content{Type: "product", Status: corecontent.StatusPublished, PublishedAt: &past}},
		{name: "published without explicit date", item: &corecontent.Content{Type: "product", Status: corecontent.StatusPublished}},
		{name: "nil", want: ErrProductUnavailable},
		{name: "wrong type", item: &corecontent.Content{Type: "post", Status: corecontent.StatusPublished}, want: ErrProductUnavailable},
		{name: "draft", item: &corecontent.Content{Type: "product", Status: corecontent.StatusDraft}, want: ErrProductUnavailable},
		{name: "scheduled", item: &corecontent.Content{Type: "product", Status: corecontent.StatusPublished, PublishedAt: &future}, want: ErrProductUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProductContent(tc.item, now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("validateProductContent() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateProductDataPriceCurrencyAndStock(t *testing.T) {
	sale := int64(1450)
	price, currency, err := validateProductData(&ProductData{
		PriceAmount: 1999, SalePriceAmount: &sale, Currency: "usd", StockStatus: "instock",
	}, "USD")
	if err != nil || price != sale || currency != "USD" {
		t.Fatalf("valid sale = (%d, %q, %v), want (%d, USD, nil)", price, currency, err, sale)
	}

	zero := int64(0)
	equal := int64(1999)
	for _, tc := range []struct {
		name string
		data *ProductData
		want error
	}{
		{name: "missing data", want: ErrProductDataMissing},
		{name: "zero base price", data: &ProductData{PriceAmount: 0, Currency: "USD"}, want: ErrInvalidProductPrice},
		{name: "oversized base price", data: &ProductData{PriceAmount: maxCartUnitPrice + 1, Currency: "USD"}, want: ErrInvalidProductPrice},
		{name: "zero sale price", data: &ProductData{PriceAmount: 1999, SalePriceAmount: &zero, Currency: "USD"}, want: ErrInvalidProductPrice},
		{name: "sale not below base", data: &ProductData{PriceAmount: 1999, SalePriceAmount: &equal, Currency: "USD"}, want: ErrInvalidProductPrice},
		{name: "wrong currency", data: &ProductData{PriceAmount: 1999, Currency: "CNY"}, want: ErrProductCurrencyMismatch},
		{name: "managed zero stock", data: &ProductData{PriceAmount: 1999, Currency: "USD", ManageStock: true, StockQty: 0, StockStatus: "instock"}, want: ErrInsufficientStock},
		{name: "managed out of stock", data: &ProductData{PriceAmount: 1999, Currency: "USD", ManageStock: true, StockQty: 3, StockStatus: "outofstock"}, want: ErrInsufficientStock},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := validateProductData(tc.data, "USD")
			if !errors.Is(err, tc.want) {
				t.Fatalf("validateProductData() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCartQuantityAndAmountBounds(t *testing.T) {
	for _, qty := range []int{0, -1, maxCartItemQty + 1, math.MaxInt} {
		if !errors.Is(validateCartQuantity(qty), ErrInvalidCartQuantity) {
			t.Errorf("qty %d should be rejected", qty)
		}
	}
	for _, qty := range []int{1, maxCartItemQty} {
		if err := validateCartQuantity(qty); err != nil {
			t.Errorf("qty %d unexpectedly rejected: %v", qty, err)
		}
	}

	if got, err := cartLineTotal(maxCartUnitPrice, maxCartItemQty); err != nil || got <= 0 {
		t.Fatalf("largest supported line rejected: total=%d err=%v", got, err)
	}
	if _, err := cartLineTotal(math.MaxInt64, 2); !errors.Is(err, ErrInvalidProductPrice) {
		t.Fatalf("overflowing line error = %v, want ErrInvalidProductPrice", err)
	}
	if got := (&cartProduct{data: &ProductData{ManageStock: true, StockQty: 0}}).maxQty(); got != 0 {
		t.Fatalf("managed zero stock maxQty = %d, want 0", got)
	}
}

func TestCartMutationRoutesRejectCrossOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := New()
	for _, tc := range []struct {
		path    string
		handler gin.HandlerFunc
	}{
		{path: "/cart/add", handler: p.handleCartAdd},
		{path: "/cart/update", handler: p.handleCartUpdate},
		{path: "/cart/remove", handler: p.handleCartRemove},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := gin.New()
			r.POST(tc.path, tc.handler)
			req := httptest.NewRequest(http.MethodPost, "https://shop.example"+tc.path, strings.NewReader(url.Values{"qty": {"1"}, "product_id": {"1"}, "item_id": {"1"}}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", "https://evil.example")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
		})
	}
}

func TestCartMutationRoutesReturnVisibleValidationErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := New()
	for _, tc := range []struct {
		name, path string
		handler    gin.HandlerFunc
		form       url.Values
		want       int
	}{
		{name: "add bad quantity", path: "/cart/add", handler: p.handleCartAdd, form: url.Values{"product_id": {"1"}, "qty": {"0"}}, want: http.StatusBadRequest},
		{name: "update bad quantity", path: "/cart/update", handler: p.handleCartUpdate, form: url.Values{"item_id": {"1"}, "qty": {"0"}}, want: http.StatusBadRequest},
		{name: "remove bad item", path: "/cart/remove", handler: p.handleCartRemove, form: url.Values{"item_id": {"0"}}, want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.POST(tc.path, tc.handler)
			req := httptest.NewRequest(http.MethodPost, "https://shop.example"+tc.path, strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", "https://shop.example")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%q", rec.Code, tc.want, rec.Body.String())
			}
			if strings.TrimSpace(rec.Body.String()) == "" {
				t.Fatal("validation error must be visible in the response body")
			}
		})
	}
}
