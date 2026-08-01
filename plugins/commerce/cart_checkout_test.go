package commerce

import (
	"errors"
	"testing"

	"go-press/core"
)

func TestConsumeCartSnapshotPreservesConcurrentMutations(t *testing.T) {
	db := commerceTestDB(t)
	p := &Plugin{engine: &core.Engine{DB: db}}
	cart := Cart{Token: "checkout-cart", Currency: "USD"}
	other := Cart{Token: "other-cart", Currency: "USD"}
	if err := db.Create(&cart).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	unchanged := CartItem{CartID: cart.ID, ProductContentID: 1, Qty: 2}
	increased := CartItem{CartID: cart.ID, ProductContentID: 2, Qty: 5}
	decreased := CartItem{CartID: cart.ID, ProductContentID: 3, Qty: 1}
	foreign := CartItem{CartID: other.ID, ProductContentID: 4, Qty: 2}
	for _, item := range []*CartItem{&unchanged, &increased, &decreased, &foreign} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}

	snapshot := CartView{cartID: cart.ID, Lines: []CartLine{
		{ItemID: unchanged.ID, Qty: 2},
		{ItemID: increased.ID, Qty: 2},
		// The buyer lowered this line after checkout started; retain the newer 1.
		{ItemID: decreased.ID, Qty: 2},
		// Even a forged/corrupt snapshot cannot consume another cart's row.
		{ItemID: foreign.ID, Qty: 2},
	}}
	if err := p.cart().consumeSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}

	var count int64
	if err := db.Model(&CartItem{}).Where("id = ?", unchanged.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("unchanged line count = %d, err=%v; want deleted", count, err)
	}
	for name, want := range map[string]struct {
		id  uint
		qty int
	}{
		"concurrent addition": {increased.ID, 3},
		"concurrent decrease": {decreased.ID, 1},
		"foreign cart":        {foreign.ID, 2},
	} {
		var item CartItem
		if err := db.First(&item, want.id).Error; err != nil || item.Qty != want.qty {
			t.Fatalf("%s qty = %d, err=%v; want %d", name, item.Qty, err, want.qty)
		}
	}
}

func TestConsumeCartSnapshotRollsBackAllLinesOnValidationFailure(t *testing.T) {
	db := commerceTestDB(t)
	p := &Plugin{engine: &core.Engine{DB: db}}
	cart := Cart{Token: "rollback-cart", Currency: "USD"}
	if err := db.Create(&cart).Error; err != nil {
		t.Fatal(err)
	}
	item := CartItem{CartID: cart.ID, ProductContentID: 1, Qty: 2}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}

	err := p.cart().consumeSnapshot(CartView{cartID: cart.ID, Lines: []CartLine{
		{ItemID: item.ID, Qty: 2},
		{ItemID: 0, Qty: 1},
	}})
	if !errors.Is(err, ErrCartItemNotFound) {
		t.Fatalf("consume error = %v, want ErrCartItemNotFound", err)
	}
	var saved CartItem
	if err := db.First(&saved, item.ID).Error; err != nil || saved.Qty != 2 {
		t.Fatalf("rolled-back line qty = %d, err=%v; want 2", saved.Qty, err)
	}
}
