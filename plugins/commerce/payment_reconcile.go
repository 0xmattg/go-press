package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	corecommerce "github.com/0xmattg/go-press/core/commerce"
	"github.com/0xmattg/go-press/pkg/logger"
)

const paymentReconcileInterval = time.Minute

// registerPaymentReconciler starts the pull-confirmation loop after gateways
// have registered. It is plugin-owned because plugins may activate after Core's
// scheduler has already started; Deactivate closes the matching stop channel.
func (p *Plugin) registerPaymentReconciler() {
	if p.reconcileStop != nil {
		return
	}
	stop := make(chan struct{})
	p.reconcileStop = stop
	go func() {
		ticker := time.NewTicker(paymentReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := p.reconcilePendingPayments(context.Background()); err != nil {
					logger.Error("commerce: payment reconciliation failed", "error", err)
				}
			}
		}
	}()
}

type pendingPaymentRow struct {
	Payment
	OrderRef    string
	OrderStatus string
}

// reconcilePendingPayments groups unresolved local intents by gateway, calls
// each optional Reconciler once, then feeds its results through the same
// idempotent PaymentSettler used by webhooks and manual confirmation.
func (p *Plugin) reconcilePendingPayments(ctx context.Context) error {
	if p == nil || p.engine == nil || p.engine.DB == nil || p.engine.Hooks == nil {
		return nil
	}
	settler := corecommerce.GetSettler(p.engine.Hooks)
	if settler == nil {
		return errors.New("commerce: payment settler unavailable")
	}

	var rows []pendingPaymentRow
	paymentTable, orderTable := tbl("payments"), tbl("orders")
	err := p.engine.DB.Table(paymentTable+" AS payment").
		Select("payment.*, orders.number AS order_ref, orders.status AS order_status").
		Joins("JOIN "+orderTable+" AS orders ON orders.id = payment.order_id").
		Where("payment.status IN ?", []string{OrderPending, paymentReconciliationState}).
		Where("orders.status IN ?", []string{OrderPending, OrderOnHold, OrderReconciliation}).
		Order("payment.id ASC").Limit(500).Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("commerce: query pending payments: %w", err)
	}

	byGateway := make(map[string][]corecommerce.PendingPayment)
	for _, row := range rows {
		var record storedCheckoutAction
		_ = json.Unmarshal([]byte(row.Raw), &record)
		paymentContext := map[string]any{}
		if len(row.Raw) > 0 {
			_ = json.Unmarshal([]byte(row.Raw), &paymentContext)
		}
		if len(record.ClientData) > 0 {
			paymentContext = record.ClientData
		}
		byGateway[row.Gateway] = append(byGateway[row.Gateway], corecommerce.PendingPayment{
			OrderRef: row.OrderRef,
			Amount:   corecommerce.New(row.Amount, row.Currency), Context: paymentContext,
			CreatedAt: row.CreatedAt, ExpiresAt: record.ExpiresAt,
		})
	}

	var reconcileErrs []error
	for _, gateway := range corecommerce.PaymentGateways(p.engine.Hooks) {
		reconciler, ok := gateway.(corecommerce.Reconciler)
		if !ok || len(byGateway[gateway.ID()]) == 0 {
			continue
		}
		for _, result := range reconciler.ReconcilePending(ctx, byGateway[gateway.ID()]) {
			if result.Gateway == "" {
				result.Gateway = gateway.ID()
			}
			if result.Gateway != gateway.ID() {
				reconcileErrs = append(reconcileErrs, fmt.Errorf("commerce: reconciler %s returned gateway %s", gateway.ID(), result.Gateway))
				continue
			}
			if err := settler.Settle(ctx, result); err != nil {
				reconcileErrs = append(reconcileErrs, fmt.Errorf("commerce: reconcile %s: %w", result.OrderRef, err))
			}
		}
	}
	return errors.Join(reconcileErrs...)
}
