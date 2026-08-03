package commerceusdt

import (
	"errors"
	"time"

	"go-press/pkg/dbprefix"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// tableSlug is hyphen-free so the generated table names are valid SQL
// identifiers (the plugin slug "commerce-usdt" keeps the hyphen for options and
// locale discovery, which are fine with it).
const tableSlug = "commerce_usdt"

func tbl(name string) string { return dbprefix.PluginTable(tableSlug, name) }

func tableBaseNames() []string {
	return []string{"invoices", "deposits", "chain_cursors", "network_cursors", "hd_counters"}
}

func migrationModels() []interface{} {
	return []interface{}{&Invoice{}, &DepositRow{}, &ChainCursor{}, &NetworkCursor{}, &HDCounter{}}
}

// Invoice statuses.
const (
	invPending   = "pending"
	invSeen      = "seen" // some deposit observed, not yet sufficient + confirmed
	invPaid      = "paid"
	invOverpaid  = "overpaid"
	invUnderpaid = "underpaid"
	invExpired   = "expired"
	invLate      = "late_reconciliation"
)

// Invoice is one order's crypto payment intent: a unique deposit address and the
// exact token amount expected, plus running received total and lifecycle status.
type Invoice struct {
	ID            uint   `gorm:"primaryKey"`
	OrderRef      string `gorm:"size:64;uniqueIndex"`
	StartKey      string `gorm:"size:191;uniqueIndex"` // = PaymentRequest.IdempotencyKey
	Chain         string `gorm:"size:32;index"`
	NetworkKey    string `gorm:"size:96;index"`
	EVMChainID    int64  `gorm:"column:evm_chain_id"`
	HDIndex       uint32
	Address       string `gorm:"size:64;index"`
	TokenContract string `gorm:"size:64"`
	TokenDecimals int
	Confirmations uint64
	ExpectedToken string `gorm:"size:80"` // big.Int decimal string (raw minor units)
	ReceivedToken string `gorm:"size:80"` // accumulated confirmed minor units
	USDMinor      int64
	Currency      string `gorm:"size:8"`
	RateScaled    int64
	DustTolerance string `gorm:"size:80"`
	Status        string `gorm:"size:20;index;default:pending"`
	CreatedAt     time.Time
	ExpiresAt     time.Time `gorm:"index"`
	WatchUntil    time.Time `gorm:"index"`
	SettledAt     *time.Time
}

func (Invoice) TableName() string { return tbl("invoices") }

// DepositRow records one confirmed on-chain transfer, deduped by immutable
// network identity + transaction hash + log index so restarts and overlapping
// scans never double-count or collide across contracts.
type DepositRow struct {
	ID            uint   `gorm:"primaryKey"`
	InvoiceID     uint   `gorm:"index"`
	Chain         string `gorm:"size:32;index"`
	NetworkKey    string `gorm:"size:96;uniqueIndex:ux_usdt_deposit_v2,priority:1"`
	TxHash        string `gorm:"size:80;uniqueIndex:ux_usdt_deposit_v2,priority:2"`
	LogIndex      uint   `gorm:"uniqueIndex:ux_usdt_deposit_v2,priority:3"`
	FromAddr      string `gorm:"size:64"`
	TokenAmount   string `gorm:"size:80"`
	BlockNumber   uint64
	BlockTime     time.Time `gorm:"index"`
	Confirmations uint64
	SeenAt        time.Time
}

func (DepositRow) TableName() string { return tbl("deposits") }

// ChainCursor is the last safely-scanned block per chain (incremental scanning).
type ChainCursor struct {
	Chain            string `gorm:"size:32;primaryKey"`
	LastScannedBlock uint64
	UpdatedAt        time.Time
}

func (ChainCursor) TableName() string { return tbl("chain_cursors") }

// NetworkCursor scopes progress to an immutable EVM chain-id + token-contract
// identity. The legacy ChainCursor remains migrated only so existing installs
// keep ownership metadata for the old table; new scans never use it.
type NetworkCursor struct {
	NetworkKey       string `gorm:"size:96;primaryKey"`
	LastScannedBlock uint64
	UpdatedAt        time.Time
}

func (NetworkCursor) TableName() string { return tbl("network_cursors") }

// HDCounter is the monotonic next HD derivation index per chain (never reused).
type HDCounter struct {
	Chain     string `gorm:"size:32;primaryKey"`
	NextIndex uint32
	UpdatedAt time.Time
}

func (HDCounter) TableName() string { return tbl("hd_counters") }

// --- repository helpers (use p.db) ---

func (p *Plugin) autoMigrate() error {
	return p.db.AutoMigrate(migrationModels()...)
}

// backfillLegacyRows upgrades invoices created before network/currency snapshots
// were introduced. It uses the current configured identity and never guesses
// when that identity is unavailable.
func (p *Plugin) backfillLegacyRows(cfg config) error {
	if p == nil || p.db == nil || cfg.networkKey() == "" {
		return nil
	}
	var invoices []Invoice
	if err := p.db.Where("network_key = '' OR currency = '' OR dust_tolerance = '' OR confirmations = 0 OR watch_until IS NULL OR watch_until = ?", time.Time{}).
		Find(&invoices).Error; err != nil {
		return err
	}
	for i := range invoices {
		inv := &invoices[i]
		updates := map[string]interface{}{}
		if inv.NetworkKey == "" {
			updates["network_key"] = cfg.networkKey()
			updates["evm_chain_id"] = cfg.net.ChainID
		}
		if inv.Currency == "" {
			updates["currency"] = "USD"
		}
		if inv.DustTolerance == "" {
			updates["dust_tolerance"] = "0"
		}
		if inv.Confirmations == 0 {
			updates["confirmations"] = cfg.Confirmations
		}
		if inv.WatchUntil.IsZero() {
			updates["watch_until"] = inv.ExpiresAt.Add(lateWatchRetention)
		}
		if len(updates) > 0 {
			if err := p.db.Model(&Invoice{}).Where("id = ?", inv.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		if err := p.db.Model(&DepositRow{}).Where("invoice_id = ? AND network_key = ''", inv.ID).
			Updates(map[string]interface{}{"network_key": cfg.networkKey()}).Error; err != nil {
			return err
		}
	}
	return nil
}

// allocHDIndex reserves the next never-reused derivation index for a chain under
// a row lock, inside the caller's transaction.
func (p *Plugin) allocHDIndex(tx *gorm.DB, chain string) (uint32, error) {
	// Ensure the counter row exists without racing (unique PK).
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&HDCounter{Chain: chain, NextIndex: 0, UpdatedAt: time.Now().UTC()}).Error; err != nil {
		return 0, err
	}
	var ctr HDCounter
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("chain = ?", chain).First(&ctr).Error; err != nil {
		return 0, err
	}
	idx := ctr.NextIndex
	ctr.NextIndex = idx + 1
	ctr.UpdatedAt = time.Now().UTC()
	if err := tx.Save(&ctr).Error; err != nil {
		return 0, err
	}
	return idx, nil
}

// findInvoiceByStartKey returns the invoice for a checkout idempotency key, or
// nil when none exists yet.
func (p *Plugin) findInvoiceByStartKey(key string) (*Invoice, error) {
	var inv Invoice
	err := p.db.Where("start_key = ?", key).First(&inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// activeInvoices returns invoices for a chain that still need watching (pending
// or partially-seen), whose addresses the watcher must scan.
func (p *Plugin) watchInvoices(networkKey string, now time.Time) ([]Invoice, error) {
	var invs []Invoice
	err := p.db.Where("network_key = ? AND (status IN ? OR (status IN ? AND watch_until > ?))",
		networkKey, []string{invPending, invSeen}, []string{invExpired, invUnderpaid, invLate}, now).
		Order("id asc").Find(&invs).Error
	return invs, err
}

func (p *Plugin) watchableInvoiceCount(now time.Time) (int64, error) {
	if p == nil || p.db == nil {
		return 0, nil
	}
	var count int64
	err := p.db.Model(&Invoice{}).
		Where("status IN ? OR (status IN ? AND watch_until > ?)",
			[]string{invPending, invSeen}, []string{invExpired, invUnderpaid, invLate}, now).
		Count(&count).Error
	return count, err
}

// expiredInvoices returns pending/seen invoices past their window (to finalize as
// expired/underpaid).
func (p *Plugin) expiredInvoices(networkKey string, safeTime time.Time) ([]Invoice, error) {
	var invs []Invoice
	err := p.db.Where("network_key = ? AND status IN ? AND expires_at < ?",
		networkKey, []string{invPending, invSeen}, safeTime).Order("id asc").Find(&invs).Error
	return invs, err
}
