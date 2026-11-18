package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The installer saves configs with an empty market section; it must not
// write one, so a fresh config never carries a half-filled [market].
func TestSaveOmitsEmptyMarketSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(path, &Config{Site: SiteConfig{Name: "Example"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "market") {
		t.Fatalf("saved config mentions market:\n%s", data)
	}
}

func TestMarketSectionRoundTrips(t *testing.T) {
	want := MarketConfig{
		Endpoint:    "https://market.example.com/api/market/v1",
		CatalogKeys: []string{"Y2F0YWxvZw=="},
		PackageKeys: []string{"cGFja2FnZQ==", "cGFja2FnZTI="},
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(path, &Config{Market: want}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Market, want) {
		t.Fatalf("Market = %+v, want %+v", got.Market, want)
	}
	if got.Market.IsZero() {
		t.Fatal("IsZero() = true for a configured market")
	}
}

func TestMarketConfigIsZero(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cfg  MarketConfig
		want bool
	}{
		{MarketConfig{}, true},
		{MarketConfig{Endpoint: "   "}, true},
		{MarketConfig{Endpoint: "https://m.example"}, false},
		{MarketConfig{CatalogKeys: []string{"k"}}, false},
		{MarketConfig{PackageKeys: []string{"k"}}, false},
		// The developer-mode lock alone keeps the official market.
		{MarketConfig{LockDeveloperMode: true}, true},
	}
	for _, tc := range cases {
		if got := tc.cfg.IsZero(); got != tc.want {
			t.Errorf("%+v.IsZero() = %v, want %v", tc.cfg, got, tc.want)
		}
	}
}

func TestMarketLockRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[market]\nlock_developer_mode = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Market.LockDeveloperMode || !got.Market.IsZero() {
		t.Fatalf("Market = %+v", got.Market)
	}
	if err := Save(path, got); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "lock_developer_mode = true") || strings.Contains(string(data), "endpoint") {
		t.Fatalf("saved config:\n%s", data)
	}
}
