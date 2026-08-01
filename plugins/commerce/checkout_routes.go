package commerce

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	corecommerce "go-press/core/commerce"
	"go-press/core/user"
	"go-press/pkg/middleware"

	"github.com/gin-gonic/gin"
)

// registerCheckoutRoutes wires the public checkout flow. POST /checkout is
// state-changing and money-adjacent, so it enforces a same-origin (CSRF) guard.
func (p *Plugin) registerCheckoutRoutes(r *gin.Engine) {
	r.GET("/checkout", p.handleCheckoutView)
	r.POST("/checkout", p.handleCheckoutSubmit)
	r.GET("/checkout/pay/:number", p.handleCheckoutPay)
	r.GET("/checkout/complete/:number", p.handleCheckoutComplete)
}

// gatewayView is a payment method rendered as a checkout radio option.
type gatewayView struct{ ID, Title, Icon string }

func (p *Plugin) gatewayViews(c *gin.Context) []gatewayView {
	var out []gatewayView
	for _, g := range corecommerce.AvailablePaymentGateways(c, p.engine.Hooks) {
		out = append(out, gatewayView{ID: g.ID(), Title: g.Title(c), Icon: g.Icon()})
	}
	return out
}

func (p *Plugin) handleCheckoutView(c *gin.Context) {
	p.renderCheckout(c, CartView{}, "")
}

func (p *Plugin) renderCheckout(c *gin.Context, cart CartView, errMsg string) {
	stableCart, checkoutKey, err := p.cart().checkoutSnapshot(c)
	if err != nil {
		if errors.Is(err, ErrEmptyCart) {
			c.Redirect(http.StatusFound, "/cart")
			return
		}
		c.String(http.StatusInternalServerError, "%s", p.t(c, "commerce.error.checkout_unavailable"))
		return
	}
	cart = stableCart
	shipping, pricingErr := p.flatShipping()
	grand := cart.Subtotal
	if pricingErr == nil {
		grand, pricingErr = addMoneyChecked(cart.Subtotal, shipping)
	}
	gateways := p.gatewayViews(c)
	if pricingErr != nil {
		shipping = 0
		grand = cart.Subtotal
		gateways = nil
		errMsg = p.t(c, "commerce.error.invalid_totals")
	}
	data := gin.H{
		"Title":       p.t(c, "commerce.checkout.title"),
		"Cart":        cart,
		"Gateways":    gateways,
		"Shipping":    formatPrice(shipping),
		"GrandTotal":  formatPrice(grand),
		"Error":       errMsg,
		"CheckoutKey": checkoutKey,
	}
	if u := user.CurrentUser(c); u != nil && u.Email != nil {
		data["Email"] = *u.Email
	}
	if err := p.engine.RenderNamespacedInActiveTheme(c, pluginSlug, "checkout", commerceTemplateDir, data); err != nil {
		c.String(http.StatusInternalServerError, "%s", p.t(c, "commerce.error.checkout_unavailable"))
	}
}

func (p *Plugin) handleCheckoutSubmit(c *gin.Context) {
	// CSRF: money-adjacent POST must be same-origin.
	if !middleware.IsSameOrigin(c.Request) {
		c.String(http.StatusForbidden, "%s", p.t(c, "commerce.error.forbidden"))
		return
	}
	in := parseCheckoutForm(c)
	order, action, err := p.checkout().PlaceOrder(c, in)
	if err != nil {
		if errors.Is(err, ErrEmptyCart) {
			c.Redirect(http.StatusFound, "/cart")
			return
		}
		p.renderCheckout(c, p.cart().View(c), p.checkoutErrorMessage(c, err))
		return
	}

	// Resolve every PaymentAction explicitly. Inline gateways get a private GET
	// hand-off page so refreshing never repeats the checkout POST; display and
	// completed actions land on the order page, which reloads persisted generic
	// instructions when applicable.
	switch typed := action.(type) {
	case corecommerce.RedirectAction:
		c.Redirect(http.StatusFound, typed.URL)
		return
	case corecommerce.InlineAction:
		c.Redirect(http.StatusSeeOther, p.checkoutPayURL(order))
		return
	case corecommerce.DisplayAction, corecommerce.CompletedAction:
		// Continue to the order-received redirect below.
	default:
		c.String(http.StatusInternalServerError, "%s", p.t(c, "commerce.error.payment_unavailable"))
		return
	}
	// Carry the access key (authorizes the guest view) and placed=1 (shows the
	// thank-you heading only right after checkout).
	c.Redirect(http.StatusSeeOther, p.checkoutCompleteURL(order, true))
}

// handleCheckoutPay resumes a persisted redirect/inline payment action without
// calling the gateway a second time. Access is restricted by the same owner or
// high-entropy key check as the order page.
func (p *Plugin) handleCheckoutPay(c *gin.Context) {
	order := p.findAuthorizedOrder(c)
	if order == nil {
		return
	}
	if order.Status != OrderPending {
		c.Redirect(http.StatusSeeOther, p.checkoutCompleteURL(order, false))
		return
	}
	action, ok, reconciling, err := p.checkout().persistedPaymentAction(order)
	if err != nil {
		c.String(http.StatusInternalServerError, "%s", p.t(c, "commerce.error.payment_unavailable"))
		return
	}
	if !ok || reconciling {
		c.Redirect(http.StatusSeeOther, p.checkoutCompleteURL(order, false))
		return
	}
	switch typed := action.(type) {
	case corecommerce.RedirectAction:
		c.Redirect(http.StatusFound, typed.URL)
	case corecommerce.InlineAction:
		p.renderInlinePayment(c, order, typed)
	default:
		c.Redirect(http.StatusSeeOther, p.checkoutCompleteURL(order, false))
	}
}

func (p *Plugin) handleCheckoutComplete(c *gin.Context) {
	order := p.findAuthorizedOrder(c)
	if order == nil {
		return
	}
	heading := p.t(c, "commerce.order.detail")
	if c.Query("placed") == "1" {
		heading = p.t(c, "commerce.order.thank_you")
	}
	p.renderOrderView(c, order, heading)
}

func (p *Plugin) findAuthorizedOrder(c *gin.Context) *Order {
	var order Order
	if err := p.engine.DB.Where("number = ?", c.Param("number")).First(&order).Error; err != nil || !p.canViewOrder(c, &order) {
		c.String(http.StatusNotFound, "%s", p.t(c, "commerce.error.order_not_found"))
		return nil
	}
	return &order
}

func (p *Plugin) checkoutPayURL(order *Order) string {
	return "/checkout/pay/" + url.PathEscape(order.Number) + "?key=" + url.QueryEscape(order.AccessKey)
}

func (p *Plugin) checkoutCompleteURL(order *Order, placed bool) string {
	value := "/checkout/complete/" + url.PathEscape(order.Number) + "?key=" + url.QueryEscape(order.AccessKey)
	if placed {
		value += "&placed=1"
	}
	return value
}

func (p *Plugin) renderInlinePayment(c *gin.Context, order *Order, action corecommerce.InlineAction) {
	payload, err := json.Marshal(action.ClientData)
	if err != nil {
		c.String(http.StatusInternalServerError, "%s", p.t(c, "commerce.error.payment_unavailable"))
		return
	}
	data := gin.H{
		"Title":          p.t(c, "commerce.payment.inline_title"),
		"Order":          *order,
		"InlineAction":   action,
		"ClientDataJSON": string(payload),
		"CompleteURL":    p.checkoutCompleteURL(order, false),
	}
	if err := p.engine.RenderNamespacedInActiveTheme(c, pluginSlug, "payment-inline", commerceTemplateDir, data); err != nil {
		c.String(http.StatusInternalServerError, "%s", p.t(c, "commerce.error.payment_unavailable"))
	}
}

// canViewOrder authorizes viewing an order's status page: true for the logged-in
// owner, or for a request carrying the correct access key (constant-time
// compared). Guest orders (no UserID) are viewable only with the key.
func (p *Plugin) canViewOrder(c *gin.Context, order *Order) bool {
	if order.UserID != nil {
		if u := user.CurrentUser(c); u != nil && u.ID == *order.UserID {
			return true
		}
	}
	key := c.Query("key")
	return key != "" && order.AccessKey != "" &&
		subtle.ConstantTimeCompare([]byte(key), []byte(order.AccessKey)) == 1
}

// renderOrderView renders the order-received/status page for an order the caller
// has already authorized. Shared by checkout completion, guest tracking, and the
// account order detail.
func (p *Plugin) renderOrderView(c *gin.Context, order *Order, heading string) {
	var items []OrderItem
	p.engine.DB.Where("order_id = ?", order.ID).Order("id asc").Find(&items)

	data := gin.H{
		"Title":       p.t(c, "commerce.order.title_prefix") + " " + order.Number,
		"Heading":     heading,
		"Order":       *order,
		"Items":       items,
		"StatusLabel": p.storefrontOrderStatusLabel(c, order.Status),
		"AwaitPay":    order.Status == OrderOnHold || order.Status == OrderPending,
	}
	// Reload generic display instructions persisted from StartPayment. This works
	// for offline transfer, crypto invoices, and future display-type gateways
	// without any gateway-specific branch in the storefront.
	if order.Status == OrderOnHold || order.Status == OrderPending {
		if action, ok, _, err := p.checkout().persistedPaymentAction(order); err == nil && ok {
			if display, isDisplay := action.(corecommerce.DisplayAction); isDisplay {
				data["PayAction"] = display
			}
		}
	}
	if err := p.engine.RenderNamespacedInActiveTheme(c, pluginSlug, "order-received", commerceTemplateDir, data); err != nil {
		c.String(http.StatusInternalServerError, "%s", p.t(c, "commerce.error.order_unavailable"))
	}
}

// parseCheckoutForm reads the checkout form into a CheckoutInput.
func parseCheckoutForm(c *gin.Context) CheckoutInput {
	billing := corecommerce.Address{
		Name:     strings.TrimSpace(c.PostForm("billing_name")),
		Company:  strings.TrimSpace(c.PostForm("billing_company")),
		Line1:    strings.TrimSpace(c.PostForm("billing_line1")),
		Line2:    strings.TrimSpace(c.PostForm("billing_line2")),
		City:     strings.TrimSpace(c.PostForm("billing_city")),
		State:    strings.TrimSpace(c.PostForm("billing_state")),
		Country:  strings.TrimSpace(c.PostForm("billing_country")),
		Postcode: strings.TrimSpace(c.PostForm("billing_postcode")),
		Phone:    strings.TrimSpace(c.PostForm("billing_phone")),
		Email:    strings.TrimSpace(c.PostForm("email")),
	}
	return CheckoutInput{
		Email:         strings.TrimSpace(c.PostForm("email")),
		Billing:       billing,
		ShipToBilling: true, // P2: single-address checkout; separate shipping is a later phase
		PaymentMethod: strings.TrimSpace(c.PostForm("payment_method")),
		CheckoutKey:   c.PostForm("checkout_key"),
	}
}

func (p *Plugin) checkoutErrorMessage(c *gin.Context, err error) string {
	if errors.Is(err, ErrNoGateway) {
		return p.t(c, "commerce.error.no_gateway")
	}
	if errors.Is(err, ErrOversell) {
		return p.t(c, "commerce.error.insufficient_stock")
	}
	if errors.Is(err, ErrInvalidOrderTotals) {
		return p.t(c, "commerce.error.invalid_totals")
	}
	if errors.Is(err, ErrCheckoutCartChanged) || errors.Is(err, ErrProductUnavailable) ||
		errors.Is(err, ErrProductDataMissing) || errors.Is(err, ErrInvalidProductPrice) ||
		errors.Is(err, ErrProductCurrencyMismatch) {
		return p.t(c, "commerce.error.checkout_cart_changed")
	}
	if errors.Is(err, ErrInvalidCheckoutKey) {
		return p.t(c, "commerce.error.invalid_checkout_key")
	}
	if errors.Is(err, ErrCheckoutAttemptClosed) {
		return p.t(c, "commerce.error.checkout_attempt_closed")
	}
	if errors.Is(err, ErrCheckoutInProgress) {
		return p.t(c, "commerce.error.checkout_in_progress")
	}
	return p.t(c, "commerce.error.checkout_failed")
}

func (p *Plugin) storefrontOrderStatusLabel(c *gin.Context, status string) string {
	key := map[string]string{
		OrderPending:           "commerce.status.pending",
		OrderOnHold:            "commerce.status.on_hold",
		OrderProcessing:        "commerce.status.processing",
		OrderCompleted:         "commerce.status.completed",
		OrderCancelled:         "commerce.status.cancelled",
		OrderFailed:            "commerce.status.failed",
		OrderRefunded:          "commerce.status.refunded",
		OrderPartiallyRefunded: "commerce.status.partially_refunded",
		OrderReconciliation:    "commerce.status.reconciliation",
	}[status]
	if key == "" {
		return status
	}
	return p.t(c, key)
}
