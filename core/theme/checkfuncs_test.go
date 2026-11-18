// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"bytes"
	"html/template"
	"testing"
)

func TestTemplateCheckFuncsCoverEngineFunctions(t *testing.T) {
	funcs := TemplateCheckFuncs()
	// Functions BaseFuncMap only adds when an engine service exists.
	for _, name := range []string{
		"seoHead", "seoHeadFor", "faviconLinks", "menuByLocation", "renderHook",
		"responsiveImage", "T", "currentLang", "langPrefixURL", "buildURL",
		"canCurrentUser", "contentList", "contentBySlug", "taxonomyTerms",
		"safeHTML", "embedHTML", "goPressVersion", "seq",
	} {
		if funcs[name] == nil {
			t.Errorf("TemplateCheckFuncs has no %s", name)
		}
	}
	for name := range CommonFuncMap() {
		if funcs[name] == nil {
			t.Errorf("TemplateCheckFuncs has no common function %s", name)
		}
	}

	// The map is a copy.
	delete(funcs, "seq")
	if TemplateCheckFuncs()["seq"] == nil {
		t.Fatal("changing the returned map changed the next one")
	}
}

func TestTemplateCheckFuncsReturnZeroValues(t *testing.T) {
	tpl := template.Must(template.New("x").Funcs(TemplateCheckFuncs()).Parse(
		`[{{range seq 1000000}}x{{end}}][{{safeHTML "<b>x</b>"}}][{{with menuByLocation "main"}}menu{{end}}][{{len (contentList . "post" 5)}}][{{T . "hello"}}]`))
	var out bytes.Buffer
	if err := tpl.Execute(&out, nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[][][][0][]" {
		t.Fatalf("output = %q", out.String())
	}
}
