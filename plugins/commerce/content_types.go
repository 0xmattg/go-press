package commerce

import "github.com/0xmattg/go-press/core/content"

// registerProductTypes idempotently registers the commerce content types and
// taxonomies into the given registry. It runs once immediately on Activate and
// again on every content.register_types fire (i.e. every theme activation), so
// the product type survives theme switches, which rebuild the registry.
//
// NOTE: a theme must not also declare its own "product" content type while
// commerce is active — commerce owns it. Shop themes rely on this registration.
func registerProductTypes(reg *content.Registry) {
	if reg == nil {
		return
	}

	reg.RegisterTaxonomy(content.TaxonomyDef{
		Name:         "product_cat",
		Label:        "Product Category",
		LabelPlural:  "Product Categories",
		ContentTypes: []string{"product"},
		Hierarchical: true,
	})
	reg.RegisterTaxonomy(content.TaxonomyDef{
		Name:         "product_tag",
		Label:        "Product Tag",
		LabelPlural:  "Product Tags",
		ContentTypes: []string{"product"},
		Hierarchical: false,
	})

	reg.RegisterType(content.ContentTypeDef{
		Name:            "product",
		Label:           "Product",
		LabelPlural:     "Products",
		ArchiveTitleKey: "commerce.catalog.title",
		HasArchive:      true,
		Supports:        []string{"title", "content", "excerpt", "thumbnail"},
		Taxonomies:      []string{"product_cat", "product_tag"},
		Rewrite:         content.RewriteRule{Slug: "store"},
		MenuIcon:        "blocks",
		MenuOrder:       15,
	})
}
