package config

import (
	"strings"
	"testing"
)

func TestCacheNamespaceExplicitValueIsSanitized(t *testing.T) {
	cfg := &Config{Redis: RedisConfig{Namespace: " site a/b* "}}
	if got := cfg.CacheNamespace(); got != "site_a_b" {
		t.Fatalf("CacheNamespace() = %q, want site_a_b", got)
	}
	long := &Config{Redis: RedisConfig{Namespace: strings.Repeat("x", 100)}}
	if got := long.CacheNamespace(); len(got) != maxCacheNamespaceLen {
		t.Fatalf("CacheNamespace() length = %d, want %d", len(got), maxCacheNamespaceLen)
	}
}

func TestCacheNamespaceDerivedFromDatabaseIdentity(t *testing.T) {
	base := Config{PG: PGConfig{Hostname: "db", Port: "5432", Database: "gopress", TablePrefix: "gp_"}}
	same := base
	other := base
	other.PG.TablePrefix = "site2_"

	a, b, c := base.CacheNamespace(), same.CacheNamespace(), other.CacheNamespace()
	if a == "" || a != b {
		t.Fatalf("derived namespace not stable: %q vs %q", a, b)
	}
	if a == c {
		t.Fatalf("sites with different table prefixes share namespace %q", a)
	}
	if strings.ContainsAny(a, ": ") {
		t.Fatalf("derived namespace %q is not key-safe", a)
	}
}

func TestCacheNamespaceFallsBackWhenSanitizedEmpty(t *testing.T) {
	cfg := &Config{Redis: RedisConfig{Namespace: "///"}, PG: PGConfig{Database: "gopress"}}
	if got := cfg.CacheNamespace(); !strings.HasPrefix(got, "s") || len(got) != 13 {
		t.Fatalf("CacheNamespace() = %q, want derived s+12 hex", got)
	}
}
