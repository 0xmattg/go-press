package commerce

import (
	"net/http"
	"strings"

	"go-press/core/user"
	"go-press/pkg/middleware"

	"github.com/gin-gonic/gin"
)

// registerAccountRoutes wires the customer-facing order pages: a guest order
// lookup (order number + email) and a logged-in "my orders" account area.
func (p *Plugin) registerAccountRoutes(r *gin.Engine) {
	r.GET("/order-tracking", p.handleOrderTrackingForm)
	r.POST("/order-tracking", p.handleOrderTrackingLookup)
	r.GET("/my-account/orders", p.handleMyOrders)
	r.GET("/my-account/orders/:number", p.handleMyOrderDetail)
}

// handleOrderTrackingForm shows the guest order-lookup form.
func (p *Plugin) handleOrderTrackingForm(c *gin.Context) {
	p.renderTracking(c, "", "", "")
}

// handleOrderTrackingLookup verifies order number + email and, on a match, sends
// the buyer to the order page carrying the access key. It never reveals whether
// a number exists: any mismatch yields the same generic error, so the (short)
// order number can't be enumerated without the matching email.
func (p *Plugin) handleOrderTrackingLookup(c *gin.Context) {
	if !middleware.IsSameOrigin(c.Request) {
		c.String(http.StatusForbidden, "%s", p.t(c, "commerce.error.forbidden"))
		return
	}
	number := strings.TrimSpace(c.PostForm("number"))
	email := strings.TrimSpace(c.PostForm("email"))
	generic := p.t(c, "commerce.error.tracking_not_found")

	// Rate-limit per client IP before touching the DB, so a brute-forcer can't
	// grind order-number/email guesses. Legit customers make one or two lookups.
	if !p.trackThrottle.allow(c.ClientIP()) {
		p.renderTracking(c, number, email, p.t(c, "commerce.error.tracking_rate_limited"))
		return
	}

	if number == "" || email == "" {
		p.renderTracking(c, number, email, p.t(c, "commerce.error.tracking_required"))
		return
	}
	var order Order
	err := p.engine.DB.Where("number = ?", number).First(&order).Error
	if err != nil || !strings.EqualFold(strings.TrimSpace(order.Email), email) {
		p.renderTracking(c, number, email, generic)
		return
	}
	c.Redirect(http.StatusFound, "/checkout/complete/"+order.Number+"?key="+order.AccessKey)
}

func (p *Plugin) renderTracking(c *gin.Context, number, email, errMsg string) {
	data := gin.H{"Title": p.t(c, "commerce.tracking.title"), "Number": number, "Email": email, "Error": errMsg}
	if err := p.engine.RenderNamespacedInActiveTheme(c, pluginSlug, "order-tracking", commerceTemplateDir, data); err != nil {
		c.String(http.StatusInternalServerError, "%s", p.t(c, "commerce.error.tracking_unavailable"))
	}
}

// handleMyOrders lists the logged-in customer's orders. Guests are bounced to
// login and returned here afterward.
func (p *Plugin) handleMyOrders(c *gin.Context) {
	u := user.CurrentUser(c)
	if u == nil {
		c.Redirect(http.StatusFound, user.LoginURL(c))
		return
	}
	var orders []Order
	p.engine.DB.Where("user_id = ?", u.ID).Order("id desc").Limit(200).Find(&orders)
	rows := make([]orderRow, 0, len(orders))
	for _, o := range orders {
		rows = append(rows, orderRow{
			ID: o.ID, Number: o.Number, Status: o.Status, StatusLabel: p.storefrontOrderStatusLabel(c, o.Status),
			TotalStr: o.GrandTotalStr(), Currency: o.Currency,
			CreatedAt: o.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	data := gin.H{"Title": p.t(c, "commerce.account.orders_title"), "Orders": rows}
	if err := p.engine.RenderNamespacedInActiveTheme(c, pluginSlug, "account-orders", commerceTemplateDir, data); err != nil {
		c.String(http.StatusInternalServerError, "%s", p.t(c, "commerce.error.account_unavailable"))
	}
}

// handleMyOrderDetail shows one of the logged-in customer's orders. Ownership is
// enforced in the query (WHERE user_id), guarding against IDOR.
func (p *Plugin) handleMyOrderDetail(c *gin.Context) {
	u := user.CurrentUser(c)
	if u == nil {
		c.Redirect(http.StatusFound, user.LoginURL(c))
		return
	}
	var order Order
	if err := p.engine.DB.Where("number = ? AND user_id = ?", c.Param("number"), u.ID).First(&order).Error; err != nil {
		c.String(http.StatusNotFound, "%s", p.t(c, "commerce.error.order_not_found"))
		return
	}
	p.renderOrderView(c, &order, p.t(c, "commerce.order.detail"))
}
