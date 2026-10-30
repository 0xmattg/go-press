// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package content

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

func metaRows(t *testing.T, db *gorm.DB, contentID uint, key string) []ContentMeta {
	t.Helper()
	var rows []ContentMeta
	if err := db.Where("content_id = ? AND meta_key = ?", contentID, key).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestSaveMetaInsertsOnceThenUpdates(t *testing.T) {
	db, _, repository, _ := contentCommandTestRuntime(t)
	if err := repository.SaveMeta(7, "_seo_title", "First"); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveMeta(7, "_seo_title", "Second"); err != nil {
		t.Fatal(err)
	}
	rows := metaRows(t, db, 7, "_seo_title")
	if len(rows) != 1 || rows[0].MetaValue != "Second" {
		t.Fatalf("rows = %+v, want one row with Second", rows)
	}
}

// Legacy databases may already hold duplicate rows for a key. A save must
// bring every copy to the new value so readers cannot disagree, and must not
// add yet another row.
func TestSaveMetaKeepsLegacyDuplicatesInAgreement(t *testing.T) {
	db, commands, repository, _ := contentCommandTestRuntime(t)
	for _, value := range []string{"old-a", "old-b"} {
		if err := db.Create(&ContentMeta{ContentID: 9, MetaKey: "subtitle", MetaValue: value}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.SaveMeta(9, "subtitle", "fresh"); err != nil {
		t.Fatal(err)
	}
	rows := metaRows(t, db, 9, "subtitle")
	if len(rows) != 2 || rows[0].MetaValue != "fresh" || rows[1].MetaValue != "fresh" {
		t.Fatalf("rows after SaveMeta = %+v, want both copies set to fresh", rows)
	}
	if meta, err := repository.GetMeta(9); err != nil || meta["subtitle"] != "fresh" {
		t.Fatalf("GetMeta() = %v, %v", meta, err)
	}

	// The command service path shares the same write rule.
	item := &Content{Type: "post", Status: StatusDraft, Title: "Dup", Slug: "dup"}
	if err := commands.Create(context.Background(), item, nil); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"x", "y"} {
		if err := db.Create(&ContentMeta{ContentID: item.ID, MetaKey: "subtitle", MetaValue: value}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := commands.Update(context.Background(), "post", item, map[string]string{"subtitle": "z"}); err != nil {
		t.Fatal(err)
	}
	rows = metaRows(t, db, item.ID, "subtitle")
	if len(rows) != 2 || rows[0].MetaValue != "z" || rows[1].MetaValue != "z" {
		t.Fatalf("rows after command update = %+v, want both copies set to z", rows)
	}
}

// A failed write must surface as an error. The previous implementation took
// any lookup error for "no row yet" and inserted a duplicate.
func TestSaveMetaReturnsWriteErrorsWithoutInserting(t *testing.T) {
	db, _, repository, _ := contentCommandTestRuntime(t)
	if err := repository.SaveMeta(11, "flag", "before"); err != nil {
		t.Fatal(err)
	}
	transient := errors.New("connection reset")
	if err := db.Callback().Update().Before("gorm:update").Register("test:fail_meta_update", func(tx *gorm.DB) {
		_ = tx.AddError(transient)
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveMeta(11, "flag", "after"); !errors.Is(err, transient) {
		t.Fatalf("SaveMeta() error = %v, want the write error", err)
	}
	if err := repository.SaveMeta(12, "flag", "new"); !errors.Is(err, transient) {
		t.Fatalf("SaveMeta() on a new key error = %v, want the write error", err)
	}
	if err := db.Callback().Update().Remove("test:fail_meta_update"); err != nil {
		t.Fatal(err)
	}
	if rows := metaRows(t, db, 11, "flag"); len(rows) != 1 || rows[0].MetaValue != "before" {
		t.Fatalf("rows after failed save = %+v, want the original single row", rows)
	}
	if rows := metaRows(t, db, 12, "flag"); len(rows) != 0 {
		t.Fatalf("failed first save inserted rows: %+v", rows)
	}
}
