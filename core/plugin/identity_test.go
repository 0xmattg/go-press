// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package plugin

import (
	"strings"
	"testing"

	"github.com/0xmattg/go-press/pkg/dbprefix"
)

func TestValidSlug(t *testing.T) {
	for _, slug := range []string{"seo", "multilang", "commerce-usdt", "my-shop-theme-two", "a1", "web3-login"} {
		if !ValidSlug(slug) {
			t.Errorf("ValidSlug(%q) = false, want true", slug)
		}
	}
	for _, slug := range []string{"", "a", "Multi", "multi_language", "-seo", "seo-", "seo--x", "1seo", "seo.x", strings.Repeat("a", 65)} {
		if ValidSlug(slug) {
			t.Errorf("ValidSlug(%q) = true, want false", slug)
		}
	}
}

func TestStorageSlugAndTable(t *testing.T) {
	if got := StorageSlug("commerce-usdt"); got != "commerce_usdt" {
		t.Fatalf("StorageSlug = %q", got)
	}
	dbprefix.Set("gp_site2_")
	t.Cleanup(func() { dbprefix.Set(dbprefix.DefaultPrefix) })
	if got := (Meta{Slug: "gopress-analytics"}).Table("events"); got != "gp_site2_plgn_gopress_analytics_events" {
		t.Fatalf("Table = %q", got)
	}
}
