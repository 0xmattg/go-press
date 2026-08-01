package commerce

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go-press/core/mail"
	"go-press/pkg/logger"
)

// onOrderStatusChanged is the commerce.order.status_changed listener. It sends
// the buyer a confirmation email when an order becomes paid (→ processing). Kept
// decoupled from the state machine so mail delivery never blocks a transition.
func (p *Plugin) onOrderStatusChanged(_ context.Context, args ...interface{}) {
	if len(args) < 3 {
		return
	}
	order, ok := args[0].(*Order)
	if !ok || order == nil {
		return
	}
	newStatus, _ := args[2].(string)
	if newStatus != OrderProcessing {
		return
	}
	p.sendOrderConfirmation(order)
}

// sendOrderConfirmation emails the buyer their paid-order confirmation,
// asynchronously via the worker pool (mirrors core's contact-message notifier).
func (p *Plugin) sendOrderConfirmation(order *Order) {
	if p.engine == nil || p.engine.Mail == nil || order == nil || strings.TrimSpace(order.Email) == "" {
		return
	}
	msg := mail.Message{
		To:      []string{order.Email},
		Subject: fmt.Sprintf("[%s] 订单 %s 已确认付款", p.siteName(), order.Number),
		Text:    p.orderConfirmationBody(order),
	}
	send := func(ctx context.Context) error {
		if err := p.engine.Mail.Send(ctx, msg); err != nil && !errors.Is(err, mail.ErrDisabled) {
			logger.Error("commerce: order confirmation email failed", "order", order.Number, "error", err)
		}
		return nil
	}
	if p.engine.Workers != nil {
		p.engine.Workers.SubmitFunc("mail:order_confirmation", send)
		return
	}
	_ = send(context.Background())
}

func (p *Plugin) orderConfirmationBody(order *Order) string {
	var b strings.Builder
	fmt.Fprintf(&b, "感谢您的订购！\n\n订单号：%s\n状态：已付款\n\n", order.Number)
	var items []OrderItem
	p.engine.DB.Where("order_id = ?", order.ID).Order("id asc").Find(&items)
	for _, it := range items {
		fmt.Fprintf(&b, "  %s × %d — %s %s\n", it.NameSnapshot, it.Qty, formatPrice(it.LineTotal), order.Currency)
	}
	b.WriteString("\n")
	if order.ShippingTotal > 0 {
		fmt.Fprintf(&b, "运费：%s %s\n", formatPrice(order.ShippingTotal), order.Currency)
	}
	fmt.Fprintf(&b, "合计：%s %s\n", formatPrice(order.GrandTotal), order.Currency)
	if url := strings.TrimRight(p.siteURL(), "/"); url != "" {
		fmt.Fprintf(&b, "\n查看订单：%s/checkout/complete/%s?key=%s\n", url, order.Number, order.AccessKey)
	}
	return b.String()
}

func (p *Plugin) siteName() string {
	if p.engine != nil && p.engine.Config != nil && strings.TrimSpace(p.engine.Config.Site.Name) != "" {
		return p.engine.Config.Site.Name
	}
	return "GoPress"
}
