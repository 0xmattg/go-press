package commerce

import (
	"context"
	"errors"
	"fmt"
	"html"
	"html/template"
	"math"
	"strconv"
	"strings"

	"go-press/core/content"

	"github.com/gin-gonic/gin"
)

var ErrInvalidPriceInput = errors.New("commerce: invalid decimal price")

// renderMetaBox injects the commerce product fields into the admin content edit
// form, only for the "product" content type. Registered on the
// admin.content_form.fields filter; args are (*gin.Context, *content.Content,
// *content.ContentTypeDef).
func (p *Plugin) renderMetaBox(value interface{}, args ...interface{}) interface{} {
	existing := asHTML(value)
	if td := contentTypeDefArg(args); td == nil || td.Name != "product" {
		return existing
	}
	var pd *ProductData
	if item := contentArg(args); item != nil && item.ID != 0 && p.repo != nil {
		pd, _ = p.repo.GetProductData(item.ID)
	}
	return existing + buildProductFieldsHTML(pd, p.storeCurrency(), p.adminLanguage())
}

// saveFields persists the product commerce fields to product_data and refreshes
// product_lookup. Registered on the admin.content.saved action; args are
// (*gin.Context, *content.Content).
func (p *Plugin) saveFields(_ context.Context, args ...interface{}) {
	if len(args) < 2 || p.repo == nil {
		return
	}
	c, _ := args[0].(*gin.Context)
	item, _ := args[1].(*content.Content)
	if c == nil || item == nil || item.ID == 0 || item.Type != "product" {
		return
	}
	pd := parseProductForm(c, item.ID, p.storeCurrency())
	expectedVersion, _ := strconv.ParseUint(strings.TrimSpace(c.PostForm("_commerce_version")), 10, 64)
	if err := p.repo.SaveProductData(pd, expectedVersion); err != nil {
		c.Error(fmt.Errorf("commerce: save product price and inventory: %w", err)).SetType(gin.ErrorTypePrivate)
	}
}

func (p *Plugin) storeCurrency() string { return p.opt("plugin_commerce_store_currency", "USD") }

func asHTML(v interface{}) template.HTML {
	switch t := v.(type) {
	case template.HTML:
		return t
	case string:
		return template.HTML(t)
	}
	return template.HTML("")
}

func contentArg(args []interface{}) *content.Content {
	for _, a := range args {
		if it, ok := a.(*content.Content); ok {
			return it
		}
	}
	return nil
}

func contentTypeDefArg(args []interface{}) *content.ContentTypeDef {
	for _, a := range args {
		if td, ok := a.(*content.ContentTypeDef); ok {
			return td
		}
	}
	return nil
}

// parseProductForm reads the _commerce_* fields off the submitted form into a
// ProductData for the given product content id.
func parseProductForm(c *gin.Context, contentID uint, currency string) *ProductData {
	pd := &ProductData{
		ContentID:   contentID,
		Type:        "simple",
		SKU:         strings.TrimSpace(c.PostForm("_commerce_sku")),
		PriceAmount: parsePrice(c.PostForm("_commerce_price")),
		Currency:    currency,
		TaxClass:    strings.TrimSpace(c.PostForm("_commerce_tax_class")),
		ManageStock: isChecked(c.PostForm("_commerce_manage_stock")),
		StockStatus: "instock",
	}
	if sp := strings.TrimSpace(c.PostForm("_commerce_sale_price")); sp != "" {
		v := parsePrice(sp)
		pd.SalePriceAmount = &v
	}
	if pd.ManageStock {
		pd.StockQty, _ = strconv.Atoi(strings.TrimSpace(c.PostForm("_commerce_stock_qty")))
		if pd.StockQty <= 0 {
			pd.StockStatus = "outofstock"
		}
	}
	pd.WeightGrams, _ = strconv.Atoi(strings.TrimSpace(c.PostForm("_commerce_weight")))
	return pd
}

func isChecked(v string) bool { return v == "true" || v == "on" || v == "1" }

// parsePrice converts a decimal price string ("19.99") to integer minor units
// (1999). Blank/invalid input yields 0. Assumes a 2-decimal currency (v1 scope).
// The strict parser below performs the arithmetic in integers so large input
// cannot overflow through a float64→int64 conversion.
func parsePrice(s string) int64 {
	amount, err := parsePriceStrict(s)
	if err != nil {
		return 0
	}
	return amount
}

func parsePriceStrict(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return 0, ErrInvalidPriceInput
	}
	whole := parts[0]
	if whole == "" {
		whole = "0"
	}
	if !decimalDigits(whole) {
		return 0, ErrInvalidPriceInput
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if fraction != "" && !decimalDigits(fraction) {
			return 0, ErrInvalidPriceInput
		}
	}

	units, err := strconv.ParseUint(whole, 10, 64)
	if err != nil {
		return 0, ErrInvalidPriceInput
	}
	cents := uint64(0)
	if len(fraction) > 0 {
		cents = uint64(fraction[0]-'0') * 10
	}
	if len(fraction) > 1 {
		cents += uint64(fraction[1] - '0')
	}
	// Preserve the old user-facing ability to enter extra precision, but round
	// it deterministically instead of depending on binary floating point.
	if len(fraction) > 2 && fraction[2] >= '5' {
		cents++
	}
	if cents == 100 {
		if units == ^uint64(0) {
			return 0, ErrInvalidPriceInput
		}
		units++
		cents = 0
	}
	maxMinor := uint64(math.MaxInt64)
	if units > maxMinor/100 || (units == maxMinor/100 && cents > maxMinor%100) {
		return 0, ErrInvalidPriceInput
	}
	return int64(units*100 + cents), nil
}

func decimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// formatPrice renders minor units back to a 2-decimal string ("1999" -> "19.99").
func formatPrice(minor int64) string {
	sign := ""
	var magnitude uint64
	if minor < 0 {
		sign = "-"
		// This form is safe for math.MinInt64, whose positive value is not
		// representable as int64.
		magnitude = uint64(-(minor + 1)) + 1
	} else {
		magnitude = uint64(minor)
	}
	fraction := strconv.FormatUint(magnitude%100, 10)
	if magnitude%100 < 10 {
		fraction = "0" + fraction
	}
	return sign + strconv.FormatUint(magnitude/100, 10) + "." + fraction
}

// buildProductFieldsHTML renders the product meta-box markup, prefilled from pd
// (nil for a new product). Field names use the _commerce_ prefix so saveFields
// picks them up.
func buildProductFieldsHTML(pd *ProductData, currency, lang string) template.HTML {
	sku, price, sale, stockQty, taxClass, weight := "", "", "", "", "", ""
	version := uint64(0)
	manageStock := false
	if pd != nil {
		version = pd.Version
		sku = pd.SKU
		if pd.PriceAmount > 0 {
			price = formatPrice(pd.PriceAmount)
		}
		if pd.SalePriceAmount != nil {
			sale = formatPrice(*pd.SalePriceAmount)
		}
		manageStock = pd.ManageStock
		if pd.StockQty != 0 {
			stockQty = strconv.Itoa(pd.StockQty)
		}
		taxClass = pd.TaxClass
		if pd.WeightGrams > 0 {
			weight = strconv.Itoa(pd.WeightGrams)
		}
	}
	e := html.EscapeString
	t := func(key string, args ...interface{}) string {
		return e(commerceAdminCatalog.T(lang, key, args...))
	}
	checked := ""
	if manageStock {
		checked = " checked"
	}

	var b strings.Builder
	b.WriteString(`<fieldset class="form-group commerce-product-fields" style="border:1px solid #e2e8f0;border-radius:8px;padding:1rem;margin-top:1rem;">`)
	b.WriteString(`<input type="hidden" name="_commerce_version" value="` + strconv.FormatUint(version, 10) + `">`)
	b.WriteString(`<legend style="font-weight:600;padding:0 .5rem;">` + t("plugin.commerce.product.data", currency) + `</legend>`)
	b.WriteString(`<div class="form-group"><label for="_commerce_sku">SKU</label><input type="text" id="_commerce_sku" name="_commerce_sku" value="` + e(sku) + `"></div>`)
	b.WriteString(`<div class="form-group"><label for="_commerce_price">` + t("plugin.commerce.product.price", currency) + `</label><input type="text" inputmode="decimal" id="_commerce_price" name="_commerce_price" value="` + e(price) + `" placeholder="0.00"></div>`)
	b.WriteString(`<div class="form-group"><label for="_commerce_sale_price">` + t("plugin.commerce.product.sale_price") + `</label><input type="text" inputmode="decimal" id="_commerce_sale_price" name="_commerce_sale_price" value="` + e(sale) + `" placeholder="0.00"></div>`)
	b.WriteString(`<div class="form-group"><label><input type="checkbox" name="_commerce_manage_stock" value="true"` + checked + `> ` + t("plugin.commerce.product.manage_stock") + `</label></div>`)
	b.WriteString(`<div class="form-group"><label for="_commerce_stock_qty">` + t("plugin.commerce.product.stock_quantity") + `</label><input type="number" id="_commerce_stock_qty" name="_commerce_stock_qty" value="` + e(stockQty) + `"></div>`)
	b.WriteString(`<div class="form-group"><label for="_commerce_tax_class">` + t("plugin.commerce.product.tax_class") + `</label><input type="text" id="_commerce_tax_class" name="_commerce_tax_class" value="` + e(taxClass) + `" placeholder="standard"></div>`)
	b.WriteString(`<div class="form-group"><label for="_commerce_weight">` + t("plugin.commerce.product.weight") + `</label><input type="number" id="_commerce_weight" name="_commerce_weight" value="` + e(weight) + `"></div>`)
	b.WriteString(`</fieldset>`)
	return template.HTML(b.String())
}
