// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package installer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// T10: the installer's theme list comes from the theme registry, includes
// every registered theme, and does not scan the themes/ directory.
func TestDiscoverThemesListsRegisteredThemesOnly(t *testing.T) {
	work := t.TempDir()
	// A theme folder that is not compiled in must not be offered.
	stray := filepath.Join(work, "themes", "installer-test-stray")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stray, "theme.toml"), []byte("[theme]\nname = \"Stray\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(work, "elsewhere", "installer-test-disk")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "theme.toml"), []byte("[theme]\nname = \"Disk Theme\"\ndescription = \"From its directory\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)

	none := func(*core.Engine, string) coreTheme.Theme { return nil }
	core.RegisterThemeWithMeta("installer-test-meta", "[theme]\nname = \"Embedded Theme\"\ndescription = \"From theme.toml in the binary\"\n", none)
	core.RegisterThemeSource("installer-test-disk", core.ThemeSource{Dir: external}, none)
	core.RegisterTheme("installer-test-bare", none)

	var got []ThemeOption
	for _, option := range discoverThemes() {
		if strings.HasPrefix(option.Slug, "installer-test-") {
			got = append(got, option)
		}
	}
	want := []ThemeOption{
		{Slug: "installer-test-bare", Name: "installer-test-bare"},
		{Slug: "installer-test-disk", Name: "Disk Theme", Description: "From its directory"},
		{Slug: "installer-test-meta", Name: "Embedded Theme", Description: "From theme.toml in the binary"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverThemes = %+v\nwant %+v", got, want)
	}
}
