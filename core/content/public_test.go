// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package content

import (
	"context"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestIsPublicMatchesPublishedQueryRule(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	for _, tc := range []struct {
		name string
		item *Content
		want bool
	}{
		{"nil", nil, false},
		{"published without date", &Content{Status: StatusPublished}, true},
		{"published in the past", &Content{Status: StatusPublished, PublishedAt: &past}, true},
		{"published exactly now", &Content{Status: StatusPublished, PublishedAt: &now}, true},
		{"scheduled", &Content{Status: StatusPublished, PublishedAt: &future}, false},
		{"draft", &Content{Status: StatusDraft, PublishedAt: &past}, false},
		{"trash", &Content{Status: StatusTrash}, false},
		{"soft deleted", &Content{Status: StatusPublished, DeletedAt: gorm.DeletedAt{Time: past, Valid: true}}, false},
	} {
		if got := tc.item.IsPublic(now); got != tc.want {
			t.Errorf("%s: IsPublic = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPublicOrderFieldsAreACopyOfTheWhitelist(t *testing.T) {
	fields := PublicOrderFields()
	want := []string{"created_at", "updated_at", "published_at", "title", "sort_order"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("PublicOrderFields = %v, want %v", fields, want)
	}
	fields[0] = "id; DROP TABLE x"
	if IsPublicOrderField("id; DROP TABLE x") || !IsPublicOrderField("created_at") {
		t.Fatal("changing the returned slice changed the whitelist")
	}
	if IsPublicOrderField("id") || IsPublicOrderField("") {
		t.Fatal("non-whitelisted fields were accepted")
	}
}

func TestMetaForIDsLoadsSeveralItemsInOneQuery(t *testing.T) {
	db, _, repository, _ := contentCommandTestRuntime(t)
	for _, row := range []ContentMeta{
		{ContentID: 1, MetaKey: "price", MetaValue: "10"},
		{ContentID: 1, MetaKey: "sku", MetaValue: "A-1"},
		{ContentID: 2, MetaKey: "price", MetaValue: "20"},
		{ContentID: 3, MetaKey: "price", MetaValue: "30"},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	queries := 0
	if err := db.Callback().Query().After("gorm:query").Register("count_meta_queries", func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}

	got, err := repository.MetaForIDs(context.Background(), []uint{2, 1, 2, 9})
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint]map[string]string{
		1: {"price": "10", "sku": "A-1"},
		2: {"price": "20"},
		9: {},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MetaForIDs = %v, want %v", got, want)
	}
	if queries != 1 {
		t.Fatalf("MetaForIDs ran %d queries, want 1", queries)
	}
	single, err := repository.GetMeta(1)
	if err != nil || !reflect.DeepEqual(single, got[1]) {
		t.Fatalf("GetMeta(1) = %v, %v; want %v", single, err, got[1])
	}
}

func TestMetaForIDsSplitsLargeIDLists(t *testing.T) {
	db, _, repository, _ := contentCommandTestRuntime(t)
	queries := 0
	if err := db.Callback().Query().After("gorm:query").Register("count_meta_batches", func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	ids := make([]uint, metaBatchSize+3)
	for i := range ids {
		ids[i] = uint(i + 1)
	}
	got, err := repository.MetaForIDs(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(ids) || queries != 2 {
		t.Fatalf("got %d entries in %d queries, want %d in 2", len(got), queries, len(ids))
	}
}
