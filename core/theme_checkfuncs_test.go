// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package core

import (
	"reflect"
	"testing"

	"github.com/0xmattg/go-press/core/media"
	"github.com/0xmattg/go-press/core/rewrite"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// The theme package checker parses templates with TemplateCheckFuncs, so it
// must offer exactly the functions a real engine gives a theme.
func TestTemplateCheckFuncsMatchTheEngine(t *testing.T) {
	e := themeSetEngine(t, "", "")
	e.Rewrite = rewrite.NewEngine(e.Registry)
	e.SEO = rewrite.NewSEOBuilder("https://example.test", "Example", e.Rewrite)
	e.Media = media.NewRepository(nil)

	var b coreTheme.BaseTheme
	b.InitBase(e, "", "", nil)
	engineFuncs := b.BaseFuncMap()
	checkFuncs := coreTheme.TemplateCheckFuncs()
	if len(engineFuncs) != len(checkFuncs) {
		t.Errorf("engine has %d functions, TemplateCheckFuncs %d", len(engineFuncs), len(checkFuncs))
	}
	for name, fn := range engineFuncs {
		stub, ok := checkFuncs[name]
		if !ok {
			t.Errorf("TemplateCheckFuncs has no %s", name)
			continue
		}
		if got, want := reflect.TypeOf(stub), reflect.TypeOf(fn); got != want {
			t.Errorf("%s: TemplateCheckFuncs has %v, the engine %v", name, got, want)
		}
	}
}
