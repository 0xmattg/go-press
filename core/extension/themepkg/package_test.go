// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package themepkg

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/0xmattg/go-press/pkg/sign"
)

// readZip packs files and reads the archive as a Package.
func readZip(t *testing.T, files map[string]string) (*Package, *Report) {
	t.Helper()
	data := zipFiles(t, files)
	pkg, report, err := ReadZip(bytes.NewReader(data), int64(len(data)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return pkg, report
}

func TestReadZipReturnsCheckedPackage(t *testing.T) {
	files := sample(t, map[string]string{"signatures/publisher.json": `{"kid":"x"}`})
	pkg, report := readZip(t, files)
	if pkg == nil {
		t.Fatalf("no package; problems %v", report.Problems)
	}
	if pkg.Report() != report || pkg.ID().Slug != "minimal-sample" || pkg.ID().Version != "1.0.0" {
		t.Fatalf("package = %+v, report = %+v", pkg.ID(), report)
	}
	digest, err := sign.PackageDigest(pkg.FS())
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Digest() == "" || pkg.Digest() != digest || pkg.Digest() != report.Digest {
		t.Fatalf("digest %q, recomputed %q, report %q", pkg.Digest(), digest, report.Digest)
	}
	var names []string
	for name := range files {
		names = append(names, name)
	}
	if err := fstest.TestFS(pkg.FS(), names...); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		data, err := fs.ReadFile(pkg.FS(), name)
		if err != nil || string(data) != content {
			t.Fatalf("%s = %q, %v", name, data, err)
		}
	}
}

func TestReadZipRejectsFailingPackage(t *testing.T) {
	pkg, report := readZip(t, sample(t, map[string]string{"static/js/main.js": "alert(1)"}))
	if pkg != nil {
		t.Fatal("a package with problems was returned")
	}
	expectProblem(t, report, RuleScriptFile, "static/js/main.js")

	data := zipFiles(t, sample(t, nil))
	big := make([]byte, MaxZipSize+1)
	copy(big, data)
	pkg, report, err := ReadZip(bytes.NewReader(big), int64(len(big)), Options{})
	if err != nil || pkg != nil {
		t.Fatalf("oversized archive: package %v, error %v", pkg, err)
	}
	expectProblem(t, report, RuleZipSize, "")
}

func TestReadDirLeavesOutHiddenFilesAndSignatures(t *testing.T) {
	src := t.TempDir()
	if err := os.CopyFS(src, os.DirFS(sampleDir)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, src, ".git/HEAD", "ref: refs/heads/main")
	writeFile(t, src, "signatures/publisher.json", "{}")
	pkg, report, err := ReadDir(os.DirFS(src), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if pkg == nil {
		t.Fatalf("no package; problems %v", report.Problems)
	}
	for _, name := range []string{".git/HEAD", "signatures/publisher.json", "signatures"} {
		if _, err := fs.Stat(pkg.FS(), name); err == nil {
			t.Fatalf("%s is in the package", name)
		}
	}
	if _, err := fs.Stat(pkg.FS(), "templates/pages/home.tmpl"); err != nil {
		t.Fatal(err)
	}

	// The package is a snapshot: later changes on disk do not reach it.
	writeFile(t, src, "static/css/style.css", "body{color:red}")
	data, err := fs.ReadFile(pkg.FS(), "static/css/style.css")
	if err != nil || bytes.Contains(data, []byte("color:red")) {
		t.Fatalf("style.css = %q, %v", data, err)
	}
}

func TestPackageFSIsReadOnly(t *testing.T) {
	pkg, _ := readZip(t, sample(t, nil))
	data, err := fs.ReadFile(pkg.FS(), "theme.toml")
	if err != nil {
		t.Fatal(err)
	}
	original := string(data)
	data[0] = 'X'
	again, _ := fs.ReadFile(pkg.FS(), "theme.toml")
	if string(again) != original {
		t.Fatal("changing a ReadFile result changed the package")
	}

	f, err := pkg.FS().Open("static/css/style.css")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, ok := f.(io.ReadSeeker); !ok {
		t.Fatal("package files must be seekable for range requests")
	}
	info, err := f.Stat()
	if err != nil || info.ModTime().IsZero() || info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("info = %v, %v", info, err)
	}
	for _, bad := range []string{"../theme.toml", "/theme.toml", "missing.css"} {
		if _, err := pkg.FS().Open(bad); err == nil {
			t.Fatalf("opened %q", bad)
		}
	}
}
