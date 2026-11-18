// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package themepkg

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/pkg/sign"
)

const sampleDir = "testdata/minimal"

// sample returns the sample package's files with changes applied; an empty
// change value deletes the file.
func sample(t *testing.T, changes map[string]string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := fs.WalkDir(os.DirFS(sampleDir), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(filepath.Join(sampleDir, filepath.FromSlash(name)))
		files[name] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range changes {
		if content == "" {
			delete(files, name)
		} else {
			files[name] = content
		}
	}
	return files
}

// withPage replaces the sample's home page content block.
func withPage(t *testing.T, content string) map[string]string {
	return sample(t, map[string]string{"templates/pages/home.tmpl": `{{define "content"}}` + content + `{{end}}`})
}

// zipFiles packs files with WriteZip.
func zipFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var list []File
	for name, content := range files {
		list = append(list, File{Name: name, Data: []byte(content)})
	}
	var buf bytes.Buffer
	if err := WriteZip(&buf, list); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// check packs files and checks the archive.
func check(t *testing.T, files map[string]string, opts ...Options) *Report {
	t.Helper()
	data := zipFiles(t, files)
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	report, err := CheckZip(bytes.NewReader(data), int64(len(data)), o)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// hasProblem reports whether report lists a problem with rule whose path
// contains path.
func hasProblem(report *Report, rule, path string) bool {
	for _, p := range report.Problems {
		if p.Rule == rule && strings.Contains(p.Path, path) {
			return true
		}
	}
	return false
}

func hasWarning(report *Report, rule string) bool {
	for _, p := range report.Warnings {
		if p.Rule == rule {
			return true
		}
	}
	return false
}

func expectProblem(t *testing.T, report *Report, rule, path string) {
	t.Helper()
	if !hasProblem(report, rule, path) {
		t.Fatalf("want problem %s at %q, got %v", rule, path, report.Problems)
	}
}

func expectOK(t *testing.T, report *Report) {
	t.Helper()
	if !report.OK() {
		t.Fatalf("want no problems, got %v", report.Problems)
	}
}

func TestSamplePackagePasses(t *testing.T) {
	report, err := CheckDir(os.DirFS(sampleDir), Options{CoreVersion: "0.7.22"})
	if err != nil {
		t.Fatal(err)
	}
	expectOK(t, report)
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings = %v", report.Warnings)
	}
	want := sign.PackageID{Kind: Kind, Slug: "minimal-sample", Version: "1.0.0"}
	if report.ID != want || report.Files != 8 || report.Size == 0 || len(report.External) != 2 {
		t.Fatalf("report = %+v", report)
	}
	digest, err := sign.PackageDigest(os.DirFS(sampleDir))
	if err != nil || report.Digest != digest {
		t.Fatalf("digest = %q, sign.PackageDigest = %q (%v)", report.Digest, digest, err)
	}
}

func TestBuildIsReproducibleAndMatchesTheDigest(t *testing.T) {
	src := t.TempDir()
	for name, content := range sample(t, nil) {
		writeFile(t, src, name, content)
	}
	// Left out: system files, hidden paths and signatures.
	writeFile(t, src, ".DS_Store", "junk")
	writeFile(t, src, ".git/config", "[core]")
	writeFile(t, src, "static/.env", "SECRET=1")
	writeFile(t, src, "signatures/publisher.json", "{}")

	var first, second bytes.Buffer
	report, err := Build(&first, os.DirFS(src), Options{})
	if err != nil {
		t.Fatal(err)
	}
	expectOK(t, report)
	if !hasWarning(report, RuleSourceHidden) || !strings.Contains(report.Warnings[0].Message, ".git") || strings.Contains(report.Warnings[0].Message, ".DS_Store") {
		t.Fatalf("warnings = %v", report.Warnings)
	}
	if _, err := Build(&second, os.DirFS(src), Options{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("two builds of the same files differ")
	}

	archive, err := zip.NewReader(bytes.NewReader(first.Bytes()), int64(first.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range archive.File {
		if strings.Contains(f.Name, ".") && strings.HasPrefix(filepath.Base(f.Name), ".") || strings.HasPrefix(f.Name, "signatures/") || strings.HasPrefix(f.Name, ".git") {
			t.Fatalf("the archive contains %s", f.Name)
		}
		if f.Method != zip.Deflate || f.Mode().Perm() != 0o644 || !f.Modified.Equal(zipTime) {
			t.Fatalf("entry %s: method %d mode %v time %v", f.Name, f.Method, f.Mode(), f.Modified)
		}
	}
	if len(archive.File) != report.Files {
		t.Fatalf("archive has %d entries, report %d files", len(archive.File), report.Files)
	}
	digest, err := sign.PackageDigest(archive)
	if err != nil || digest != report.Digest {
		t.Fatalf("archive digest %q, report %q (%v)", digest, report.Digest, err)
	}
	zipReport, err := CheckZip(bytes.NewReader(first.Bytes()), int64(first.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	expectOK(t, zipReport)
	if zipReport.Digest != report.Digest {
		t.Fatalf("CheckZip digest %q, Build %q", zipReport.Digest, report.Digest)
	}
}

func TestBuildWritesNothingWhenTheCheckFails(t *testing.T) {
	src := t.TempDir()
	for name, content := range sample(t, map[string]string{"static/js/app.js": "alert(1)"}) {
		writeFile(t, src, name, content)
	}
	var out bytes.Buffer
	report, err := Build(&out, os.DirFS(src), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() || out.Len() != 0 {
		t.Fatalf("ok = %v, wrote %d bytes", report.OK(), out.Len())
	}
	expectProblem(t, report, RuleScriptFile, "static/js/app.js")
}

func TestWriteZipRejectsDuplicates(t *testing.T) {
	err := WriteZip(&bytes.Buffer{}, []File{{Name: "a.txt"}, {Name: "a.txt"}})
	if err == nil {
		t.Fatal("duplicate names were written")
	}
}

func TestReportListsAtMostMaxProblems(t *testing.T) {
	files := sample(t, nil)
	for i := 0; i < MaxProblems+5; i++ {
		files["static/x"+strings.Repeat("y", i%50)+string(rune('a'+i%26))+strings.Repeat("z", i/26)+".exe"] = "x"
	}
	report := check(t, files)
	if len(report.Problems) != MaxProblems || report.Omitted == 0 || report.OK() {
		t.Fatalf("problems = %d, omitted = %d", len(report.Problems), report.Omitted)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
