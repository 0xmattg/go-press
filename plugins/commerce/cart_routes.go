package commerce

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/0xmattg/go-press/pkg/middleware"

	"github.com/gin-gonic/gin"
)

// commerceTemplateDir is where the plugin's default storefront fragment
// templates live (theme overrides at <theme templates>/commerce/ win).
const commerceTemplateDir = "plugins/commerce/templates/commerce"

// registerStorefrontRoutes wires the public cart routes onto the gin engine.
// Called from the routes.register hook so the routes appear/disappear with the
// module and survive router rebuilds.
func (p *Plugin) registerStorefrontRoutes(r *gin.Engine) {
	r.GET("/cart", p.handleCartView)
	r.POST("/cart/add", p.handleCartAdd)
	r.POST("/cart/update", p.handleCartUpdate)
	r.POST("/cart/remove", p.handleCartRemove)
	p.registerCheckoutRoutes(r)
	p.registerAccountRoutes(r)
}

func (p *Plugin) handleCartView(c *gin.Context) {
	data := gin.H{"Title": p.t(c, "commerce.cart.title"), "Cart": p.cart().View(c)}
	if err := p.engine.RenderNamespacedInActiveTheme(c, pluginSlug, "cart", commerceTemplateDir, data); err != nil {
		c.String(http.StatusInternalServerError, "%s", p.t(c, "commerce.error.cart_unavailable"))
	}
}

func (p *Plugin) handleCartAdd(c *gin.Context) {
	if !middleware.IsSameOrigin(c.Request) {
		c.String(http.StatusForbidden, "%s", p.t(c, "commerce.error.forbidden"))
		return
	}
	qty, err := strconv.Atoi(strings.TrimSpace(c.PostForm("qty")))
	if err != nil {
		p.writeCartMutationError(c, ErrInvalidCartQuantity)
		return
	}
	productID, err := parseUintForm(c, "product_id")
	if err != nil {
		p.writeCartMutationError(c, ErrProductUnavailable)
		return
	}
	if err := p.cart().Add(c, productID, qty); err != nil {
		p.writeCartMutationError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/cart")
}

func (p *Plugin) handleCartUpdate(c *gin.Context) {
	if !middleware.IsSameOrigin(c.Request) {
		c.String(http.StatusForbidden, "%s", p.t(c, "commerce.error.forbidden"))
		return
	}
	qty, err := strconv.Atoi(strings.TrimSpace(c.PostForm("qty")))
	if err != nil {
		p.writeCartMutationError(c, ErrInvalidCartQuantity)
		return
	}
	itemID, err := parseUintForm(c, "item_id")
	if err != nil {
		p.writeCartMutationError(c, ErrCartItemNotFound)
		return
	}
	if err := p.cart().SetItemQty(c, itemID, qty); err != nil {
		p.writeCartMutationError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/cart")
}

func (p *Plugin) handleCartRemove(c *gin.Context) {
	if !middleware.IsSameOrigin(c.Request) {
		c.String(http.StatusForbidden, "%s", p.t(c, "commerce.error.forbidden"))
		return
	}
	itemID, err := parseUintForm(c, "item_id")
	if err != nil {
		p.writeCartMutationError(c, ErrCartItemNotFound)
		return
	}
	if err := p.cart().RemoveItem(c, itemID); err != nil {
		p.writeCartMutationError(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/cart")
}

func parseUintForm(c *gin.Context, key string) (uint, error) {
	v, err := strconv.ParseUint(strings.TrimSpace(c.PostForm(key)), 10, strconv.IntSize)
	if err != nil || v == 0 {
		return 0, errors.New("invalid unsigned form value")
	}
	return uint(v), nil
}

func (p *Plugin) writeCartMutationError(c *gin.Context, err error) {
	status, key := http.StatusInternalServerError, "commerce.error.cart_unavailable"
	switch {
	case errors.Is(err, ErrInvalidCartQuantity):
		status, key = http.StatusBadRequest, "commerce.error.invalid_quantity"
	case errors.Is(err, ErrProductUnavailable):
		status, key = http.StatusNotFound, "commerce.error.product_unavailable"
	case errors.Is(err, ErrProductDataMissing):
		status, key = http.StatusUnprocessableEntity, "commerce.error.product_data_missing"
	case errors.Is(err, ErrInvalidProductPrice), errors.Is(err, ErrProductCurrencyMismatch):
		status, key = http.StatusUnprocessableEntity, "commerce.error.invalid_price"
	case errors.Is(err, ErrInsufficientStock):
		status, key = http.StatusConflict, "commerce.error.insufficient_stock"
	case errors.Is(err, ErrCartItemNotFound):
		status, key = http.StatusNotFound, "commerce.error.cart_item_not_found"
	}
	c.Header("Cache-Control", "no-store")
	c.String(status, "%s", p.t(c, key))
}
