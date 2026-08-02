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
	return []string{"invoices", "deposits", "chain_cursors", "hd_counters"}
}

func migrationModels() []interface{} {
	return []interface{}{&Invoice{}, &DepositRow{}, &ChainCursor{}, &HDCounter{}}
}

// Invoice statuses.
const (
	invPending   = "pending"
	invSeen      = "seen" // some deposit observed, not yet sufficient + confirmed
	invPaid      = "paid"
	invUnderpaid = "underpaid"
	invExpired   = "expired"
)

// Invoice is one order's crypto payment intent: a unique deposit address and the
// exact token amount expected, plus running received total and lifecycle status.
type Invoice struct {
	ID            uint   `gorm:"primaryKey"`
	OrderRef      string `gorm:"size:64;uniqueIndex"`
	StartKey      string `gorm:"size:191;uniqueIndex"` // = PaymentRequest.IdempotencyKey
	Chain         string `gorm:"size:32;index"`
	HDIndex       uint32
	Address       string `gorm:"size:64;index"`
	TokenContract string `gorm:"size:64"`
	TokenDecimals int
	ExpectedToken string `gorm:"size:80"` // big.Int decimal string (raw minor units)
	ReceivedToken string `gorm:"size:80"` // accumulated confirmed minor units
	USDMinor      int64
	RateScaled    int64
	Status        string `gorm:"size:20;index;default:pending"`
	CreatedAt     time.Time
	ExpiresAt     time.Time `gorm:"index"`
	SettledAt     *time.Time
}

func (Invoice) TableName() string { return tbl("invoices") }

// DepositRow records one confirmed on-chain transfer, deduped by (chain, tx, log
// index) so restarts and overlapping scans never double-count.
type DepositRow struct {
	ID            uint   `gorm:"primaryKey"`
	InvoiceID     uint   `gorm:"index"`
	Chain         string `gorm:"size:32;uniqueIndex:ux_usdt_deposit,priority:1"`
	TxHash        string `gorm:"size:80;uniqueIndex:ux_usdt_deposit,priority:2"`
	LogIndex      uint   `gorm:"uniqueIndex:ux_usdt_deposit,priority:3"`
	FromAddr      string `gorm:"size:64"`
	TokenAmount   string `gorm:"size:80"`
	BlockNumber   uint64
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
func (p *Plugin) activeInvoices(chain string) ([]Invoice, error) {
	var invs []Invoice
	err := p.db.Where("chain = ? AND status IN ?", chain, []string{invPending, invSeen}).
		Order("id asc").Limit(1000).Find(&invs).Error
	return invs, err
}

// expiredInvoices returns pending/seen invoices past their window (to finalize as
// expired/underpaid).
func (p *Plugin) expiredInvoices(chain string, now time.Time) ([]Invoice, error) {
	var invs []Invoice
	err := p.db.Where("chain = ? AND status IN ? AND expires_at < ?",
		chain, []string{invPending, invSeen}, now).Limit(200).Find(&invs).Error
	return invs, err
}
