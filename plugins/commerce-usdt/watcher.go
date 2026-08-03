package commerceusdt

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	corecommerce "go-press/core/commerce"
	"go-press/pkg/logger"
)

const (
	watchInterval = 30 * time.Second
	maxScanSpan   = 2000
	// Covers more than the seven-day late-payment retention on Ethereum at its
	// normal block cadence, including margin for variance and upgrade recovery.
	initialLookback    = 60_000
	addressBatchSize   = 200
	lateWatchRetention = 7 * 24 * time.Hour
)

type observedDeposit struct {
	invoiceID uint
	deposit   Deposit
}

func (p *Plugin) startWatcher() {
	p.watchMu.Lock()
	if p.watchStop != nil {
		p.watchMu.Unlock()
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	p.watchStop = stop
	p.watchDone = done
	p.watchMu.Unlock()

	go func() {
		defer close(done)
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
	p.watchMu.Lock()
	stop, done := p.watchStop, p.watchDone
	p.watchStop, p.watchDone = nil, nil
	if stop != nil {
		close(stop)
	}
	p.watchMu.Unlock()
	if done != nil {
		<-done
	}
}

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
	if err := chain.VerifyConfiguration(ctx); err != nil {
		logger.Error("commerce-usdt: runtime RPC identity verification failed", "error", err)
		return
	}

	scannedTo, err := p.scanChain(ctx, cfg, chain)
	if err != nil {
		logger.Error("commerce-usdt: chain scan failed", "error", err)
		return
	}
	if scannedTo == 0 {
		return
	}
	safeTime, err := chain.BlockTimestamp(ctx, scannedTo)
	if err != nil {
		logger.Error("commerce-usdt: safe block timestamp failed", "block", scannedTo, "error", err)
		return
	}
	if err := p.finalizeExpired(ctx, cfg, chain, safeTime); err != nil {
		logger.Error("commerce-usdt: finalize expired invoices failed", "error", err)
	}
}

// scanChain fetches confirmed logs outside the database transaction, validates
// and timestamps them, then atomically persists the whole batch together with
// the cursor. A failed insert can therefore never skip money permanently.
func (p *Plugin) scanChain(ctx context.Context, cfg config, chain *evmChain) (uint64, error) {
	networkKey := cfg.networkKey()
	invoices, err := p.watchInvoices(networkKey, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("list watch invoices: %w", err)
	}
	if len(invoices) == 0 {
		return 0, nil
	}
	latest, err := chain.LatestBlock(ctx)
	if err != nil {
		return 0, fmt.Errorf("latest block: %w", err)
	}
	requiredConfs := chain.Confirmations()
	for i := range invoices {
		if invoices[i].Confirmations > requiredConfs {
			requiredConfs = invoices[i].Confirmations
		}
	}
	if latest < requiredConfs {
		return 0, nil
	}
	safe := latest - requiredConfs
	cursor, err := p.cursor(networkKey)
	if err != nil {
		return 0, fmt.Errorf("load cursor: %w", err)
	}
	from, to, ok := scanRange(cursor, safe)
	if !ok {
		if err := p.reconcileInvoices(ctx, chain, invoices); err != nil {
			return 0, err
		}
		return cursor, nil
	}

	byAddr := make(map[string]*Invoice, len(invoices))
	addrs := make([]string, 0, len(invoices))
	for i := range invoices {
		key := strings.ToLower(invoices[i].Address)
		if _, exists := byAddr[key]; exists {
			return 0, fmt.Errorf("duplicate active deposit address %s", invoices[i].Address)
		}
		byAddr[key] = &invoices[i]
		addrs = append(addrs, invoices[i].Address)
	}

	var observed []observedDeposit
	for start := 0; start < len(addrs); start += addressBatchSize {
		end := start + addressBatchSize
		if end > len(addrs) {
			end = len(addrs)
		}
		deposits, err := chain.ScanTransfers(ctx, addrs[start:end], from, to)
		if err != nil {
			return 0, fmt.Errorf("scan transfers %d-%d: %w", from, to, err)
		}
		if err := hydrateDepositTimes(ctx, chain, deposits); err != nil {
			return 0, err
		}
		for _, deposit := range deposits {
			invoice := byAddr[strings.ToLower(deposit.To)]
			if invoice == nil {
				return 0, fmt.Errorf("validated deposit has no invoice for %s", deposit.To)
			}
			observed = append(observed, observedDeposit{invoiceID: invoice.ID, deposit: deposit})
		}
	}
	if err := p.persistScanBatch(networkKey, chain, to, latest, observed); err != nil {
		return 0, err
	}
	if err := p.reconcileInvoices(ctx, chain, invoices); err != nil {
		return 0, err
	}
	return to, nil
}

func hydrateDepositTimes(ctx context.Context, chain *evmChain, deposits []Deposit) error {
	timestamps := make(map[uint64]time.Time)
	for i := range deposits {
		ts, ok := timestamps[deposits[i].BlockNumber]
		if !ok {
			var err error
			ts, err = chain.BlockTimestamp(ctx, deposits[i].BlockNumber)
			if err != nil {
				return fmt.Errorf("deposit block timestamp %d: %w", deposits[i].BlockNumber, err)
			}
			timestamps[deposits[i].BlockNumber] = ts
		}
		deposits[i].BlockTime = ts
	}
	return nil
}

func (p *Plugin) persistScanBatch(networkKey string, chain *evmChain, to, latest uint64, observed []observedDeposit) error {
	return p.db.Transaction(func(tx *gorm.DB) error {
		for _, item := range observed {
			confirmations := uint64(0)
			if latest >= item.deposit.BlockNumber {
				confirmations = latest - item.deposit.BlockNumber + 1
			}
			row := DepositRow{
				InvoiceID: item.invoiceID, Chain: chain.ID(), NetworkKey: networkKey,
				TxHash: item.deposit.TxHash, LogIndex: item.deposit.LogIndex,
				FromAddr: item.deposit.From, TokenAmount: item.deposit.TokenAmount.String(),
				BlockNumber: item.deposit.BlockNumber, BlockTime: item.deposit.BlockTime,
				Confirmations: confirmations, SeenAt: time.Now().UTC(),
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "network_key"}, {Name: "tx_hash"}, {Name: "log_index"}},
				DoNothing: true,
			}).Create(&row).Error; err != nil {
				return fmt.Errorf("record deposit: %w", err)
			}
		}
		return setCursorTx(tx, networkKey, to)
	})
}

// reconcileInvoices is intentionally independent from finding new logs. A
// restart after deposits commit but before settlement will retry from durable
// rows, while Commerce's idempotency key prevents duplicate order transitions.
func (p *Plugin) reconcileInvoices(ctx context.Context, chain *evmChain, invoices []Invoice) error {
	for i := range invoices {
		inv := &invoices[i]
		rows, err := p.depositRows(inv.ID)
		if err != nil {
			return fmt.Errorf("load invoice %d deposits: %w", inv.ID, err)
		}
		total, onTime := aggregateDeposits(rows, inv.ExpiresAt)
		if err := p.updateReceived(inv.ID, total); err != nil {
			return err
		}
		if inv.Status == invExpired && total.Sign() > 0 {
			status, amount, ok := settlementForReceived(inv, total)
			if !ok {
				status, amount = corecommerce.SettleUnderpaid, underpaidMinor(inv, total)
			}
			if err := p.settle(ctx, inv, total, amount, status, invLate, firstTx(rows)); err != nil {
				return err
			}
			continue
		}
		if inv.Status != invPending && inv.Status != invSeen {
			continue
		}
		status, amount, ok := settlementForReceived(inv, onTime)
		if ok && (status == corecommerce.SettlePaid || status == corecommerce.SettleOverpaid) {
			invStatus := invPaid
			if status == corecommerce.SettleOverpaid {
				invStatus = invOverpaid
			}
			if err := p.settle(ctx, inv, total, amount, status, invStatus, firstTx(rows)); err != nil {
				return err
			}
			continue
		}
		if total.Sign() > 0 {
			if err := p.db.Model(&Invoice{}).Where("id = ? AND status IN ?", inv.ID, []string{invPending, invSeen}).
				Update("status", invSeen).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Plugin) finalizeExpired(ctx context.Context, cfg config, chain *evmChain, safeTime time.Time) error {
	invoices, err := p.expiredInvoices(cfg.networkKey(), safeTime)
	if err != nil {
		return err
	}
	for i := range invoices {
		inv := &invoices[i]
		rows, err := p.depositRows(inv.ID)
		if err != nil {
			return fmt.Errorf("load expired invoice %d deposits: %w", inv.ID, err)
		}
		total, onTime := aggregateDeposits(rows, inv.ExpiresAt)
		status, amount, full := settlementForReceived(inv, onTime)
		if full && (status == corecommerce.SettlePaid || status == corecommerce.SettleOverpaid) {
			invStatus := invPaid
			if status == corecommerce.SettleOverpaid {
				invStatus = invOverpaid
			}
			if err := p.settle(ctx, inv, total, amount, status, invStatus, firstTx(rows)); err != nil {
				return err
			}
			continue
		}
		if onTime.Sign() > 0 {
			amount = underpaidMinor(inv, onTime)
			if err := p.settle(ctx, inv, total, amount, corecommerce.SettleUnderpaid, invUnderpaid, firstTx(rows)); err != nil {
				return err
			}
			continue
		}
		if err := p.settle(ctx, inv, total, 0, corecommerce.SettleExpired, invExpired, ""); err != nil {
			return err
		}
		// Funds first observed after the deadline are never silently accepted as
		// an ordinary on-time order. Reporting them after cancellation deliberately
		// moves Commerce to its reconciliation state.
		if total.Sign() > 0 {
			status, amount, ok := settlementForReceived(inv, total)
			if !ok {
				status, amount = corecommerce.SettleUnderpaid, underpaidMinor(inv, total)
			}
			if err := p.settle(ctx, inv, total, amount, status, invLate, firstTx(rows)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Plugin) depositRows(invoiceID uint) ([]DepositRow, error) {
	var rows []DepositRow
	err := p.db.Where("invoice_id = ?", invoiceID).
		Order("block_number asc, log_index asc, id asc").Find(&rows).Error
	return rows, err
}

func aggregateDeposits(rows []DepositRow, deadline time.Time) (total, onTime *big.Int) {
	total, onTime = big.NewInt(0), big.NewInt(0)
	for _, row := range rows {
		value, ok := new(big.Int).SetString(row.TokenAmount, 10)
		if !ok || value.Sign() <= 0 {
			continue
		}
		total.Add(total, value)
		blockTime := row.BlockTime
		if blockTime.IsZero() {
			blockTime = row.SeenAt
		}
		if !blockTime.After(deadline) {
			onTime.Add(onTime, value)
		}
	}
	return total, onTime
}

func settlementForReceived(inv *Invoice, received *big.Int) (corecommerce.SettleStatus, int64, bool) {
	if inv == nil || received == nil {
		return "", 0, false
	}
	expected, ok := new(big.Int).SetString(inv.ExpectedToken, 10)
	if !ok {
		return "", 0, false
	}
	dust, ok := new(big.Int).SetString(inv.DustTolerance, 10)
	if !ok {
		dust = big.NewInt(0)
	}
	if !withinTolerance(received, expected, dust) {
		return "", 0, false
	}
	actual := tokenToUSD(received, inv.RateScaled, inv.TokenDecimals)
	if received.Cmp(expected) > 0 && actual > inv.USDMinor {
		return corecommerce.SettleOverpaid, actual, true
	}
	return corecommerce.SettlePaid, inv.USDMinor, true
}

func underpaidMinor(inv *Invoice, received *big.Int) int64 {
	amount := tokenToUSD(received, inv.RateScaled, inv.TokenDecimals)
	if amount >= inv.USDMinor {
		amount = inv.USDMinor - 1
	}
	if amount < 0 {
		amount = 0
	}
	return amount
}

func (p *Plugin) updateReceived(invoiceID uint, received *big.Int) error {
	return p.db.Model(&Invoice{}).Where("id = ?", invoiceID).
		Update("received_token", received.String()).Error
}

func (p *Plugin) settle(ctx context.Context, inv *Invoice, received *big.Int, amountMinor int64, status corecommerce.SettleStatus, invStatus, txn string) error {
	settler := corecommerce.GetSettler(p.hooks)
	if settler == nil {
		return errors.New("commerce-usdt: commerce settler unavailable")
	}
	key := fmt.Sprintf("usdt:%d:%s:%s", inv.ID, status, inv.OrderRef)
	err := settler.Settle(ctx, corecommerce.SettleRequest{
		OrderRef: inv.OrderRef, Gateway: gatewayID, TxnID: txn,
		Amount: corecommerce.New(amountMinor, inv.Currency), Status: status, IdempotencyKey: key,
		Raw: map[string]any{
			"chain": inv.Chain, "network_key": inv.NetworkKey,
			"address": inv.Address, "token_contract": inv.TokenContract,
			"expected_token": inv.ExpectedToken, "received_token": received.String(),
		},
	})
	if err != nil {
		return fmt.Errorf("settle order %s as %s: %w", inv.OrderRef, status, err)
	}
	now := time.Now().UTC()
	if err := p.db.Model(&Invoice{}).Where("id = ?", inv.ID).Updates(map[string]interface{}{
		"status": invStatus, "received_token": received.String(), "settled_at": &now,
	}).Error; err != nil {
		return fmt.Errorf("mark invoice %d settled: %w", inv.ID, err)
	}
	inv.Status, inv.SettledAt = invStatus, &now
	logger.Info("commerce-usdt: settled", "order", inv.OrderRef, "status", string(status))
	return nil
}

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
	if to-from >= maxScanSpan {
		to = from + maxScanSpan - 1
	}
	return from, to, true
}

func firstTx(rows []DepositRow) string {
	if len(rows) > 0 {
		return rows[0].TxHash
	}
	return ""
}

func (p *Plugin) cursor(networkKey string) (uint64, error) {
	var cur NetworkCursor
	err := p.db.Where("network_key = ?", networkKey).First(&cur).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return cur.LastScannedBlock, nil
}

func setCursorTx(tx *gorm.DB, networkKey string, block uint64) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "network_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_scanned_block", "updated_at"}),
	}).Create(&NetworkCursor{NetworkKey: networkKey, LastScannedBlock: block, UpdatedAt: time.Now().UTC()}).Error
}
