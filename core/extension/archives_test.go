// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package extension

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// h1 returns an h1 digest of s.
func h1(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "h1:" + base64.StdEncoding.EncodeToString(sum[:])
}

func TestArchivesPathNamesVersionsByDigest(t *testing.T) {
	a := NewArchives("/site/packages/archives")
	// An h1 digest whose base64 has "/" and "+": the name must not.
	sum := [32]byte{0xfb, 0xff, 0xbf, 0x01}
	digest := "h1:" + base64.StdEncoding.EncodeToString(sum[:])
	path, err := a.Path("aurora", "1.2.0", digest)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/site/packages/archives", "aurora", "1.2.0-fbffbf01.zip"); path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
	other, _ := a.Path("aurora", "1.2.0", h1("other"))
	if other == path {
		t.Fatal("different content got the same name")
	}
	for _, bad := range []struct{ slug, version, digest string }{
		{"../x", "1.0.0", h1("x")},
		{"Aurora", "1.0.0", h1("x")},
		{"aurora", "1.0.0/../../x", h1("x")},
		{"aurora", "not-a-version", h1("x")},
		{"aurora", "1.0.0", "sha256:abc"},
		{"aurora", "1.0.0", "h1:YWJj"},
		{"aurora", "1.0.0", "h1:!!!"},
	} {
		if _, err := a.Path(bad.slug, bad.version, bad.digest); err == nil {
			t.Errorf("Path(%q, %q, %q) accepted", bad.slug, bad.version, bad.digest)
		}
	}
}

func TestArchivesPutRemoveAndFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "packages", "archives")
	a := NewArchives(root)
	if files, err := a.Files(); err != nil || len(files) != 0 {
		t.Fatalf("empty store: %v, %v", files, err)
	}
	digest := h1("aurora 1.0.0")
	path, size, err := a.Put("aurora", "1.0.0", digest, []byte("zip bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len("zip bytes")) {
		t.Fatalf("size = %d", size)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "zip bytes" {
		t.Fatalf("stored %q, %v", data, err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		dirInfo, _ := os.Stat(filepath.Dir(path))
		if info.Mode().Perm() != archiveFileMode || dirInfo.Mode().Perm() != archiveDirMode {
			t.Fatalf("modes file %v dir %v", info.Mode().Perm(), dirInfo.Mode().Perm())
		}
	}

	// The same version and digest again keeps the stored file.
	again, againSize, err := a.Put("aurora", "1.0.0", digest, []byte("other zip bytes, same content"))
	if err != nil || again != path || againSize != size {
		t.Fatalf("second put = %s %d %v", again, againSize, err)
	}
	if data, _ := os.ReadFile(path); string(data) != "zip bytes" {
		t.Fatal("a stored archive was overwritten")
	}

	second, _, err := a.Put("aurora", "1.1.0", h1("aurora 1.1.0"), []byte("v2"))
	if err != nil {
		t.Fatal(err)
	}
	// Temporary and hidden files are not archives.
	if err := os.WriteFile(filepath.Join(root, "aurora", archiveTempPrefix+"x"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := a.Files()
	if err != nil || len(files) != 2 {
		t.Fatalf("files = %v, %v", files, err)
	}

	if err := a.Remove("aurora", "1.0.0", digest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("removed archive still present")
	}
	if err := a.Remove("aurora", "1.0.0", digest); err != nil {
		t.Fatalf("removing a missing archive: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatal("removing one version removed another")
	}
}

func TestArchivesRemoveStaleTemp(t *testing.T) {
	root := t.TempDir()
	a := NewArchives(root)
	dir := filepath.Join(root, "aurora")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, archiveTempPrefix+"old")
	fresh := filepath.Join(dir, archiveTempPrefix+"fresh")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveStaleTemp(time.Hour)
	if err != nil || removed != 1 {
		t.Fatalf("removed %d, %v", removed, err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("stale temp file kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a write in progress was removed")
	}
	if n, err := NewArchives(filepath.Join(root, "missing")).RemoveStaleTemp(time.Hour); err != nil || n != 0 {
		t.Fatalf("missing root: %d, %v", n, err)
	}
}
