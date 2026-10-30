// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package cache

import (
	"os"
	"testing"
	"time"
)

// TestRedisNamespacesIsolateSites runs against a real Redis when
// GOPRESS_TEST_REDIS_ADDR is set (for example "127.0.0.1:6379"); it uses DB 15
// and only touches keys under its own test namespaces.
func TestRedisNamespacesIsolateSites(t *testing.T) {
	addr := os.Getenv("GOPRESS_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set GOPRESS_TEST_REDIS_ADDR to run Redis integration tests")
	}
	siteA := NewNamespacedRedisCache(addr, "", 15, "itest-site-a")
	siteB := NewNamespacedRedisCache(addr, "", 15, "itest-site-b")
	if siteA == nil || siteB == nil {
		t.Fatalf("cannot connect to Redis at %s", addr)
	}
	t.Cleanup(func() {
		siteA.Flush()
		siteB.Flush()
		_ = siteA.Close()
		_ = siteB.Close()
	})

	// Same page key (same path and language) on two sites sharing one DB.
	key := pageCachePrefix + hashKey("/about#lang=en")
	siteA.Set(key, []byte("site A page"), time.Minute)
	siteB.Set(key, []byte("site B page"), time.Minute)

	if got, ok := siteA.Get(key); !ok || string(got) != "site A page" {
		t.Fatalf("site A read %q, %v", got, ok)
	}
	if got, ok := siteB.Get(key); !ok || string(got) != "site B page" {
		t.Fatalf("site B read %q, %v", got, ok)
	}

	siteA.Flush()
	if _, ok := siteA.Get(key); ok {
		t.Fatal("site A key survived its own Flush")
	}
	if got, ok := siteB.Get(key); !ok || string(got) != "site B page" {
		t.Fatalf("site A Flush removed site B's page: %q, %v", got, ok)
	}
}
