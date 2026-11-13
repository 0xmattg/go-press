// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"strings"
	"testing"

	"github.com/0xmattg/go-press/config"
)

// A partial [market] section must stop the server before it touches the
// database, so a site never runs with a mix of markets and keys.
func TestBuildAndBootstrapRejectsPartialMarketConfig(t *testing.T) {
	cfg := &config.Config{
		CMS: config.CMSConfig{JWTSecret: "a-unique-random-secret-for-this-test"},
		// Nothing listens here: reaching the database step would fail with a
		// connection error instead of the market error.
		PG:     config.PGConfig{Hostname: "127.0.0.1", Port: "1", User: "nobody", Database: "none"},
		Market: config.MarketConfig{Endpoint: "https://market.example.com/api/market/v1"},
	}
	engine, err := BuildAndBootstrap(cfg, "/tmp/site/config.toml", false)
	if engine != nil {
		t.Fatal("BuildAndBootstrap returned an engine")
	}
	if err == nil || !strings.Contains(err.Error(), "invalid [market]") || !strings.Contains(err.Error(), "catalog_keys, package_keys") {
		t.Fatalf("err = %v", err)
	}
}
