package gopressanalytics

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestGeoIPLookupCountryFromCSVGzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dbip-country-lite.csv.gz")
	if err := writeGeoIPTestFile(path, "203.0.113.0,203.0.113.255,US\n2001:db8::,2001:db8::ffff,CN\n"); err != nil {
		t.Fatalf("write geoip fixture: %v", err)
	}
	db := newGeoIPDatabase(path)
	if err := db.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := db.LookupCountry("203.0.113.8"); got != "US" {
		t.Fatalf("LookupCountry IPv4 = %q, want US", got)
	}
	if got := db.LookupCountry("2001:db8::1"); got != "CN" {
		t.Fatalf("LookupCountry IPv6 = %q, want CN", got)
	}
	if got := db.LookupCountry("127.0.0.1"); got != "" {
		t.Fatalf("LookupCountry loopback = %q, want empty", got)
	}
	status := db.Status()
	if !status.Available || status.Records != 2 {
		t.Fatalf("status = %#v, want available with 2 records", status)
	}
}

func writeGeoIPTestFile(path, content string) error {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(content)); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
