package commerce

import (
	"context"
	"strconv"
	"time"

	"go-press/pkg/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// sweepInterval is how often abandoned pending orders are swept. The reservation
// TTL itself is configurable (plugin_commerce_reservation_ttl_minutes).
const sweepInterval = 5 * time.Minute

// registerReservationSweeper starts the background loop that cancels abandoned
// pending orders and releases their held stock. It runs as a plugin-owned
// goroutine (not core's Scheduler, whose tickers only start at boot — commerce
// activates later) and is stopped on Deactivate via sweepStop.
func (p *Plugin) registerReservationSweeper() {
	if p.sweepStop != nil {
		return // already running
	}
	stop := make(chan struct{})
	p.sweepStop = stop
	go func() {
		t := time.NewTicker(sweepInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				p.releaseStaleReservations()
			}
		}
	}()
}

// reservationTTL is how long a pending (unpaid, redirect/inline) order holds its
// stock before being auto-cancelled. on_hold offline orders are excluded — bank
// transfers legitimately take days and are resolved by an admin.
func (p *Plugin) reservationTTL() time.Duration {
	mins, _ := strconv.Atoi(p.opt("plugin_commerce_reservation_ttl_minutes", "60"))
	if mins <= 0 {
		mins = 60
	}
	return time.Duration(mins) * time.Minute
}

// releaseStaleReservations cancels pending orders older than the TTL and returns
// their reserved stock, each in its own transaction so one bad order can't block
// the rest.
func (p *Plugin) releaseStaleReservations() {
	if p.engine == nil || p.engine.DB == nil {
		return
	}
	cutoff := time.Now().UTC().Add(-p.reservationTTL())
	var stale []Order
	if err := p.engine.DB.Where("status = ? AND created_at < ?", OrderPending, cutoff).
		Limit(100).Find(&stale).Error; err != nil {
		logger.Error("commerce: sweep query failed", "error", err)
		return
	}
	for i := range stale {
		order := stale[i]
		var change *OrderStatusChange
		err := p.engine.DB.Transaction(func(tx *gorm.DB) error {
			// Re-check status under a row lock to avoid racing a settlement.
			var fresh Order
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ?", order.ID).First(&fresh).Error; err != nil {
				return err
			}
			if fresh.Status != OrderPending {
				return nil
			}
			var reconciling int64
			if err := tx.Model(&Payment{}).Where("order_id = ? AND status = ?", fresh.ID, paymentReconciliationState).
				Count(&reconciling).Error; err != nil {
				return err
			}
			if reconciling > 0 {
				// A lost/ambiguous StartPayment response may still settle remotely.
				// Keep its reservation until a gateway event or an operator resolves it.
				return nil
			}
			var err error
			change, err = p.orders().Transition(context.Background(), tx, &fresh, EventCancel, "system", "超时未支付，自动取消")
			if err != nil {
				return err
			}
			var items []OrderItem
			if err := tx.Where("order_id = ?", fresh.ID).Find(&items).Error; err != nil {
				return err
			}
			for _, it := range items {
				if err := p.inventory().Release(tx, it.ProductContentID, it.Qty, fresh.ID); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			logger.Error("commerce: release stale reservation failed", "order", order.Number, "error", err)
			continue
		}
		if change != nil {
			p.orders().PublishStatusChange(context.Background(), change)
			logger.Info("commerce: cancelled stale order", "order", order.Number)
		}
	}
}
