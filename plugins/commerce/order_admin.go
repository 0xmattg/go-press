package commerce

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go-press/core/admin"
	corecommerce "go-press/core/commerce"
	"go-press/pkg/logger"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrRefundAmount               = errors.New("commerce: refund amount is invalid")
	ErrRefundExceedsRemaining     = errors.New("commerce: refund exceeds remaining total")
	ErrRefundIdempotencyConflict  = errors.New("commerce: refund idempotency key conflict")
	ErrRefundGatewayUnavailable   = errors.New("commerce: original refund gateway is unavailable")
	ErrRefundInProgress           = errors.New("commerce: refund is in progress")
	ErrRefundStatusSync           = errors.New("commerce: refund succeeded but order status sync failed")
	ErrRefundIdempotencyRequired  = errors.New("commerce: automatic refund gateway lacks idempotent result support")
	ErrRefundTransactionMissing   = errors.New("commerce: automatic refund returned no transaction id")
	ErrRefundCorrelationAmbiguous = errors.New("commerce: refund webhook cannot be correlated unambiguously")
)

// adminTemplateDir holds the plugin's admin page fragments (rendered inside the
// core admin chrome via Handler.RenderExtensionPage).
var adminTemplateDir = filepath.Join("plugins", pluginSlug, "templates", "admin")

// registerOrderAdminRoutes wires the order back office. Every route is guarded by
// admin.RequirePermission (JWT + RBAC + same-origin CSRF for POSTs); reads need
// shop_order.read, status changes shop_order.update, refunds shop_order.refund.
func (p *Plugin) registerOrderAdminRoutes(r *gin.Engine) {
	auth, rbac := p.engine.Auth, p.engine.RBAC
	read := admin.RequirePermission(auth, rbac, "shop_order", "read")
	update := admin.RequirePermission(auth, rbac, "shop_order", "update")
	refund := admin.RequirePermission(auth, rbac, "shop_order", "refund")

	r.GET("/admin/commerce/orders", read, p.handleOrderList)
	r.GET("/admin/commerce/orders/:id", read, p.handleOrderDetail)
	r.POST("/admin/commerce/orders/:id/mark-paid", update, p.handleOrderMarkPaid)
	r.POST("/admin/commerce/orders/:id/status", update, p.handleOrderStatus)
	r.POST("/admin/commerce/orders/:id/note", update, p.handleOrderNote)
	r.POST("/admin/commerce/orders/:id/refund", refund, p.handleOrderRefund)
}

// orderRow is a list-view row with display-ready fields.
type orderRow struct {
	ID          uint
	Number      string
	StatusLabel string
	Status      string
	TotalStr    string
	Currency    string
	Email       string
	CreatedAt   string
}

func (p *Plugin) handleOrderList(c *gin.Context) {
	lang := p.adminLanguage()
	var orders []Order
	p.engine.DB.Order("id desc").Limit(200).Find(&orders)
	rows := make([]orderRow, 0, len(orders))
	for _, o := range orders {
		rows = append(rows, orderRow{
			ID: o.ID, Number: o.Number, Status: o.Status, StatusLabel: p.adminOrderStatusLabel(lang, o.Status),
			TotalStr: o.GrandTotalStr(), Currency: o.Currency, Email: o.Email,
			CreatedAt: o.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	p.renderAdmin(c, "orders", p.adminT(lang, "plugin.commerce.orders.title"), "plugin-commerce-orders", gin.H{"Orders": rows})
}

func (p *Plugin) handleOrderDetail(c *gin.Context) {
	lang := p.adminLanguage()
	order, ok := p.loadOrder(c)
	if !ok {
		return
	}
	var items []OrderItem
	p.engine.DB.Where("order_id = ?", order.ID).Order("id asc").Find(&items)
	var addresses []OrderAddress
	p.engine.DB.Where("order_id = ?", order.ID).Find(&addresses)
	var notes []OrderNote
	p.engine.DB.Where("order_id = ?", order.ID).Order("id desc").Find(&notes)
	var payments []Payment
	p.engine.DB.Where("order_id = ?", order.ID).Order("id asc").Find(&payments)
	var refunds []Refund
	p.engine.DB.Where("order_id = ?", order.ID).Order("id asc").Find(&refunds)

	billing, shipping := OrderAddress{}, OrderAddress{}
	for _, a := range addresses {
		if a.Type == "billing" {
			billing = a
		} else if a.Type == "shipping" {
			shipping = a
		}
	}

	refunded, _ := successfulRefundTotal(p.engine.DB, order.ID)
	pending, _ := reservedRefundTotal(p.engine.DB, order.ID, 0)
	remaining := order.GrandTotal - refunded - pending
	if remaining < 0 {
		remaining = 0
	}
	canRefund := (order.Status == OrderProcessing || order.Status == OrderCompleted || order.Status == OrderPartiallyRefunded || order.Status == OrderReconciliation) && remaining > 0
	refundKey := newRefundIdempotencyKey()
	retryAmount, retryReason := "", ""
	if candidate := strings.TrimSpace(c.Query("refund_key")); candidate != "" && len(candidate) <= 191 {
		var retry Refund
		if err := p.engine.DB.Where("order_id = ? AND idempotency_key = ?", order.ID, candidate).First(&retry).Error; err == nil {
			refundKey = candidate
			retryAmount = formatPrice(retry.Amount)
			retryReason = retry.Reason
			canRefund = true // permits retrying status sync even after full refund
		}
	}

	p.renderAdmin(c, "order-detail", p.adminT(lang, "plugin.commerce.order.title", order.Number), "plugin-commerce-orders", gin.H{
		"Order":       *order,
		"StatusLabel": p.adminOrderStatusLabel(lang, order.Status),
		"Items":       items,
		"Billing":     billing,
		"Shipping":    shipping,
		"Notes":       notes,
		"Payments":    payments,
		"Refunds":     refunds,
		"CanMarkPaid": order.Status == OrderPending || order.Status == OrderOnHold,
		"CanShip":     order.Status == OrderProcessing || order.Status == OrderPartiallyRefunded,
		"CanCancel":   order.Status == OrderPending || order.Status == OrderOnHold,
		"CanRefund":   canRefund,
		"RefundKey":   refundKey,
		"Refundable":  formatPrice(remaining),
		"RetryAmount": retryAmount,
		"RetryReason": retryReason,
	})
}

// handleOrderMarkPaid confirms an offline/manual payment by routing through the
// settler — the same idempotent path gateways use — so inventory commit, the
// state transition, and the confirmation email all fire consistently.
func (p *Plugin) handleOrderMarkPaid(c *gin.Context) {
	order, ok := p.loadOrder(c)
	if !ok {
		return
	}
	settler := corecommerce.GetSettler(p.engine.Hooks)
	lang := p.adminLanguage()
	if settler == nil {
		p.redirectOrder(c, order.ID, "", p.adminT(lang, "plugin.commerce.error.settler_unavailable"))
		return
	}
	err := settler.Settle(c.Request.Context(), corecommerce.SettleRequest{
		OrderRef: order.Number, Gateway: order.PaymentMethod, Status: corecommerce.SettlePaid,
		Amount:         corecommerce.New(order.GrandTotal, order.Currency),
		IdempotencyKey: "manual-paid:" + order.Number,
	})
	if err != nil {
		p.redirectOrder(c, order.ID, "", p.adminT(lang, "plugin.commerce.error.mark_paid_failed", err.Error()))
		return
	}
	p.redirectOrder(c, order.ID, p.adminT(lang, "plugin.commerce.notice.marked_paid"), "")
}

// handleOrderStatus applies a ship/cancel transition. Cancel releases the held
// stock in the same transaction.
func (p *Plugin) handleOrderStatus(c *gin.Context) {
	lang := p.adminLanguage()
	order, ok := p.loadOrder(c)
	if !ok {
		return
	}
	event := c.PostForm("event")
	if event != EventShip && event != EventCancel {
		p.redirectOrder(c, order.ID, "", p.adminT(lang, "plugin.commerce.error.unsupported_action"))
		return
	}
	var change *OrderStatusChange
	err := p.engine.DB.Transaction(func(tx *gorm.DB) error {
		var fresh Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fresh, "id = ?", order.ID).Error; err != nil {
			return err
		}
		if event == EventCancel {
			pending, err := reservedRefundTotal(tx, fresh.ID, 0)
			if err != nil {
				return err
			}
			if pending > 0 {
				return ErrRefundInProgress
			}
		}
		var err error
		change, err = p.orders().Transition(c.Request.Context(), tx, &fresh, event, p.adminActor(c), "")
		if err != nil {
			return err
		}
		if event == EventCancel {
			var items []OrderItem
			if err := tx.Where("order_id = ?", fresh.ID).Find(&items).Error; err != nil {
				return err
			}
			for _, it := range items {
				if err := p.inventory().Release(tx, it.ProductContentID, it.Qty, fresh.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		p.redirectOrder(c, order.ID, "", p.adminT(lang, "plugin.commerce.error.action_failed", err.Error()))
		return
	}
	p.orders().PublishStatusChange(c.Request.Context(), change)
	p.redirectOrder(c, order.ID, p.adminT(lang, "plugin.commerce.notice.status_updated"), "")
}

func (p *Plugin) handleOrderNote(c *gin.Context) {
	lang := p.adminLanguage()
	order, ok := p.loadOrder(c)
	if !ok {
		return
	}
	note := c.PostForm("note")
	if note == "" {
		p.redirectOrder(c, order.ID, "", p.adminT(lang, "plugin.commerce.error.note_required"))
		return
	}
	_ = p.orders().AddNote(p.engine.DB, order.ID, p.adminActor(c), note, c.PostForm("is_customer") == "1")
	p.redirectOrder(c, order.ID, p.adminT(lang, "plugin.commerce.notice.note_added"), "")
}

// handleOrderRefund reserves refund capacity under an order row lock, performs
// the external refund with an idempotency key, then marks the row succeeded or
// failed and advances status from the cumulative succeeded total.
func (p *Plugin) handleOrderRefund(c *gin.Context) {
	lang := p.adminLanguage()
	order, ok := p.loadOrder(c)
	if !ok {
		return
	}
	amount := parsePrice(c.PostForm("amount"))
	if amount <= 0 {
		p.redirectOrder(c, order.ID, "", p.adminT(lang, "plugin.commerce.error.refund_amount_invalid"))
		return
	}
	reason := strings.TrimSpace(c.PostForm("reason"))
	key := strings.TrimSpace(c.PostForm("idempotency_key"))
	if key == "" {
		key = newRefundIdempotencyKey()
	}
	if len(key) > 191 {
		p.redirectOrder(c, order.ID, "", p.adminT(lang, "plugin.commerce.error.refund_key_invalid"))
		return
	}

	gateway := p.checkout().registeredGatewayByID(order.PaymentMethod)
	if err := validateRefundGateway(gateway); err != nil {
		p.redirectOrder(c, order.ID, "", p.refundErrorMessage(lang, err))
		return
	}
	allowPartial := !gateway.Capabilities().Refund || gateway.Capabilities().PartialRefund
	attempt, err := p.prepareRefund(order.ID, amount, reason, key, allowPartial)
	if err != nil {
		p.redirectOrder(c, order.ID, "", p.refundErrorMessage(lang, err))
		return
	}
	result := corecommerce.RefundResult{}
	if attempt.Refund.Status != RefundSucceeded && gateway.Capabilities().Refund {
		req := corecommerce.RefundRequest{
			OrderRef: attempt.Order.Number, PaymentID: attempt.Payment.TxnID,
			Amount: corecommerce.New(attempt.Refund.Amount, attempt.Refund.Currency), Reason: attempt.Refund.Reason,
			IdempotencyKey: key,
		}
		refunder := gateway.(corecommerce.IdempotentRefunder) // validated below
		result, err = refunder.RefundWithResult(c, req)
		if err == nil {
			err = validateAutomaticRefundResult(gateway, result)
		}
	}
	if err != nil {
		_ = p.failRefund(attempt.Refund.ID, err)
		p.redirectRefundError(c, order.ID, key, p.adminT(lang, "plugin.commerce.error.gateway_refund_failed", err.Error()))
		return
	}
	if err := p.completeRefund(c.Request.Context(), attempt.Refund.ID, result, p.adminActor(c)); err != nil {
		p.redirectRefundError(c, order.ID, key, p.adminT(lang, "plugin.commerce.error.refund_sync_failed", err.Error()))
		return
	}
	p.redirectOrder(c, order.ID, p.adminT(lang, "plugin.commerce.notice.refund_recorded"), "")
}

// validateRefundGateway prevents a historical automated payment method from
// silently degrading to a local-only refund when its plugin is disabled or
// missing. A registered gateway that explicitly advertises Refund=false is a
// deliberate manual/offline workflow and remains recordable.
func validateRefundGateway(gateway corecommerce.PaymentGateway) error {
	if gateway == nil {
		return ErrRefundGatewayUnavailable
	}
	if gateway.Capabilities().Refund {
		if _, ok := gateway.(corecommerce.IdempotentRefunder); !ok {
			return ErrRefundIdempotencyRequired
		}
	}
	return nil
}

func validateAutomaticRefundResult(gateway corecommerce.PaymentGateway, result corecommerce.RefundResult) error {
	if gateway != nil && gateway.Capabilities().Refund && strings.TrimSpace(result.TransactionID) == "" {
		return ErrRefundTransactionMissing
	}
	return nil
}

type refundAttempt struct {
	Order   Order
	Payment Payment
	Refund  Refund
}

// prepareRefund serializes on the order row, checks the succeeded cumulative
// amount plus all in-flight reservations, then inserts/refreshes a pending row.
func (p *Plugin) prepareRefund(orderID uint, amount int64, reason, key string, allowPartial bool) (*refundAttempt, error) {
	attempt := &refundAttempt{}
	err := p.engine.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&attempt.Order, "id = ?", orderID).Error; err != nil {
			return err
		}
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("idempotency_key = ?", key).First(&attempt.Refund).Error
		if err == nil {
			if attempt.Refund.OrderID != orderID || attempt.Refund.Amount != amount || attempt.Refund.Currency != attempt.Order.Currency {
				return ErrRefundIdempotencyConflict
			}
			if attempt.Refund.Status == RefundSucceeded {
				return nil
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if attempt.Order.Status != OrderProcessing && attempt.Order.Status != OrderCompleted && attempt.Order.Status != OrderPartiallyRefunded && attempt.Order.Status != OrderReconciliation {
			return ErrIllegalTransition
		}

		succeeded, err := successfulRefundTotal(tx, orderID)
		if err != nil {
			return err
		}
		pending, err := reservedRefundTotal(tx, orderID, attempt.Refund.ID)
		if err != nil {
			return err
		}
		remaining := attempt.Order.GrandTotal - succeeded - pending
		if amount <= 0 {
			return ErrRefundAmount
		}
		if amount > remaining {
			return ErrRefundExceedsRemaining
		}
		if amount < remaining && !allowPartial {
			return errors.New("commerce: gateway does not support partial refunds")
		}

		if attempt.Refund.PaymentID != 0 {
			if err := tx.First(&attempt.Payment, "id = ? AND order_id = ?", attempt.Refund.PaymentID, orderID).Error; err != nil {
				return err
			}
		} else {
			// Prefer the capture/settled payment; offline payments legitimately have
			// an empty transaction id, so fall back to the newest payment row.
			err = tx.Where("order_id = ? AND txn_id <> '' AND status IN ?", orderID,
				[]string{string(corecommerce.SettlePaid), string(corecommerce.SettleOverpaid)}).
				Order("id desc").First(&attempt.Payment).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = tx.Where("order_id = ?", orderID).Order("id desc").First(&attempt.Payment).Error
			}
			if err != nil {
				return err
			}
		}

		now := time.Now().UTC()
		if attempt.Refund.ID == 0 {
			attempt.Refund = Refund{
				OrderID: orderID, PaymentID: attempt.Payment.ID, Amount: amount,
				Currency: attempt.Order.Currency, Reason: reason, Status: RefundPending,
				IdempotencyKey: &key, Gateway: attempt.Payment.Gateway, CreatedAt: now, UpdatedAt: now,
			}
			return tx.Create(&attempt.Refund).Error
		}
		attempt.Refund.Gateway = attempt.Payment.Gateway
		attempt.Refund.Status = RefundPending
		attempt.Refund.Error = ""
		attempt.Refund.UpdatedAt = now
		return tx.Save(&attempt.Refund).Error
	})
	return attempt, err
}

func (p *Plugin) failRefund(refundID uint, cause error) error {
	return p.engine.DB.Transaction(func(tx *gorm.DB) error {
		var refund Refund
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&refund, "id = ?", refundID).Error; err != nil {
			return err
		}
		if refund.Status == RefundSucceeded {
			return nil
		}
		return tx.Model(&refund).Updates(map[string]interface{}{
			"status": RefundFailed, "error": cause.Error(), "updated_at": time.Now().UTC(),
		}).Error
	})
}

// completeRefund first commits the remote money fact, then synchronizes the
// order state in a separate transaction. A status/note failure can therefore
// never roll back evidence that the provider already refunded the buyer.
func (p *Plugin) completeRefund(ctx context.Context, refundID uint, result corecommerce.RefundResult, actor string) error {
	var identity Refund
	if err := p.engine.DB.Select("id", "order_id").First(&identity, "id = ?", refundID).Error; err != nil {
		return err
	}
	err := p.engine.DB.Transaction(func(tx *gorm.DB) error {
		// Match prepareRefund's lock order (order → refund) to avoid deadlocks.
		var order Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", identity.OrderID).Error; err != nil {
			return err
		}
		var refund Refund
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&refund, "id = ?", refundID).Error; err != nil {
			return err
		}
		gateway := strings.TrimSpace(refund.Gateway)
		if gateway == "" {
			var payment Payment
			if err := tx.Select("id", "gateway").First(&payment, "id = ? AND order_id = ?", refund.PaymentID, order.ID).Error; err != nil {
				return err
			}
			gateway = strings.TrimSpace(payment.Gateway)
		}
		if result.TransactionID != "" {
			var duplicate Refund
			err := tx.Where("gateway = ? AND gateway_refund_id = ? AND id <> ?", gateway, result.TransactionID, refund.ID).First(&duplicate).Error
			switch {
			case err == nil:
				// One provider refund can fund exactly one local refund fact. A
				// second local idempotency key must never make it count twice.
				return ErrRefundIdempotencyConflict
			case errors.Is(err, gorm.ErrRecordNotFound):
				// Continue with the first use of this provider id.
			default:
				return err
			}
		}
		if refund.Status == RefundSucceeded {
			if refund.GatewayRefundID != "" && result.TransactionID != "" && refund.GatewayRefundID != result.TransactionID {
				return ErrRefundIdempotencyConflict
			}
			updates := map[string]interface{}{}
			if refund.Gateway != gateway {
				updates["gateway"] = gateway
			}
			if refund.GatewayRefundID == "" && result.TransactionID != "" {
				updates["gateway_refund_id"] = result.TransactionID
				updates["raw"] = marshalRaw(result.Raw)
			}
			if len(updates) > 0 {
				updates["updated_at"] = time.Now().UTC()
				return tx.Model(&refund).Updates(updates).Error
			}
			return nil
		}
		already, err := successfulRefundTotal(tx, order.ID)
		if err != nil {
			return err
		}
		if refund.Amount > order.GrandTotal-already {
			return ErrRefundExceedsRemaining
		}
		if err := tx.Model(&refund).Updates(map[string]interface{}{
			"status": RefundSucceeded, "gateway": gateway, "gateway_refund_id": result.TransactionID,
			"raw": marshalRaw(result.Raw), "error": "", "updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	if err := p.syncRefundOrderStatus(ctx, identity.OrderID, actor); err != nil {
		note := "退款已由网关完成，但订单状态同步失败，需人工核对：" + err.Error()
		if noteErr := p.orders().AddNote(p.engine.DB, identity.OrderID, actor, note, false); noteErr != nil {
			logger.Error("commerce: refund status reconciliation note failed", "order_id", identity.OrderID, "error", noteErr)
		}
		return fmt.Errorf("%w: %v", ErrRefundStatusSync, err)
	}
	return nil
}

func (p *Plugin) syncRefundOrderStatus(ctx context.Context, orderID uint, actor string) error {
	var change *OrderStatusChange
	err := p.engine.DB.Transaction(func(tx *gorm.DB) error {
		var order Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", orderID).Error; err != nil {
			return err
		}
		refunded, err := successfulRefundTotal(tx, order.ID)
		if err != nil {
			return err
		}
		event := refundEvent(refunded, order.GrandTotal)
		if event == EventPartialRefund {
			// Keep the fulfillment state (processing/completed) intact. The Refund
			// row itself is the idempotent financial/audit source of truth, so no
			// status hook or replayable note is needed here.
			return nil
		}
		if order.Status == OrderRefunded {
			return nil
		}
		target, allowed := nextStatus(order.Status, event)
		if !allowed {
			return fmt.Errorf("%w: %s cannot apply %s", ErrIllegalTransition, order.Status, event)
		}
		change, err = p.orders().Transition(ctx, tx, &order, event, actor,
			fmt.Sprintf("累计退款 %s %s，订单状态更新为 %s", formatPrice(refunded), order.Currency, target))
		return err
	})
	if err != nil {
		return err
	}
	p.orders().PublishStatusChange(ctx, change)
	return nil
}

func (p *Plugin) refundErrorMessage(lang string, err error) string {
	switch {
	case errors.Is(err, ErrRefundAmount), errors.Is(err, ErrRefundExceedsRemaining):
		return p.adminT(lang, "plugin.commerce.error.refund_exceeds_remaining")
	case errors.Is(err, ErrRefundIdempotencyConflict):
		return p.adminT(lang, "plugin.commerce.error.refund_conflict")
	case errors.Is(err, ErrIllegalTransition):
		return p.adminT(lang, "plugin.commerce.error.refund_status")
	case errors.Is(err, ErrRefundGatewayUnavailable):
		return p.adminT(lang, "plugin.commerce.error.refund_gateway_unavailable")
	case errors.Is(err, ErrRefundIdempotencyRequired):
		return p.adminT(lang, "plugin.commerce.error.refund_idempotency")
	default:
		return p.adminT(lang, "plugin.commerce.error.refund_failed", err.Error())
	}
}

func newRefundIdempotencyKey() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "refund:" + hex.EncodeToString(b)
}

// loadOrder fetches the :id order or writes a 404 and returns ok=false.
func (p *Plugin) loadOrder(c *gin.Context) (*Order, bool) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	var order Order
	if err := p.engine.DB.First(&order, "id = ?", uint(id)).Error; err != nil {
		c.String(http.StatusNotFound, p.adminT(p.adminLanguage(), "plugin.commerce.error.order_not_found"))
		return nil, false
	}
	return &order, true
}

func (p *Plugin) renderAdmin(c *gin.Context, fragment, title, active string, data gin.H) {
	data["ExtensionCatalog"] = commerceAdminCatalog
	path := filepath.Join(adminTemplateDir, fragment+".tmpl")
	if err := p.engine.Admin.RenderExtensionPage(c, path, title, active, data); err != nil {
		c.String(http.StatusInternalServerError, "admin page unavailable")
	}
}

func (p *Plugin) redirectOrder(c *gin.Context, id uint, success, errMsg string) {
	dest := fmt.Sprintf("/admin/commerce/orders/%d", id)
	switch {
	case errMsg != "":
		dest += "?error=" + url.QueryEscape(errMsg)
	case success != "":
		dest += "?success=" + url.QueryEscape(success)
	}
	c.Redirect(http.StatusFound, dest)
}

func (p *Plugin) redirectRefundError(c *gin.Context, id uint, key, errMsg string) {
	dest := fmt.Sprintf("/admin/commerce/orders/%d?error=%s&refund_key=%s", id,
		url.QueryEscape(errMsg), url.QueryEscape(key))
	c.Redirect(http.StatusFound, dest)
}

func (p *Plugin) adminActor(c *gin.Context) string {
	if name := c.GetString("admin_username"); name != "" {
		return name
	}
	return "admin"
}
