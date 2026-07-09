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
