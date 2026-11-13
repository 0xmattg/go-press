package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/pkg/sign"
)

func readPub(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

func readSig(t *testing.T, path string) sign.Signature {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sig sign.Signature
	if err := json.Unmarshal(data, &sig); err != nil {
		t.Fatal(err)
	}
	return sig
}

// A local market end to end: sign a theme package as publisher and market,
// list it in a catalog payload, sign the catalog, and verify it like a site.
func TestPkgCatalogSignVerifyFlow(t *testing.T) {
	dir := t.TempDir()
	keys := filepath.Join(dir, "keys")
	for _, name := range []string{"publisher", "market", "catalog"} {
		if code, _, stderr := pkgRun(t, "keygen", "-out", filepath.Join(keys, name)); code != 0 {
			t.Fatalf("keygen %s: %s", name, stderr)
		}
	}
	themeDir := filepath.Join(dir, "aurora")
	writeTree(t, themeDir, map[string]string{
		"theme.toml":                  "[theme]\nslug = \"aurora\"\nversion = \"1.0.0\"\n",
		"templates/layouts/base.tmpl": "<html></html>",
	})
	for _, role := range []string{"publisher", "market"} {
		if code, _, stderr := pkgRun(t, "sign", "-key", filepath.Join(keys, role+".key"), "-role", role, themeDir); code != 0 {
			t.Fatalf("sign %s: %s", role, stderr)
		}
	}
	_, digest, _ := pkgRun(t, "digest", themeDir)
	publisherSig := readSig(t, filepath.Join(themeDir, "signatures", "publisher.json"))
	marketSig := readSig(t, filepath.Join(themeDir, "signatures", "market.json"))

	now := time.Now().UTC()
	payload := map[string]any{
		"schema": 1, "sequence": 3,
		"generated_at": now.Add(-time.Hour).Format(time.RFC3339),
		"expires_at":   now.Add(7 * 24 * time.Hour).Format(time.RFC3339),
		"items": []map[string]any{{
			"kind": "theme", "slug": "aurora", "version": "1.0.0",
			"digest": strings.TrimSpace(digest), "size": 2048,
			"download_url": "http://127.0.0.1:8099/download/aurora-1.0.0.zip",
			"pricing":      "free", "title": "Aurora",
			"publisher": map[string]string{"kid": publisherSig.KID, "public_key": publisherSig.PublicKey},
			"signatures": map[string]any{
				"publisher": sign.Signature{KID: publisherSig.KID, Alg: publisherSig.Alg, Sig: publisherSig.Sig},
				"market":    marketSig,
			},
		}},
		"revoked": []map[string]any{{"kind": "theme", "slug": "evil-x", "versions": []string{"*"}, "severity": "critical", "reason": "malware"}},
	}
	data, _ := json.Marshal(payload)
	payloadPath := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(payloadPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(dir, "catalog.json")
	if code, stdout, stderr := pkgRun(t, "catalog", "sign", "-key", filepath.Join(keys, "catalog.key"), "-out", catalogPath, payloadPath); code != 0 || !strings.Contains(stdout, "1 signature") {
		t.Fatalf("catalog sign: %d %s %s", code, stdout, stderr)
	}

	catalogPub := readPub(t, filepath.Join(keys, "catalog.pub"))
	marketPub := readPub(t, filepath.Join(keys, "market.pub"))
	endpoint := "http://127.0.0.1:8099/api/market/v1"
	code, stdout, stderr := pkgRun(t, "catalog", "verify", "-catalog-key", catalogPub, "-package-key", marketPub, "-endpoint", endpoint, catalogPath)
	if code != 0 {
		t.Fatalf("catalog verify: %s", stderr)
	}
	for _, want := range []string{"sequence 3", "items:     1", "theme aurora 1.0.0 free", "revoked:   1", "evil-x [*] critical/malware"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("verify output lacks %q:\n%s", want, stdout)
		}
	}

	// The official endpoint does not allow a loopback HTTP download URL.
	if code, _, stderr := pkgRun(t, "catalog", "verify", "-catalog-key", catalogPub, "-package-key", marketPub, catalogPath); code != 1 || !strings.Contains(stderr, "download_url") {
		t.Fatalf("verify against the official endpoint: %d %s", code, stderr)
	}
	// Wrong catalog key: rejected.
	if code, _, stderr := pkgRun(t, "catalog", "verify", "-catalog-key", marketPub, "-package-key", catalogPub, "-endpoint", endpoint, catalogPath); code != 1 || !strings.Contains(stderr, "signature") {
		t.Fatalf("verify with the wrong key: %d %s", code, stderr)
	}
	// A package key this verifier does not trust hides the item.
	other := filepath.Join(keys, "other")
	pkgRun(t, "keygen", "-out", other)
	if code, stdout, _ := pkgRun(t, "catalog", "verify", "-catalog-key", catalogPub, "-package-key", readPub(t, other+".pub"), "-endpoint", endpoint, catalogPath); code != 0 || !strings.Contains(stdout, "hidden:    1") {
		t.Fatalf("verify with an unknown package key: %d %s", code, stdout)
	}
	// Without -out the envelope goes to stdout.
	if code, stdout, _ := pkgRun(t, "catalog", "sign", "-key", filepath.Join(keys, "catalog.key"), payloadPath); code != 0 || !strings.Contains(stdout, `"format":"gopress-catalog-v1"`) {
		t.Fatalf("catalog sign to stdout: %d %s", code, stdout)
	}
}

func TestPkgCatalogUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"catalog"},
		{"catalog", "publish"},
		{"catalog", "sign", "payload.json"},
		{"catalog", "verify", "-catalog-key", "x", "catalog.json"},
	} {
		if code, _, _ := pkgRun(t, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
