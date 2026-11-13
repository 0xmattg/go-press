// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"strings"
	"testing"

	"github.com/0xmattg/go-press/config"
	"github.com/0xmattg/go-press/pkg/sign"
)

func publicKeys(t *testing.T, n int) []string {
	t.Helper()
	out := make([]string, n)
	for i := range out {
		key, err := sign.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = key.Public().String()
	}
	return out
}

func TestBuiltinTrustRoot(t *testing.T) {
	if len(builtinCatalogKeys) > 2 || len(builtinPackageKeys) > 2 {
		t.Fatal("at most two built-in keys per set")
	}
	root, err := NewTrustRoot(config.MarketConfig{})
	if err != nil {
		t.Fatalf("built-in keys do not form a valid trust root: %v", err)
	}
	if root.Endpoint != DefaultMarketEndpoint || root.Custom {
		t.Fatalf("root = %+v", root)
	}
	want := MarketOfficial
	if len(builtinCatalogKeys) == 0 || len(builtinPackageKeys) == 0 {
		want = MarketDisabled
	}
	if root.Mode() != want || root.Host() != "gopress.xyz" {
		t.Fatalf("Mode() = %q, Host() = %q", root.Mode(), root.Host())
	}
	if root.catalogURL() != DefaultMarketEndpoint+"/catalog?schema=1" {
		t.Fatalf("catalogURL() = %q", root.catalogURL())
	}
}

func TestCustomTrustRoot(t *testing.T) {
	keys := publicKeys(t, 3)
	root, err := NewTrustRoot(config.MarketConfig{
		Endpoint:    " https://market.example.com/api/market/v1/ ",
		CatalogKeys: keys[:2],
		PackageKeys: keys[2:],
	})
	if err != nil {
		t.Fatal(err)
	}
	if !root.Enabled() || root.Mode() != MarketCustom || !root.Custom || root.Host() != "market.example.com" {
		t.Fatalf("root = %+v, mode %s", root, root.Mode())
	}
	if root.Endpoint != "https://market.example.com/api/market/v1" {
		t.Fatalf("Endpoint = %q", root.Endpoint)
	}
	if len(root.CatalogKeys) != 2 || len(root.PackageKeys) != 1 {
		t.Fatalf("keys = %d catalog, %d package", len(root.CatalogKeys), len(root.PackageKeys))
	}

	for _, endpoint := range []string{"http://127.0.0.1:8099/api/market/v1", "http://localhost:8099", "http://[::1]:8099/m"} {
		root, err := NewTrustRoot(config.MarketConfig{Endpoint: endpoint, CatalogKeys: keys[:1], PackageKeys: keys[2:]})
		if err != nil {
			t.Errorf("%s: %v", endpoint, err)
			continue
		}
		if !root.allowsLoopbackHTTP() {
			t.Errorf("%s: loopback HTTP market must allow loopback HTTP downloads", endpoint)
		}
	}
}

func TestTrustRootRejectsBadConfig(t *testing.T) {
	keys := publicKeys(t, 6)
	endpoint := "https://market.example.com/api/market/v1"
	cases := map[string]struct {
		cfg  config.MarketConfig
		want string
	}{
		"endpoint only":       {config.MarketConfig{Endpoint: endpoint}, "missing catalog_keys, package_keys"},
		"keys only":           {config.MarketConfig{CatalogKeys: keys[:1], PackageKeys: keys[1:2]}, "missing endpoint"},
		"no package keys":     {config.MarketConfig{Endpoint: endpoint, CatalogKeys: keys[:1]}, "missing package_keys"},
		"plain http":          {config.MarketConfig{Endpoint: "http://market.example.com", CatalogKeys: keys[:1], PackageKeys: keys[1:2]}, "must use https"},
		"other scheme":        {config.MarketConfig{Endpoint: "ftp://market.example.com", CatalogKeys: keys[:1], PackageKeys: keys[1:2]}, "must use https"},
		"relative":            {config.MarketConfig{Endpoint: "/api/market/v1", CatalogKeys: keys[:1], PackageKeys: keys[1:2]}, "not an absolute URL"},
		"query":               {config.MarketConfig{Endpoint: endpoint + "?x=1", CatalogKeys: keys[:1], PackageKeys: keys[1:2]}, "query"},
		"credentials":         {config.MarketConfig{Endpoint: "https://u:p@market.example.com", CatalogKeys: keys[:1], PackageKeys: keys[1:2]}, "credentials"},
		"fragment":            {config.MarketConfig{Endpoint: endpoint + "#x", CatalogKeys: keys[:1], PackageKeys: keys[1:2]}, "fragment"},
		"key not base64":      {config.MarketConfig{Endpoint: endpoint, CatalogKeys: []string{"not a key"}, PackageKeys: keys[1:2]}, "catalog_keys"},
		"duplicate key":       {config.MarketConfig{Endpoint: endpoint, CatalogKeys: keys[:1], PackageKeys: []string{keys[1], keys[1]}}, "twice"},
		"key in both sets":    {config.MarketConfig{Endpoint: endpoint, CatalogKeys: keys[:1], PackageKeys: keys[:1]}, "both catalog_keys and package_keys"},
		"too many keys":       {config.MarketConfig{Endpoint: endpoint, CatalogKeys: keys[:5], PackageKeys: keys[5:]}, "at most 4"},
		"blank endpoint text": {config.MarketConfig{Endpoint: "   ", CatalogKeys: keys[:1], PackageKeys: keys[1:2]}, "missing endpoint"},
	}
	for name, tc := range cases {
		_, err := NewTrustRoot(tc.cfg)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.want)
		}
	}
}
