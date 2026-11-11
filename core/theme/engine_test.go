// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestTemplateEngineLoadSkipsAdminTemplates(t *testing.T) {
	files := themeFiles("builtin")
	// Like real theme settings pages: one calls an admin-only function (a
	// parse error in the front-end set), another defines a block that would
	// otherwise leak into front-end pages.
	files["templates/admin/theme_settings.tmpl"] = &fstest.MapFile{Data: []byte(`{{define "content"}}{{X "admin.only"}}{{end}}`)}
	files["templates/admin/extra.tmpl"] = &fstest.MapFile{Data: []byte(`{{define "settings_panel"}}admin{{end}}`)}
	files["templates/partials/admin/badge.tmpl"] = &fstest.MapFile{Data: []byte(`badge`)}

	th := &fsTestTheme{}
	th.InitBaseFS(nil, filepath.Join(t.TempDir(), "missing"), files, fsTestThemeTOML, nil)

	engine := NewTemplateEngine(th)
	if err := engine.Load(); err != nil {
		t.Fatalf("TemplateEngine.Load: %v", err)
	}
	if engine.Resolve([]string{"admin/theme_settings.tmpl", "admin/extra.tmpl"}) != nil {
		t.Fatal("admin/ templates must not join the front-end template set")
	}
	if engine.Get().Lookup("settings_panel") != nil {
		t.Fatal("blocks defined under admin/ must not be defined in the front-end set")
	}
	if engine.Resolve([]string{"single.tmpl"}) == nil {
		t.Fatal("front-end templates must still load")
	}
	if engine.Resolve([]string{"partials/admin/badge.tmpl"}) == nil {
		t.Fatal("only the top-level admin/ directory is skipped")
	}
}
