// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"fmt"
	"strings"
	"testing"
)

func TestExternalConfigParsesFromThemeTOML(t *testing.T) {
	cfg, err := ParseFileConfig(`
[theme]
slug = "aurora"
name = "Aurora"

[[external]]
host = "fonts.googleapis.com"
kinds = ["style"]
purpose = "Web fonts"
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Slug != "aurora" || len(cfg.External) != 1 || cfg.External[0].Kinds[0] != ExternalStyle {
		t.Fatalf("config = %+v", cfg)
	}
	if errs := cfg.ValidateExternal(); errs != nil {
		t.Fatal(errs)
	}
}

func TestExternalHostMatching(t *testing.T) {
	exact := ExternalConfig{Host: "fonts.gstatic.com", Kinds: []string{ExternalFont}}
	wild := ExternalConfig{Host: "*.example.org", Kinds: []string{ExternalImage}}
	cases := []struct {
		entry ExternalConfig
		host  string
		want  bool
	}{
		{exact, "fonts.gstatic.com", true},
		{exact, "FONTS.gstatic.com.", true},
		{exact, "x.fonts.gstatic.com", false},
		{exact, "gstatic.com", false},
		{wild, "img.example.org", true},
		{wild, "a.b.example.org", true},
		{wild, "example.org", false},
		{wild, "badexample.org", false},
		{wild, "", false},
	}
	for _, tc := range cases {
		if got := tc.entry.Matches(tc.host); got != tc.want {
			t.Errorf("%s matches %q = %v", tc.entry.Host, tc.host, got)
		}
	}
	if !exact.Allows(ExternalFont) || exact.Allows(ExternalImage) {
		t.Fatal("Allows is wrong")
	}
	cfg := &FileConfig{External: []ExternalConfig{exact, wild}}
	if got := cfg.ExternalHosts("img.example.org"); len(got) != 1 || got[0].Host != wild.Host {
		t.Fatalf("ExternalHosts = %+v", got)
	}
	if (*FileConfig)(nil).ExternalHosts("x.org") != nil || (*FileConfig)(nil).ValidateExternal() != nil {
		t.Fatal("a nil config declares hosts")
	}
}

func TestValidateExternal(t *testing.T) {
	valid := ExternalConfig{Host: "media.example.com", Kinds: []string{ExternalImage, ExternalMedia}, Purpose: "Product photos"}
	cases := []struct {
		desc  string
		entry ExternalConfig
		want  string
	}{
		{"missing host", ExternalConfig{Kinds: []string{ExternalImage}, Purpose: "x"}, "host is required"},
		{"scheme", ExternalConfig{Host: "https://cdn.example.com", Kinds: []string{ExternalImage}, Purpose: "x"}, "domain name"},
		{"port", ExternalConfig{Host: "cdn.example.com:443", Kinds: []string{ExternalImage}, Purpose: "x"}, "domain name"},
		{"bare star", ExternalConfig{Host: "*", Kinds: []string{ExternalImage}, Purpose: "x"}, "domain name"},
		{"star on a top-level domain", ExternalConfig{Host: "*.com", Kinds: []string{ExternalImage}, Purpose: "x"}, "two labels"},
		{"inner star", ExternalConfig{Host: "a.*.example.com", Kinds: []string{ExternalImage}, Purpose: "x"}, "domain name"},
		{"single label", ExternalConfig{Host: "localhost", Kinds: []string{ExternalImage}, Purpose: "x"}, "two labels"},
		{"upper case", ExternalConfig{Host: "CDN.example.com", Kinds: []string{ExternalImage}, Purpose: "x"}, "lowercase"},
		{"IP address", ExternalConfig{Host: "192.168.1.10", Kinds: []string{ExternalImage}, Purpose: "x"}, "IP address"},
		{"bad label", ExternalConfig{Host: "-cdn.example.com", Kinds: []string{ExternalImage}, Purpose: "x"}, "lowercase"},
		{"no kinds", ExternalConfig{Host: "cdn.example.com", Purpose: "x"}, "kinds is empty"},
		{"script kind", ExternalConfig{Host: "cdn.example.com", Kinds: []string{"script"}, Purpose: "x"}, "may not load scripts"},
		{"unknown kind", ExternalConfig{Host: "cdn.example.com", Kinds: []string{"video"}, Purpose: "x"}, "unknown kind"},
		{"repeated kind", ExternalConfig{Host: "cdn.example.com", Kinds: []string{ExternalImage, ExternalImage}, Purpose: "x"}, "listed twice"},
		{"no purpose", ExternalConfig{Host: "cdn.example.com", Kinds: []string{ExternalImage}, Purpose: "  "}, "purpose is required"},
		{"long purpose", ExternalConfig{Host: "cdn.example.com", Kinds: []string{ExternalImage}, Purpose: strings.Repeat("x", maxExternalPurpose+1)}, "longer than"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			errs := (&FileConfig{External: []ExternalConfig{valid, tc.entry}}).ValidateExternal()
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), tc.want) || !strings.Contains(errs[0].Error(), "#2") {
				t.Fatalf("errors = %v, want one containing %q", errs, tc.want)
			}
		})
	}

	dup := (&FileConfig{External: []ExternalConfig{valid, valid}}).ValidateExternal()
	if len(dup) != 1 || !strings.Contains(dup[0].Error(), "declared twice") {
		t.Fatalf("duplicate host errors = %v", dup)
	}
	var many []ExternalConfig
	for i := 0; i <= MaxExternalHosts; i++ {
		many = append(many, ExternalConfig{Host: fmt.Sprintf("h%d.example.com", i), Kinds: []string{ExternalImage}, Purpose: "x"})
	}
	if errs := (&FileConfig{External: many}).ValidateExternal(); len(errs) != 1 || !strings.Contains(errs[0].Error(), "at most") {
		t.Fatalf("too many hosts errors = %v", errs)
	}
	wild := ExternalConfig{Host: "*.example.org", Kinds: ExternalKinds, Purpose: "Everything"}
	if errs := (&FileConfig{External: []ExternalConfig{wild}}).ValidateExternal(); errs != nil {
		t.Fatalf("wildcard errors = %v", errs)
	}
}
