package commerce

import (
	"fmt"
	"html"
	"html/template"
	"reflect"

	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
)

// hookAddToCart is the storefront slot a shop theme renders on a product page:
//
//	{{renderHook "commerce.product.add_to_cart" .Item .Ctx}}
//
// commerce fills it with the product's price and an add-to-cart form. The theme
// depends on commerce (theme.toml [requires]) and calls this documented slot; it
// never imports the plugin — interaction stays core-mediated via renderHook.
const hookAddToCart = "commerce.product.add_to_cart"

// renderAddToCart renders price + add-to-cart form for the product passed to the
// slot. The passed value is the product view (a map or struct with an ID).
func (p *Plugin) renderAddToCart(value interface{}, args ...interface{}) interface{} {
	existing := asHTML(value)
	if p.repo == nil {
		return existing
	}
	pid := extractID(firstArg(args))
	if pid == 0 {
		return existing
	}
	pd, err := p.repo.GetProductData(pid)
	if err != nil || pd == nil {
		return existing
	}
	c := ginArg(args)
	return existing + buildAddToCartHTML(pid, pd,
		p.t(c, "commerce.add_to_cart"), p.t(c, "commerce.out_of_stock"))
}

// buildAddToCartHTML renders the price block + add-to-cart form (or an
// out-of-stock notice) for a product. Pure function so it is unit-testable.
func buildAddToCartHTML(pid uint, pd *ProductData, addLabel, outOfStockLabel string) template.HTML {
	price := pd.PriceAmount
	struck := ""
	if pd.SalePriceAmount != nil && *pd.SalePriceAmount > 0 && *pd.SalePriceAmount < pd.PriceAmount {
		struck = formatPrice(pd.PriceAmount)
		price = *pd.SalePriceAmount
	}

	var out string
	out += `<div class="commerce-add-to-cart">`
	out += `<div class="commerce-price">`
	if struck != "" {
		out += `<del style="opacity:.6;margin-right:.4rem;">` + html.EscapeString(struck) + `</del>`
	}
	out += `<strong>` + html.EscapeString(formatPrice(price)+" "+pd.Currency) + `</strong></div>`
	if pd.ManageStock && pd.StockStatus != "instock" {
		out += `<p class="commerce-oos">` + html.EscapeString(outOfStockLabel) + `</p>`
	} else {
		out += fmt.Sprintf(`<form method="POST" action="/cart/add" class="commerce-atc-form" style="display:flex;gap:.5rem;align-items:center;margin-top:.5rem;">`+
			`<input type="hidden" name="product_id" value="%d">`+
			`<input type="number" name="qty" value="1" min="1" style="width:64px;">`+
			`<button type="submit" class="btn btn-primary">%s</button></form>`, pid, html.EscapeString(addLabel))
	}
	out += `</div>`
	return template.HTML(out)
}

// renderMiniCart appends a cart link with the current item count to the theme's
// header.nav.after slot, plus a "my orders" link for logged-in customers.
func (p *Plugin) renderMiniCart(value interface{}, args ...interface{}) interface{} {
	existing := asHTML(value)
	n, account := 0, ""
	if c := ginArg(args); c != nil {
		n = p.cart().Count(c)
		if user.CurrentUser(c) != nil {
			account = `<li class="commerce-my-orders"><a href="/my-account/orders">` +
				html.EscapeString(p.t(c, "commerce.account.my_orders")) + `</a></li>`
		}
	}
	return existing + template.HTML(account+fmt.Sprintf(
		`<li class="commerce-mini-cart"><a href="/cart">🛒 <span class="commerce-cart-count">%d</span></a></li>`, n))
}

func firstArg(args []interface{}) interface{} {
	if len(args) > 0 {
		return args[0]
	}
	return nil
}

func ginArg(args []interface{}) *gin.Context {
	for _, a := range args {
		if c, ok := a.(*gin.Context); ok {
			return c
		}
	}
	return nil
}

// extractID pulls an "ID" value from a map or struct passed to a render hook,
// so the add-to-cart slot works whether the theme passes a gin.H view (BaseTheme)
// or a typed struct (custom theme).
func extractID(v interface{}) uint {
	if v == nil {
		return 0
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return 0
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map:
		if mv := rv.MapIndex(reflect.ValueOf("ID")); mv.IsValid() {
			return toUint(mv)
		}
	case reflect.Struct:
		if f := rv.FieldByName("ID"); f.IsValid() {
			return toUint(f)
		}
	}
	return 0
}

func toUint(v reflect.Value) uint {
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return uint(v.Uint())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return uint(v.Int())
	}
	return 0
}
