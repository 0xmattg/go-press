// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package assets

import (
	"bytes"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestResolvePicksBuiltinDiskOrFallback(t *testing.T) {
	diskDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(diskDir, "marker.txt"), []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	builtin := fstest.MapFS{"marker.txt": {Data: []byte("builtin")}}
	missing := filepath.Join(diskDir, "missing")

	cases := []struct {
		name       string
		diskDir    string
		builtin    fs.FS
		preferDisk bool
		want       string
	}{
		{"release mode reads built-in files", diskDir, builtin, false, "builtin"},
		{"debug mode reads the directory on disk", diskDir, builtin, true, "disk"},
		{"debug mode without the directory reads built-in files", missing, builtin, true, "builtin"},
		{"module without built-in files reads disk", diskDir, nil, false, "disk"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := fs.ReadFile(Resolve(tc.diskDir, tc.builtin, tc.preferDisk), "marker.txt")
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.want {
				t.Fatalf("read %q, want %q", data, tc.want)
			}
		})
	}
}

func TestDirFSEmptyMeansWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "here.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if !Exists(DirFS(""), "here.txt") {
		t.Fatal(`DirFS("") must be rooted at the working directory, not "/"`)
	}
}

func TestExistsAndSub(t *testing.T) {
	fsys := fstest.MapFS{"templates/admin/settings.tmpl": {Data: []byte("x")}}
	if !Exists(fsys, "templates/admin/settings.tmpl") {
		t.Fatal("file should exist")
	}
	for _, name := range []string{"templates/admin", "missing.tmpl", "../escape.tmpl", "/abs.tmpl"} {
		if Exists(fsys, name) {
			t.Fatalf("Exists(%q) = true, want false", name)
		}
	}
	if Exists(nil, "x") || Sub(nil, "templates") != nil {
		t.Fatal("nil filesystem must be treated as empty")
	}
	if !Exists(Sub(fsys, "templates"), "admin/settings.tmpl") {
		t.Fatal("Sub should root the filesystem at the subdirectory")
	}
}

func TestParseFilesFollowsTemplateParseFilesNaming(t *testing.T) {
	fsys := fstest.MapFS{
		"layouts/base.tmpl":   {Data: []byte(`{{define "base"}}({{template "content" .}}){{end}}`)},
		"pages/[draft].tmpl":  {Data: []byte(`{{define "content"}}draft{{end}}`)},
		"partials/named.tmpl": {Data: []byte(`root body`)},
	}
	tmpl, err := ParseFiles(template.New(""), fsys, "layouts/base.tmpl", "pages/[draft].tmpl")
	if err != nil {
		t.Fatalf("names with glob characters must be read literally: %v", err)
	}
	for _, name := range []string{"base.tmpl", "[draft].tmpl", "base", "content"} {
		if tmpl.Lookup(name) == nil {
			t.Fatalf("template %q missing after ParseFiles", name)
		}
	}
	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "base", nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "(draft)" {
		t.Fatalf("rendered %q, want (draft)", out.String())
	}

	// A file whose base name equals the root template's name fills the root.
	root, err := ParseFiles(template.New("named.tmpl"), fsys, "partials/named.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := root.Execute(&out, nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "root body" {
		t.Fatalf("root template = %q, want the file body", out.String())
	}

	if _, err := ParseFiles(template.New(""), fsys, "missing.tmpl"); err == nil {
		t.Fatal("missing file must fail")
	}
	if _, err := ParseFiles(template.New(""), fsys); err == nil {
		t.Fatal("no names must fail")
	}
	if _, err := ParseFiles(template.New(""), nil, "layouts/base.tmpl"); err == nil {
		t.Fatal("nil filesystem must fail")
	}
}

func TestTemplateNamesListsSortedTemplatesOnly(t *testing.T) {
	fsys := fstest.MapFS{
		"partials/b.tmpl":        {Data: []byte("b")},
		"partials/a.tmpl":        {Data: []byte("a")},
		"partials/readme.md":     {Data: []byte("skip")},
		"partials/nested/c.tmpl": {Data: []byte("skip")},
	}
	got, err := TemplateNames(fsys, "partials")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"partials/a.tmpl", "partials/b.tmpl"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TemplateNames = %v, want %v", got, want)
	}
	if got, err := TemplateNames(fsys, "missing"); err != nil || got != nil {
		t.Fatalf("missing dir = %v, %v; want nil, nil", got, err)
	}
}
