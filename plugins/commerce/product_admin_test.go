package commerce

import (
	"errors"
	"math"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/content"

	"github.com/gin-gonic/gin"
)

func TestParseFormatPrice(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"19.99", 1999}, {"0", 0}, {"", 0}, {"abc", 0}, {"5", 500}, {"1.005", 101}, {"1000000.00", 100000000},
		{".50", 50}, {"92233720368547758.07", 9223372036854775807},
		{"92233720368547758.08", 0}, {"999999999999999999999999", 0}, {"-1.00", 0}, {"1e6", 0},
	}
	for _, c := range cases {
		if got := parsePrice(c.in); got != c.want {
			t.Errorf("parsePrice(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	if got := formatPrice(1999); got != "19.99" {
		t.Errorf("formatPrice(1999) = %q, want 19.99", got)
	}
	if got := formatPrice(math.MaxInt64); got != "92233720368547758.07" {
		t.Errorf("formatPrice(MaxInt64) = %q", got)
	}
	if got := formatPrice(math.MinInt64); got != "-92233720368547758.08" {
		t.Errorf("formatPrice(MinInt64) = %q", got)
	}
	// Round-trip.
	if formatPrice(parsePrice("14.50")) != "14.50" {
		t.Error("price round-trip failed")
	}
}

func newFormCtx(form map[string]string) *gin.Context {
	vals := url.Values{}
	for k, v := range form {
		vals.Set(k, v)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest("POST", "/", strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.Request = req
	return c
}

func TestParseProductForm(t *testing.T) {
	c := newFormCtx(map[string]string{
		"_commerce_sku":          "ABC-1",
		"_commerce_price":        "19.99",
		"_commerce_sale_price":   "14.50",
		"_commerce_manage_stock": "true",
		"_commerce_stock_qty":    "3",
		"_commerce_tax_class":    "standard",
		"_commerce_weight":       "250",
	})
	pd := parseProductForm(c, 7, "USD")
	if pd.ContentID != 7 || pd.SKU != "ABC-1" || pd.PriceAmount != 1999 || pd.Currency != "USD" {
		t.Fatalf("basic fields wrong: %+v", pd)
	}
	if pd.SalePriceAmount == nil || *pd.SalePriceAmount != 1450 {
		t.Fatalf("sale price wrong: %v", pd.SalePriceAmount)
	}
	if !pd.ManageStock || pd.StockQty != 3 || pd.StockStatus != "instock" || pd.WeightGrams != 250 {
		t.Fatalf("stock/weight wrong: %+v", pd)
	}

	// Managed stock at zero => out of stock.
	c2 := newFormCtx(map[string]string{"_commerce_manage_stock": "true", "_commerce_stock_qty": "0"})
	if pd2 := parseProductForm(c2, 1, "USD"); pd2.StockStatus != "outofstock" {
		t.Fatalf("zero managed stock should be outofstock, got %q", pd2.StockStatus)
	}
}

func TestBuildProductFieldsHTML(t *testing.T) {
	got := string(buildProductFieldsHTML(nil, "USD", "en"))
	for _, want := range []string{`name="_commerce_price"`, `name="_commerce_sku"`, `name="_commerce_manage_stock"`, "USD"} {
		if !strings.Contains(got, want) {
			t.Errorf("meta box missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "Product Data") || strings.Contains(got, "商品数据") {
		t.Fatalf("English product meta box was not localized: %s", got)
	}
	zh := string(buildProductFieldsHTML(nil, "CNY", "zh-CN"))
	if !strings.Contains(zh, "商品数据") || !strings.Contains(zh, "价格") {
		t.Fatalf("Chinese product meta box was not localized: %s", zh)
	}
}

func TestSaveFieldsReportsProductPersistenceConflict(t *testing.T) {
	db := commerceTestDB(t)
	repo := NewRepository(db)
	if _, err := repo.CreateProductDataIfMissing(&ProductData{
		ContentID: 77, Type: "simple", PriceAmount: 1000, Currency: "USD", StockStatus: "instock",
	}); err != nil {
		t.Fatal(err)
	}

	plugin := &Plugin{repo: repo}
	ctx := newFormCtx(map[string]string{
		"_commerce_version": "0",
		"_commerce_price":   "12.00",
	})
	plugin.saveFields(ctx, ctx, &content.Content{ID: 77, Type: "product"})

	if len(ctx.Errors) != 1 || !errors.Is(ctx.Errors[0].Err, ErrProductDataConflict) {
		t.Fatalf("save errors = %v, want ErrProductDataConflict", ctx.Errors)
	}
	current, err := repo.GetProductData(77)
	if err != nil || current.PriceAmount != 1000 {
		t.Fatalf("product after rejected save = %+v, err=%v", current, err)
	}
}
