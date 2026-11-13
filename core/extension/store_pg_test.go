// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/0xmattg/go-press/pkg/dbprefix"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// pgSite creates a throwaway schema in GOPRESS_TEST_PG_DSN and returns a
// function that opens a connection pool for a site with the given prefix,
// the way pkg/postgresql does (prefix set before the pool exists).
func pgSite(t *testing.T) (string, func(prefix string) *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("GOPRESS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set GOPRESS_TEST_PG_DSN to run PostgreSQL integration tests")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	schemaName := fmt.Sprintf("gopress_ext_itest_%d", time.Now().UnixNano())
	if err := admin.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatal(err)
	}
	var pools []*gorm.DB
	t.Cleanup(func() {
		dbprefix.Set(dbprefix.DefaultPrefix)
		for _, db := range pools {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		_ = admin.Exec(`DROP SCHEMA "` + schemaName + `" CASCADE`).Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return schemaName, func(prefix string) *gorm.DB {
		dbprefix.Set(prefix)
		db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), &gorm.Config{
			Logger: gormlogger.Discard, NamingStrategy: schema.NamingStrategy{TablePrefix: prefix},
		})
		if err != nil {
			t.Fatal(err)
		}
		pools = append(pools, db)
		return db
	}
}

func indexNames(t *testing.T, db *gorm.DB, table string) []string {
	t.Helper()
	var names []string
	if err := db.Raw(`SELECT indexname FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? ORDER BY indexname`, table).
		Scan(&names).Error; err != nil {
		t.Fatal(err)
	}
	return names
}

func TestPGCreateTablesV1IsIdempotentAndSiteScoped(t *testing.T) {
	_, open := pgSite(t)
	ctx := context.Background()

	for _, prefix := range []string{"gp_", "gp_x_"} {
		db := open(prefix)
		for pass := 0; pass < 2; pass++ {
			if err := CreateTablesV1(ctx, db); err != nil {
				t.Fatalf("%s pass %d: %v", prefix, pass, err)
			}
		}
		got := indexNames(t, db, prefix+"ext_packages")
		want := []string{prefix + "ext_packages_pkey", prefix + "ext_packages_uniq_current", prefix + "ext_packages_uniq_version"}
		sort.Strings(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s indexes = %v, want %v", prefix, got, want)
		}
		var rows int64
		if err := db.Table(prefix + "ext_site").Count(&rows).Error; err != nil || rows != 1 {
			t.Fatalf("%s ext_site rows = %d, %v; want exactly 1", prefix, rows, err)
		}
	}
}

// Two processes that receive different catalogs under the same sequence at
// the same moment: the row lock makes one of them see the other's catalog,
// so exactly one is stored and the other reports an equivocation.
func TestPGMarketRacingCatalogsUnderOneSequence(t *testing.T) {
	_, open := pgSite(t)
	ctx := context.Background()
	if err := CreateTablesV1(ctx, open("gp_")); err != nil {
		t.Fatal(err)
	}
	k := newTestKeys(t)
	var markets [2]*Market
	var servers [2]*fakeMarket
	for i := range markets {
		servers[i] = newFakeMarket(t)
		markets[i] = NewMarket(NewStore(open("gp_")), k.root(t, servers[i].endpoint()), nil, "")
		markets[i].now = func() time.Time { return testNow }
	}
	for round := int64(1); round <= 10; round++ {
		servers[0].set(newPayload(round, k.signedItem(t, "aurora", "1.0.0")).envelope(t, k.catalog))
		servers[1].set(newPayload(round, k.signedItem(t, "borealis", "1.0.0")).envelope(t, k.catalog))
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range markets {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, errs[i] = markets[i].Refresh(ctx)
			}(i)
		}
		wg.Wait()
		ok, equivocations := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrCatalogEquivocation):
				equivocations++
			default:
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if ok != 1 || equivocations != 1 {
			t.Fatalf("round %d: %d stored, %d equivocations; want 1 and 1", round, ok, equivocations)
		}
	}
}

func TestPGStoreEnforcesOneCurrentAndSerializesWriters(t *testing.T) {
	_, open := pgSite(t)
	ctx := context.Background()
	db := open("gp_")
	if err := CreateTablesV1(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)

	a := theme("aurora", "1.0.0", "h1:aaa=")
	b := theme("aurora", "1.1.0", "h1:bbb=")
	if err := s.Add(ctx, a, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(ctx, b, false); err != nil {
		t.Fatal(err)
	}
	// The database itself refuses a second current version.
	if err := db.Model(&Package{}).Where("id = ?", b.ID).Update("is_current", true).Error; err == nil {
		t.Fatal("partial unique index allowed two current versions")
	}

	// Concurrent writers from separate pools (separate "processes"): every
	// write counts once, and racing makeCurrent adds never break the
	// one-current rule.
	const writers, perWriter = 4, 10
	stores := make([]*Store, writers)
	for i := range stores {
		stores[i] = NewStore(open("gp_"))
	}
	before := mustGeneration(t, s)
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				version := strconv.Itoa(w) + "." + strconv.Itoa(i)
				errs <- stores[w].Add(ctx, theme("racer", version, "h1:"+version+"="), true)
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Add: %v", err)
		}
	}
	if got := mustGeneration(t, s); got != before+writers*perWriter {
		t.Fatalf("generation = %d, want %d", got, before+writers*perWriter)
	}
	versions, _ := s.Versions(ctx, KindTheme, "racer")
	current := 0
	for _, v := range versions {
		if v.IsCurrent {
			current++
		}
	}
	if len(versions) != writers*perWriter || current != 1 {
		t.Fatalf("racer: %d versions, %d current; want %d and 1", len(versions), current, writers*perWriter)
	}
}
