package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTrustedProxyListDefaultsToLoopback(t *testing.T) {
	t.Parallel()
	got := ServerConfig{}.TrustedProxyList()
	if !reflect.DeepEqual(got, DefaultTrustedProxies) {
		t.Fatalf("TrustedProxyList() = %v, want %v", got, DefaultTrustedProxies)
	}
	got[0] = "0.0.0.0/0"
	if DefaultTrustedProxies[0] != "127.0.0.1" {
		t.Fatal("TrustedProxyList() returned the shared default slice")
	}
}

func TestTrustedProxyListTrimsBlankEntries(t *testing.T) {
	t.Parallel()
	got := ServerConfig{TrustedProxies: []string{" 10.0.0.0/8 ", "", "  ", "::1"}}.TrustedProxyList()
	if want := []string{"10.0.0.0/8", "::1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("TrustedProxyList() = %v, want %v", got, want)
	}
	blank := ServerConfig{TrustedProxies: []string{" ", ""}}.TrustedProxyList()
	if !reflect.DeepEqual(blank, DefaultTrustedProxies) {
		t.Fatalf("TrustedProxyList() with only blanks = %v, want default", blank)
	}
}

// Load uses the global viper instance, so this test is intentionally not
// parallel.
func TestTrustedProxiesLoadAndSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	withProxies := filepath.Join(dir, "with.toml")
	if err := os.WriteFile(withProxies, []byte("[server]\nport = 8081\ntrusted_proxies = [\"127.0.0.1\", \"198.51.100.0/24\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(withProxies)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if want := []string{"127.0.0.1", "198.51.100.0/24"}; !reflect.DeepEqual(loaded.Server.TrustedProxies, want) {
		t.Fatalf("loaded trusted proxies = %v, want %v", loaded.Server.TrustedProxies, want)
	}

	// Existing site configs re-saved by the admin must not gain the key.
	withoutProxies := filepath.Join(dir, "without.toml")
	if err := Save(withoutProxies, &Config{Server: ServerConfig{Host: "127.0.0.1", Port: 8081}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err := os.ReadFile(withoutProxies)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "trusted_proxies") {
		t.Fatalf("Save() wrote an empty trusted_proxies key:\n%s", data)
	}
}
