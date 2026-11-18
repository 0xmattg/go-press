// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/rewrite"
	"github.com/0xmattg/go-press/core/taxonomy"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// pgDataFuncTheme seeds the data function fixture into a throwaway schema of
// GOPRESS_TEST_PG_DSN, so the SQL runs against PostgreSQL itself: NOW(),
// column qualification across joins, and the batch queries.
func pgDataFuncTheme(t *testing.T) (*BaseTheme, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("GOPRESS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set GOPRESS_TEST_PG_DSN to run PostgreSQL integration tests")
	}
	dbprefix.Set(dbprefix.DefaultPrefix)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	schemaName := fmt.Sprintf("gopress_theme_itest_%d", time.Now().UnixNano())
	if err := admin.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec(`DROP SCHEMA "` + schemaName + `" CASCADE`).Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&content.Content{}, &content.ContentMeta{}, &taxonomy.Term{}, &taxonomy.Taxonomy{}, &taxonomy.TermRelationship{}); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE gp_test_content_lang (content_id BIGINT PRIMARY KEY, lang TEXT NOT NULL)`,
		`CREATE TABLE gp_test_taxonomy_lang (taxonomy_id BIGINT PRIMARY KEY, lang TEXT NOT NULL)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	return seedDataFuncTheme(t, db), db
}

func TestPGDataFunctions(t *testing.T) {
	b, db := pgDataFuncTheme(t)
	en := dataRequest("en")

	if got := viewIDs(b.contentListFunc(en, "product", 10)); !reflect.DeepEqual(got, []uint{idGadget, idWidget, idFeatured}) {
		t.Fatalf("en products = %v", got)
	}
	// sort_order exists on both contents and term_relationships; the order
	// column must be qualified once the taxonomy filter joins them.
	if got := viewIDs(b.contentListFunc(en, "product", 10, "taxonomy", "product_cat", "term", "widgets")); !reflect.DeepEqual(got, []uint{idWidget, idFeatured}) {
		t.Fatalf("widgets = %v", got)
	}
	if got := viewIDs(b.contentListFunc(en, "product", 10, "meta_key", "featured", "meta_value", "1", "order", "title asc")); !reflect.DeepEqual(got, []uint{idFeatured}) {
		t.Fatalf("featured = %v", got)
	}
	if view := b.contentBySlugFunc(dataRequest("zh"), "product", "widget"); view == nil || view["ID"] != uint(idOtherLang) {
		t.Fatalf("zh widget = %v", view)
	}
	if view := b.contentBySlugFunc(en, "product", "scheduled"); view != nil {
		t.Fatal("scheduled content was returned")
	}
	names := []string{}
	for _, view := range b.taxonomyTermsFunc(en, "category", "order", "name") {
		names = append(names, fmt.Sprint(view["Name"]))
	}
	if strings.Join(names, ",") != "Empty,News" {
		t.Fatalf("categories by name = %v", names)
	}

	items, err := content.NewQuery(content.ScopedDB(en, db)).Published().Types([]string{"product", "post"}).OrderStable("published_at", "DESC").Get()
	if err != nil {
		t.Fatal(err)
	}
	queries := countQueries(t, db)
	views := b.contentViews(en, items)
	if *queries != 2 || len(views) != 5 {
		t.Fatalf("contentViews: %d views in %d queries", len(views), *queries)
	}
}

// An archive of a sort_order type filtered by taxonomy joins
// term_relationships, which has its own sort_order column; the page must still
// render on PostgreSQL with only the matching items.
func TestPGArchiveTaxonomyFilterWithSortOrder(t *testing.T) {
	b, _ := pgDataFuncTheme(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/products?product_cat=widgets", nil)
	b.renderArchive(c, &rewrite.ResolvedRoute{ContentType: "product", Page: 1})
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Widget") || strings.Contains(body, "Gadget") {
		t.Fatalf("status %d, body:\n%s", rec.Code, body)
	}
}
