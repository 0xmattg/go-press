// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package admin

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func newLayoutTestHandler(t *testing.T) *Handler {
	t.Helper()
	dir := t.TempDir()
	layout := filepath.Join(dir, "layouts", "admin.tmpl")
	if err := os.MkdirAll(filepath.Dir(layout), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout, []byte(`{{define "admin"}}[{{template "content" .}}]{{end}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Handler{tmplDir: dir, funcMap: template.FuncMap{}}
}

func renderAdminLayout(t *testing.T, h *Handler, ref TemplateRef) string {
	t.Helper()
	tmpl, err := h.parseWithAdminLayout(ref)
	if err != nil {
		t.Fatalf("parseWithAdminLayout(%+v): %v", ref, err)
	}
	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "admin", nil); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestParseWithAdminLayoutReadsExtensionFilesystem(t *testing.T) {
	h := newLayoutTestHandler(t)
	ref := TemplateRef{
		FS:   fstest.MapFS{"templates/admin/orders.tmpl": {Data: []byte(`{{define "content"}}orders{{end}}`)}},
		Name: "templates/admin/orders.tmpl",
	}
	if got := renderAdminLayout(t, h, ref); got != "[orders]" {
		t.Fatalf("rendered %q, want the fragment inside the admin layout", got)
	}

	if _, err := h.parseWithAdminLayout(TemplateRef{}); err == nil {
		t.Fatal("the zero TemplateRef must fail instead of rendering an empty page")
	}
	missing := TemplateRef{FS: fstest.MapFS{}, Name: "templates/admin/missing.tmpl"}
	if _, err := h.parseWithAdminLayout(missing); err == nil {
		t.Fatal("a missing fragment must fail")
	}
}

func TestTemplateRefFromPathKeepsFilePathCallers(t *testing.T) {
	h := newLayoutTestHandler(t)
	path := filepath.Join(t.TempDir(), "templates", "admin", "settings.tmpl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{{define "content"}}legacy settings{{end}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := TemplateRefFromPath(path)
	if !ref.Valid() || ref.Name != "settings.tmpl" {
		t.Fatalf("TemplateRefFromPath = %+v", ref)
	}
	if got := renderAdminLayout(t, h, ref); got != "[legacy settings]" {
		t.Fatalf("rendered %q, want the file-path template", got)
	}
	if TemplateRefFromPath("").Valid() {
		t.Fatal("an empty path means no template")
	}
}
