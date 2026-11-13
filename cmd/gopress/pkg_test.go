package main

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pkgRun(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runPkg(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func zipDir(t *testing.T, dir, target string) {
	t.Helper()
	out, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = out.Close()
}

func TestPkgKeygenSignVerifyFlow(t *testing.T) {
	work := t.TempDir()
	publisherKey := filepath.Join(work, "keys", "publisher")
	marketKey := filepath.Join(work, "keys", "market")

	for _, prefix := range []string{publisherKey, marketKey} {
		if code, out, errOut := pkgRun(t, "keygen", "-out", prefix); code != 0 || !strings.Contains(out, "kid:") {
			t.Fatalf("keygen %s: code=%d out=%s err=%s", prefix, code, out, errOut)
		}
	}
	info, err := os.Stat(publisherKey + ".key")
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	if code, _, errOut := pkgRun(t, "keygen", "-out", publisherKey); code != 1 || !strings.Contains(errOut, "already exists") {
		t.Fatalf("keygen overwrote a key without -force: code=%d err=%s", code, errOut)
	}
	marketPub, _ := os.ReadFile(marketKey + ".pub")

	pkgDir := filepath.Join(work, "aurora")
	writeTree(t, pkgDir, map[string]string{
		"theme.toml":                  "[theme]\nname = \"Aurora\"\nversion = \"1.2.0\"\n",
		"templates/layouts/base.tmpl": "{{define \"base\"}}ok{{end}}",
		"static/css/style.css":        "body{}",
	})

	// A countersignature needs a publisher signature first.
	if code, _, errOut := pkgRun(t, "sign", "-key", marketKey+".key", "-role", "market", pkgDir); code != 1 || !strings.Contains(errOut, "refusing to countersign") {
		t.Fatalf("market signed an unsigned package: code=%d err=%s", code, errOut)
	}
	// The slug defaults to the directory name, the version to theme.toml.
	code, out, errOut := pkgRun(t, "sign", "-key", publisherKey+".key", pkgDir)
	if code != 0 || !strings.Contains(out, "signed theme aurora 1.2.0 as publisher") {
		t.Fatalf("publisher sign: code=%d out=%s err=%s", code, out, errOut)
	}
	if code, _, _ := pkgRun(t, "sign", "-key", publisherKey+".key", pkgDir); code != 1 {
		t.Fatal("re-signing without -force must fail")
	}
	if code, out, errOut := pkgRun(t, "sign", "-key", marketKey+".key", "-role", "market", pkgDir); code != 0 || !strings.Contains(out, "as market") {
		t.Fatalf("market sign: code=%d out=%s err=%s", code, out, errOut)
	}

	code, out, errOut = pkgRun(t, "verify", "-market-key", strings.TrimSpace(string(marketPub)), "-require-market", pkgDir)
	if code != 0 || !strings.Contains(out, "publisher signature: ok") || !strings.Contains(out, "market signature:    ok") {
		t.Fatalf("verify directory: code=%d out=%s err=%s", code, out, errOut)
	}
	_, dirDigest, _ := pkgRun(t, "digest", pkgDir)

	// The same package as a .zip archive verifies too and has the same
	// digest. An archive has no directory name, so the slug is given.
	archive := filepath.Join(work, "aurora-1.2.0.zip")
	zipDir(t, pkgDir, archive)
	if code, out, errOut := pkgRun(t, "verify", "-slug", "aurora", "-market-key", strings.TrimSpace(string(marketPub)), "-require-market", archive); code != 0 {
		t.Fatalf("verify archive: code=%d out=%s err=%s", code, out, errOut)
	}
	if _, zipDigest, _ := pkgRun(t, "digest", archive); zipDigest != dirDigest || !strings.HasPrefix(zipDigest, "h1:") {
		t.Fatalf("archive digest %q != directory digest %q", zipDigest, dirDigest)
	}

	// Without a market key the countersignature is reported as unchecked.
	if code, out, _ := pkgRun(t, "verify", pkgDir); code != 0 || !strings.Contains(out, "not checked") {
		t.Fatalf("verify without market key: code=%d out=%s", code, out)
	}
	// An untrusted market key fails.
	publisherPub, _ := os.ReadFile(publisherKey + ".pub")
	if code, _, _ := pkgRun(t, "verify", "-market-key", strings.TrimSpace(string(publisherPub)), pkgDir); code != 1 {
		t.Fatal("verification passed with an untrusted market key")
	}
	// Tampering with any file breaks verification.
	writeTree(t, pkgDir, map[string]string{"static/css/style.css": "body{color:red}"})
	if code, _, errOut := pkgRun(t, "verify", pkgDir); code != 1 || !strings.Contains(errOut, "does not verify") {
		t.Fatalf("tampered package verified: code=%d err=%s", code, errOut)
	}
}

func TestPkgUsageErrors(t *testing.T) {
	if code, out, _ := pkgRun(t); code != 2 || !strings.Contains(out, "gopress pkg") {
		t.Fatalf("no args: code=%d", code)
	}
	if code, _, _ := pkgRun(t, "help"); code != 0 {
		t.Fatal("help must succeed")
	}
	for _, args := range [][]string{
		{"nope"},
		{"keygen"},
		{"sign", "-key", "x.key"},
		{"sign", "-key", "x.key", "-role", "admin", t.TempDir()},
		{"verify"},
		{"digest"},
	} {
		if code, _, _ := pkgRun(t, args...); code != 2 {
			t.Errorf("%v: code=%d, want 2", args, code)
		}
	}
	// Slug from theme.toml takes precedence over the directory name, and a
	// bad slug is rejected.
	dir := filepath.Join(t.TempDir(), "Bad_Dir")
	writeTree(t, dir, map[string]string{"theme.toml": "[theme]\nversion = \"1.0.0\"\n"})
	if code, _, errOut := pkgRun(t, "digest", dir); code != 0 {
		t.Fatalf("digest: %s", errOut)
	}
	if code, _, errOut := pkgRun(t, "verify", dir); code != 1 || !strings.Contains(errOut, "invalid package slug") {
		t.Fatalf("bad directory slug accepted: code=%d err=%s", code, errOut)
	}
}
