package commerce

import (
	"errors"
	"math"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrOversell is returned when a reservation would drive managed stock below
// zero. The caller must roll the checkout transaction back.
var (
	ErrOversell          = errors.New("commerce: insufficient stock")
	ErrInventoryOverflow = errors.New("commerce: inventory quantity overflow")
)

// InventoryService adjusts stock atomically and records every movement in the
// inventory ledger. Reservations decrement on-hand stock immediately (holding it
// for a pending order); Commit finalizes a paid reservation and Release returns
// stock for a cancelled/expired order.
type InventoryService struct{ p *Plugin }

func (p *Plugin) inventory() *InventoryService { return &InventoryService{p: p} }

// stockShort reports whether reserving qty would oversell. Pure: unmanaged stock
// is treated as unlimited. Extracted for unit testing.
func stockShort(managed bool, onHand, qty int) bool {
	return managed && onHand < qty
}

// Reserve holds qty of a product against orderID within tx, taking a row lock on
// product_data (SELECT … FOR UPDATE) so concurrent checkouts of the last unit
// can't both succeed. Returns ErrOversell when managed stock is insufficient;
// unmanaged products always succeed. MUST run inside the checkout transaction.
func (s *InventoryService) Reserve(tx *gorm.DB, productID uint, qty int, orderID uint) error {
	if qty < 1 {
		return nil
	}
	var pd ProductData
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&pd, "content_id = ?", productID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrProductDataMissing
	}
	if err != nil {
		return err
	}
	if !pd.ManageStock {
		return nil
	}
	if stockShort(true, pd.StockQty, qty) {
		return ErrOversell
	}
	pd.StockQty -= qty
	if pd.StockQty <= 0 {
		pd.StockStatus = "outofstock"
	}
	if err := tx.Model(&ProductData{}).Where("content_id = ?", productID).
		Updates(map[string]interface{}{"stock_qty": pd.StockQty, "stock_status": pd.StockStatus, "version": gorm.Expr("version + 1"), "updated_at": time.Now().UTC()}).Error; err != nil {
		return err
	}
	if err := s.writeLedger(tx, productID, -qty, "reserve", &orderID); err != nil {
		return err
	}
	return s.syncLookupStock(tx, &pd)
}

// Commit finalizes a paid reservation. Stock was already decremented at Reserve,
// so this only records an audit marker and bumps the sales counter.
func (s *InventoryService) Commit(db *gorm.DB, productID uint, qty int, orderID uint) error {
	if qty < 1 {
		return nil
	}
	if err := s.writeLedger(db, productID, 0, "out", &orderID); err != nil {
		return err
	}
	return db.Model(&ProductLookup{}).Where("content_id = ?", productID).
		UpdateColumn("sales", gorm.Expr("sales + ?", qty)).Error
}

// Release returns reserved stock for a cancelled/expired order and records the
// movement. Only touches managed products.
func (s *InventoryService) Release(db *gorm.DB, productID uint, qty int, orderID uint) error {
	if qty < 1 {
		return nil
	}
	var pd ProductData
	// Serialize release with reservations and other cancellation/failure paths.
	// Without this row lock two concurrent releases can both read the same old
	// quantity and one increment is lost.
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&pd, "content_id = ?", productID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !pd.ManageStock {
		return nil
	}
	if pd.StockQty < 0 || qty > math.MaxInt-pd.StockQty {
		return ErrInventoryOverflow
	}
	pd.StockQty += qty
	if pd.StockQty > 0 {
		pd.StockStatus = "instock"
	}
	if err := db.Model(&ProductData{}).Where("content_id = ?", productID).
		Updates(map[string]interface{}{"stock_qty": pd.StockQty, "stock_status": pd.StockStatus, "version": gorm.Expr("version + 1"), "updated_at": time.Now().UTC()}).Error; err != nil {
		return err
	}
	if err := s.writeLedger(db, productID, qty, "release", &orderID); err != nil {
		return err
	}
	return s.syncLookupStock(db, &pd)
}

func (s *InventoryService) writeLedger(db *gorm.DB, productID uint, delta int, reason string, orderID *uint) error {
	return db.Create(&InventoryLedger{
		ProductRef: productID, Delta: delta, Reason: reason, OrderID: orderID, CreatedAt: time.Now().UTC(),
	}).Error
}

// syncLookupStock keeps product_lookup.in_stock consistent with the current
// product_data stock status (catalog reads use the lookup table).
func (s *InventoryService) syncLookupStock(db *gorm.DB, pd *ProductData) error {
	return db.Model(&ProductLookup{}).Where("content_id = ?", pd.ContentID).
		UpdateColumn("in_stock", pd.StockStatus == "instock").Error
}
