// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package option

import "testing"

func TestSystemTranslatableOptionsSurviveThemeRegistryClear(t *testing.T) {
	ClearTranslatableOptions()
	RegisterTranslatable("home_title", "home", "Home title")

	if !IsSystemTranslatable("site_name") {
		t.Fatal("site_name should be a system translatable option")
	}
	if !IsTranslatable("site_description") {
		t.Fatal("site_description should be included in general translatable checks")
	}

	ClearTranslatableOptions()

	if !IsSystemTranslatable("site_name") {
		t.Fatal("site_name should still be system translatable after clearing theme options")
	}
	if IsTranslatable("home_title") {
		t.Fatal("theme option should be cleared")
	}
}

func TestStagedThemeTranslatablesReplaceCurrentOnlyOnCommit(t *testing.T) {
	ClearTranslatableOptions()
	t.Cleanup(func() {
		DiscardThemeTranslatables()
		ClearTranslatableOptions()
	})
	RegisterTranslatable("old_title", "home", "Old title")

	StageThemeTranslatables()
	RegisterTranslatable("new_title", "home", "New title")
	RegisterTranslatable("new_title", "home", "Duplicate")
	if !IsTranslatable("old_title") || IsTranslatable("new_title") {
		t.Fatal("while staged, readers must see the current keys only")
	}
	CommitThemeTranslatables()
	if IsTranslatable("old_title") {
		t.Fatal("the previous theme's key must not linger after commit")
	}
	if keys := AllTranslatableOptions(); len(keys) != 1 || keys[0].Key != "new_title" || keys[0].Label != "New title" {
		t.Fatalf("keys after commit = %+v", keys)
	}
	if !IsTranslatable("site_name") {
		t.Fatal("system translatable options are not part of the theme set")
	}

	StageThemeTranslatables()
	RegisterTranslatable("discarded", "home", "Discarded")
	DiscardThemeTranslatables()
	if IsTranslatable("discarded") || !IsTranslatable("new_title") {
		t.Fatal("discard must keep the current keys")
	}
	CommitThemeTranslatables() // nothing staged: no change
	if !IsTranslatable("new_title") {
		t.Fatal("commit without staging changed the keys")
	}
}
