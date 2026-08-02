package commerceusdt

import (
	"context"
	"math/big"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	corecommerce "go-press/core/commerce"
	"go-press/pkg/logger"
)

const (
	watchInterval   = 30 * time.Second // how often the chain is polled
	maxScanSpan     = 2000             // max blocks per eth_getLogs call
	initialLookback = 300              // blocks to look back on first-ever scan
)

// startWatcher launches the pull-based confirmation loop as a plugin-owned
// goroutine (mirrors commerce's reservation sweeper — not core's Scheduler,
// whose tickers start at boot while this default-inactive module activates later).
func (p *Plugin) startWatcher() {
	if p.watchStop != nil {
		return
	}
	stop := make(chan struct{})
	p.watchStop = stop
	go func() {
		t := time.NewTicker(watchInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				p.watchTick()
			}
		}
	}()
}

func (p *Plugin) stopWatcher() {
	if p.watchStop != nil {
		close(p.watchStop)
		p.watchStop = nil
	}
}

// watchTick scans the configured chain once and finalizes expired invoices.
func (p *Plugin) watchTick() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("commerce-usdt: watcher panic", "recover", r)
		}
	}()
	cfg := p.loadConfig()
	if !cfg.ready() {
		return
	}
	chain := p.buildChain(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p.scanChain(ctx, cfg, chain)
	p.finalizeExpired(ctx, cfg, chain)
}

// scanChain fetches new confirmed deposits to active invoice addresses and
// settles any that reached the expected amount. Because scanning is bounded to
// blocks at least `confirmations` deep, every deposit found is already confirmed.
func (p *Plugin) scanChain(ctx context.Context, cfg config, chain *evmChain) {
	invs, err := p.activeInvoices(chain.ID())
	if err != nil {
		logger.Error("commerce-usdt: list active invoices failed", "error", err)
		return
	}
	if len(invs) == 0 {
		return
	}
	latest, err := chain.LatestBlock(ctx)
	if err != nil {
		logger.Error("commerce-usdt: latest block failed", "error", err)
		return
	}
	confs := chain.Confirmations()
	if latest < confs {
		return
	}
	safe := latest - confs

	from, to, ok := scanRange(p.cursor(chain.ID()), safe)
	if !ok {
		return
	}

	byAddr := make(map[string]*Invoice, len(invs))
	addrs := make([]string, 0, len(invs))
	for i := range invs {
		byAddr[strings.ToLower(invs[i].Address)] = &invs[i]
		addrs = append(addrs, invs[i].Address)
	}

	deposits, err := chain.ScanTransfers(ctx, addrs, from, to)
	if err != nil {
		logger.Error("commerce-usdt: scan transfers failed", "error", err, "from", from, "to", to)
		return // do not advance cursor on error
	}
	for _, d := range deposits {
		inv := byAddr[strings.ToLower(d.To)]
		if inv == nil {
			continue
		}
		p.recordDeposit(ctx, chain, inv, d, safe)
	}
	p.setCursor(chain.ID(), to)
}

// recordDeposit stores a confirmed deposit (idempotently) and re-evaluates the
// invoice for settlement.
func (p *Plugin) recordDeposit(ctx context.Context, chain *evmChain, inv *Invoice, d Deposit, safe uint64) {
	confs := uint64(0)
	if safe >= d.BlockNumber {
		confs = safe - d.BlockNumber + chain.Confirmations()
	}
	row := DepositRow{
		InvoiceID: inv.ID, Chain: chain.ID(), TxHash: d.TxHash, LogIndex: d.LogIndex,
		FromAddr: d.From, TokenAmount: d.TokenAmount.String(), BlockNumber: d.BlockNumber,
		Confirmations: confs, SeenAt: time.Now().UTC(),
	}
	res := p.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "chain"}, {Name: "tx_hash"}, {Name: "log_index"}},
		DoNothing: true,
	}).Create(&row)
	if res.Error != nil {
		logger.Error("commerce-usdt: record deposit failed", "error", res.Error)
		return
	}
	p.reevaluate(ctx, chain, inv)
}

// reevaluate recomputes an invoice's confirmed received total and settles it as
// paid once it meets the expected amount (within the dust tolerance).
func (p *Plugin) reevaluate(ctx context.Context, chain *evmChain, inv *Invoice) {
	var rows []DepositRow
	if err := p.db.Where("invoice_id = ?", inv.ID).Find(&rows).Error; err != nil {
		return
	}
	received := big.NewInt(0)
	for _, r := range rows {
		if v, ok := new(big.Int).SetString(r.TokenAmount, 10); ok {
			received.Add(received, v)
		}
	}
	expected, _ := new(big.Int).SetString(inv.ExpectedToken, 10)
	if expected == nil {
		expected = big.NewInt(0)
	}
	dust := p.loadConfig().DustTolerance

	p.db.Model(&Invoice{}).Where("id = ? AND status IN ?", inv.ID, []string{invPending, invSeen}).
		Updates(map[string]interface{}{"received_token": received.String(), "status": invSeen})

	if inv.Status != invPaid && withinTolerance(received, expected, dust) {
		p.settle(ctx, inv, received, corecommerce.SettlePaid, invPaid, firstTx(rows))
	}
}

// finalizeExpired closes out invoices past their window: zero received → expired
// (commerce cancels + releases stock); partial → underpaid (commerce keeps it
// on hold for manual handling, never silently pocketing funds).
func (p *Plugin) finalizeExpired(ctx context.Context, cfg config, chain *evmChain) {
	invs, err := p.expiredInvoices(chain.ID(), time.Now().UTC())
	if err != nil {
		return
	}
	for i := range invs {
		inv := &invs[i]
		var rows []DepositRow
		p.db.Where("invoice_id = ?", inv.ID).Find(&rows)
		received := big.NewInt(0)
		for _, r := range rows {
			if v, ok := new(big.Int).SetString(r.TokenAmount, 10); ok {
				received.Add(received, v)
			}
		}
		expected, _ := new(big.Int).SetString(inv.ExpectedToken, 10)
		if expected == nil {
			expected = big.NewInt(0)
		}
		if withinTolerance(received, expected, cfg.DustTolerance) {
			// Full amount arrived right at the deadline — settle as paid.
			p.settle(ctx, inv, received, corecommerce.SettlePaid, invPaid, firstTx(rows))
			continue
		}
		if received.Sign() > 0 {
			p.settle(ctx, inv, received, corecommerce.SettleUnderpaid, invUnderpaid, firstTx(rows))
		} else {
			p.settle(ctx, inv, received, corecommerce.SettleExpired, invExpired, "")
		}
	}
}

// settle reports a terminal outcome to commerce (idempotent) and marks the
// invoice. The idempotency key is per-invoice-per-status so a repeated tick or a
// restart can never advance the order twice.
func (p *Plugin) settle(ctx context.Context, inv *Invoice, received *big.Int, status corecommerce.SettleStatus, invStatus, txn string) {
	settler := corecommerce.GetSettler(p.hooks)
	if settler == nil {
		return
	}
	key := "usdt:" + inv.Chain + ":" + string(status) + ":" + inv.OrderRef
	err := settler.Settle(ctx, corecommerce.SettleRequest{
		OrderRef:       inv.OrderRef,
		Gateway:        gatewayID,
		TxnID:          txn,
		Amount:         corecommerce.New(inv.USDMinor, "USD"),
		Status:         status,
		IdempotencyKey: key,
		Raw: map[string]any{
			"chain":          inv.Chain,
			"address":        inv.Address,
			"expected_token": inv.ExpectedToken,
			"received_token": received.String(),
		},
	})
	if err != nil {
		logger.Error("commerce-usdt: settle failed", "order", inv.OrderRef, "status", string(status), "error", err)
		return
	}
	now := time.Now().UTC()
	p.db.Model(&Invoice{}).Where("id = ?", inv.ID).Updates(map[string]interface{}{
		"status": invStatus, "received_token": received.String(), "settled_at": &now,
	})
	logger.Info("commerce-usdt: settled", "order", inv.OrderRef, "status", string(status))
}

// scanRange computes the [from,to] block window to scan given the persisted
// cursor and the current safe (confirmed) head. Pure, so it is unit-testable.
// A zero cursor (first-ever scan) starts a bounded lookback rather than genesis;
// the span is clamped to maxScanSpan. ok is false when there is nothing new.
func scanRange(cursor, safe uint64) (from, to uint64, ok bool) {
	if cursor == 0 {
		if safe > initialLookback {
			from = safe - initialLookback
		} else {
			from = 1
		}
	} else {
		from = cursor + 1
	}
	if from > safe {
		return 0, 0, false
	}
	to = safe
	if to-from > maxScanSpan {
		to = from + maxScanSpan
	}
	return from, to, true
}

func firstTx(rows []DepositRow) string {
	if len(rows) > 0 {
		return rows[0].TxHash
	}
	return ""
}

// cursor / setCursor persist the last safely-scanned block per chain.
func (p *Plugin) cursor(chain string) uint64 {
	var cur ChainCursor
	if err := p.db.Where("chain = ?", chain).First(&cur).Error; err != nil {
		return 0
	}
	return cur.LastScannedBlock
}

func (p *Plugin) setCursor(chain string, block uint64) {
	p.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "chain"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_scanned_block", "updated_at"}),
	}).Create(&ChainCursor{Chain: chain, LastScannedBlock: block, UpdatedAt: time.Now().UTC()})
}
