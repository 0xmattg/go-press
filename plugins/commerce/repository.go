package commerce

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrProductDataConflict means an admin submitted a stale product form. The
// caller must reload instead of overwriting a concurrent inventory mutation.
var ErrProductDataConflict = errors.New("commerce: product data changed concurrently")

// Repository is the commerce data-access layer. P0 only wires migrations; query
// methods land with the catalog/cart/order phases (P1–P2).
type Repository struct {
	db *gorm.DB
}

// NewRepository builds a Repository over the engine's *gorm.DB.
func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

// migrationModels are every commerce table. Order/cart tables are created in P0
// (schema only) so later phases add behavior without a migration step then.
func migrationModels() []interface{} {
	return []interface{}{
		&ProductData{}, &ProductLookup{},
		&Order{}, &OrderItem{}, &OrderAddress{}, &Payment{},
		&Cart{}, &CartItem{},
		&OrderNote{}, &Refund{}, &InventoryLedger{},
	}
}

// AutoMigrate creates/updates all commerce tables. Idempotent.
func (r *Repository) AutoMigrate() error {
	return r.db.AutoMigrate(migrationModels()...)
}

// GetProductData returns the commerce data for a product content row, or a
// gorm.ErrRecordNotFound error when none exists yet.
func (r *Repository) GetProductData(contentID uint) (*ProductData, error) {
	var pd ProductData
	if err := r.db.First(&pd, "content_id = ?", contentID).Error; err != nil {
		return nil, err
	}
	return &pd, nil
}

// CreateProductDataIfMissing imports seed metadata exactly once. Re-running a
// demo import or reactivating Commerce must never reset live price or stock.
func (r *Repository) CreateProductDataIfMissing(pd *ProductData) (bool, error) {
	if pd.Version == 0 {
		pd.Version = 1
	}
	created := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "content_id"}},
			DoNothing: true,
		}).Create(pd)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if !created {
			return nil
		}
		return NewRepository(tx).SyncLookup(pd)
	})
	return created, err
}

// SaveProductData performs an optimistic-locking admin save and refreshes the
// lookup in the same transaction. expectedVersion=0 is valid only for a new
// product; an existing row then yields ErrProductDataConflict.
func (r *Repository) SaveProductData(pd *ProductData, expectedVersion uint64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if expectedVersion == 0 {
			pd.Version = 1
			if err := tx.Create(pd).Error; err != nil {
				return ErrProductDataConflict
			}
		} else {
			updates := map[string]interface{}{
				"type": pd.Type, "sku": pd.SKU, "price_amount": pd.PriceAmount,
				"currency": pd.Currency, "sale_price_amount": pd.SalePriceAmount,
				"tax_class": pd.TaxClass, "manage_stock": pd.ManageStock,
				"stock_qty": pd.StockQty, "stock_status": pd.StockStatus,
				"weight_grams": pd.WeightGrams, "virtual": pd.Virtual,
				"downloadable": pd.Downloadable, "version": gorm.Expr("version + 1"),
				"updated_at": time.Now().UTC(),
			}
			result := tx.Model(&ProductData{}).
				Where("content_id = ? AND version = ?", pd.ContentID, expectedVersion).
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrProductDataConflict
			}
		}
		return NewRepository(tx).SyncLookup(pd)
	})
}

// SyncLookup refreshes the denormalized product_lookup row (effective price +
// stock) used for fast catalog filtering/sorting. Called after a product saves.
func (r *Repository) SyncLookup(pd *ProductData) error {
	price := pd.PriceAmount
	if pd.SalePriceAmount != nil && *pd.SalePriceAmount > 0 {
		price = *pd.SalePriceAmount
	}
	lk := ProductLookup{
		ContentID:    pd.ContentID,
		CurrentPrice: price,
		Currency:     pd.Currency,
		InStock:      pd.StockStatus == "instock",
		UpdatedAt:    time.Now().UTC(),
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "content_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"current_price": lk.CurrentPrice,
			"currency":      lk.Currency,
			"in_stock":      lk.InStock,
			"updated_at":    lk.UpdatedAt,
		}),
	}).Create(&lk).Error
}
