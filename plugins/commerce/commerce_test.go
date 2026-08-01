package commerce

import (
	"testing"

	"go-press/core/content"
)

// TestRegisterProductTypesIdempotent verifies the commerce content types
// register (and re-register safely) into a registry — the basis for surviving
// theme switches via the content.register_types hook.
func TestRegisterProductTypesIdempotent(t *testing.T) {
	reg := content.NewRegistry()
	registerProductTypes(reg)
	registerProductTypes(reg) // must be idempotent

	pt := reg.GetType("product")
	if pt == nil {
		t.Fatal("product content type not registered")
	}
	if pt.Rewrite.Slug != "store" {
		t.Fatalf("product rewrite slug = %q, want store", pt.Rewrite.Slug)
	}
	if !pt.HasArchive {
		t.Fatal("product should have an archive")
	}
	if pt.ArchiveTitleKey != "commerce.catalog.title" {
		t.Fatalf("product archive title key = %q", pt.ArchiveTitleKey)
	}
	if reg.GetTaxonomy("product_cat") == nil || reg.GetTaxonomy("product_tag") == nil {
		t.Fatal("product taxonomies not registered")
	}
}

// TestPluginIdentityAndDefaultInactive verifies the opt-in module identity.
func TestPluginIdentityAndDefaultInactive(t *testing.T) {
	p := New()
	if p.Name() != "commerce" {
		t.Fatalf("Name = %q, want commerce", p.Name())
	}
	if !p.DefaultInactive() {
		t.Fatal("commerce must ship default-inactive")
	}
	if p.Version() == "" {
		t.Fatal("version should be sourced from plugin.toml")
	}
}
