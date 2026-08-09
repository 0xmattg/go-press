package commerce

import (
	"errors"
	"testing"

	"github.com/0xmattg/go-press/core"

	"gorm.io/gorm"
)

func TestProductDataOptimisticSaveProtectsReservedStock(t *testing.T) {
	db := commerceTestDB(t)
	repo := NewRepository(db)
	seed := &ProductData{
		ContentID: 41, Type: "simple", PriceAmount: 1000, Currency: "USD",
		ManageStock: true, StockQty: 5, StockStatus: "instock",
	}
	created, err := repo.CreateProductDataIfMissing(seed)
	if err != nil || !created {
		t.Fatalf("initial product create = %v, %v", created, err)
	}

	// This is the version an admin form rendered before checkout reserved stock.
	stale, err := repo.GetProductData(seed.ContentID)
	if err != nil {
		t.Fatal(err)
	}
	p := &Plugin{engine: &core.Engine{DB: db}, repo: repo}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return p.inventory().Reserve(tx, seed.ContentID, 2, 77)
	}); err != nil {
		t.Fatal(err)
	}

	stale.PriceAmount = 1200
	stale.StockQty = 99
	if err := repo.SaveProductData(stale, stale.Version); !errors.Is(err, ErrProductDataConflict) {
		t.Fatalf("stale save error = %v, want ErrProductDataConflict", err)
	}
	current, err := repo.GetProductData(seed.ContentID)
	if err != nil || current.StockQty != 3 || current.Version != stale.Version+1 || current.PriceAmount != 1000 {
		t.Fatalf("current product = %+v, err=%v; stale form overwrote inventory", current, err)
	}
}

func TestSeedProductDataNeverResetsExistingProduct(t *testing.T) {
	db := commerceTestDB(t)
	repo := NewRepository(db)
	first := &ProductData{ContentID: 42, Type: "simple", PriceAmount: 1000, Currency: "USD", ManageStock: true, StockQty: 4, StockStatus: "instock"}
	if created, err := repo.CreateProductDataIfMissing(first); err != nil || !created {
		t.Fatalf("first seed = %v, %v", created, err)
	}
	second := &ProductData{ContentID: 42, Type: "simple", PriceAmount: 9999, Currency: "USD", ManageStock: true, StockQty: 99, StockStatus: "instock"}
	if created, err := repo.CreateProductDataIfMissing(second); err != nil || created {
		t.Fatalf("repeated seed = %v, %v; want ignored", created, err)
	}
	current, err := repo.GetProductData(first.ContentID)
	if err != nil || current.PriceAmount != 1000 || current.StockQty != 4 {
		t.Fatalf("product after repeated seed = %+v, err=%v", current, err)
	}
}
