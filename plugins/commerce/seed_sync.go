package commerce

import (
	"strconv"
	"strings"

	"go-press/pkg/logger"
)

// syncProductsFromSeed derives product_data + product_lookup rows from the
// _commerce_* meta on product content after a seed/demo import (fired via the
// core hook.SeedCompleted action). This lets a theme's demo seed file carry
// product prices/stock as plain content meta — the seed stays pure core content
// and never references the commerce tables, and priced demo products "just work"
// after a one-click import.
func (p *Plugin) syncProductsFromSeed() {
	if p.engine == nil || p.repo == nil || p.engine.Content == nil {
		return
	}
	products, err := p.engine.Content.ListByType("product", 1000, 0)
	if err != nil {
		logger.Warn("commerce: seed sync could not list products", "error", err)
		return
	}
	currency := p.storeCurrency()
	synced := 0
	for i := range products {
		item := products[i]
		meta, _ := p.engine.Content.GetMeta(item.ID)
		price := parsePrice(meta["_commerce_price"])
		if price == 0 {
			continue // not a priced demo product — leave it alone
		}
		pd := &ProductData{
			ContentID:   item.ID,
			Type:        "simple",
			SKU:         strings.TrimSpace(meta["_commerce_sku"]),
			PriceAmount: price,
			Currency:    currency,
			TaxClass:    strings.TrimSpace(meta["_commerce_tax_class"]),
			StockStatus: "instock",
		}
		if sp := parsePrice(meta["_commerce_sale_price"]); sp > 0 {
			pd.SalePriceAmount = &sp
		}
		if isChecked(meta["_commerce_manage_stock"]) {
			pd.ManageStock = true
			pd.StockQty, _ = strconv.Atoi(strings.TrimSpace(meta["_commerce_stock_qty"]))
			if pd.StockQty <= 0 {
				pd.StockStatus = "outofstock"
			}
		}
		created, err := p.repo.CreateProductDataIfMissing(pd)
		if err != nil || !created {
			continue
		}
		synced++
	}
	if synced > 0 {
		logger.Info("commerce: synced products from seed meta", "count", synced)
	}
}
