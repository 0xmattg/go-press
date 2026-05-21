package content

import "sync"

// RewriteRule defines the public URL shape for a content type.
//
// A content type named "product" with Slug "products" resolves archive URLs at
// /products and single content URLs at /products/{content-slug}. WithFront is
// reserved for permalink structures that add a global front base.
type RewriteRule struct {
	Slug      string // URL prefix, e.g. "products"
	WithFront bool   // prepend global permalink front base
}

// MetaFieldDef describes an admin-managed custom field for a content type.
//
// Core stores values in content_meta as strings. Type controls rendering and
// form parsing in admin screens; repositories intentionally keep the storage
// format simple so themes and plugins can introduce fields without migrations.
type MetaFieldDef struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"` // "string", "int", "bool", "text", "select", "image"
	Default  string   `json:"default"`
	Options  []string `json:"options,omitempty"` // for Type="select"
	Required bool     `json:"required"`
}

// ContentTypeDef declares a content model known to GoPress at runtime.
//
// Core and themes register these definitions into Registry. The same definition
// drives admin navigation, CRUD forms, REST API exposure, rewrite URLs, archive
// rendering, sitemap entries, and taxonomy pickers.
type ContentTypeDef struct {
	Name            string         `json:"name"`
	Label           string         `json:"label"`
	LabelPlural     string         `json:"label_plural"`
	ArchiveTitleKey string         `json:"archive_title_key"`
	Supports        []string       `json:"supports"` // "title","content","excerpt","thumbnail","meta"
	MetaFields      []MetaFieldDef `json:"meta_fields"`
	Taxonomies      []string       `json:"taxonomies"`
	HasArchive      bool           `json:"has_archive"`
	Rewrite         RewriteRule    `json:"rewrite"`
	Templates       TemplateDef    `json:"templates"`
	MenuIcon        string         `json:"menu_icon"` // optional: built-in icon key or raw SVG
	MenuOrder       int            `json:"menu_order"`
}

// TemplateDef optionally maps a content type to theme page template names.
//
// Most themes can rely on the default hierarchy derived from the content type
// name and rewrite slug. Themes that reuse a visual template for a differently
// named content type can set Archive and Single from theme.toml instead of
// requiring core to guess by menu order or presentation labels.
type TemplateDef struct {
	Archive string `json:"archive"`
	Single  string `json:"single"`
}

// TaxonomyDef declares a taxonomy that can be attached to one or more content types.
//
// Hierarchical taxonomies behave like categories; non-hierarchical taxonomies
// behave like tags. The distinction affects admin UI and how callers should
// present term selection, not the underlying relationship table.
type TaxonomyDef struct {
	Name         string   `json:"name"`
	Label        string   `json:"label"`
	LabelPlural  string   `json:"label_plural"`
	ContentTypes []string `json:"content_types"` // which content types use this taxonomy
	Hierarchical bool     `json:"hierarchical"`  // true = category-like, false = tag-like
	MenuIcon     string   `json:"menu_icon"`     // optional: built-in icon key or raw SVG
}

// Registry is the in-memory catalog of active content types and taxonomies.
//
// The registry is rebuilt during theme activation so it must only contain
// definitions for the currently active runtime: core types plus the active
// theme's theme.toml declarations. It is safe for concurrent reads while the
// site is serving requests.
type Registry struct {
	mu         sync.RWMutex
	types      map[string]*ContentTypeDef
	taxonomies map[string]*TaxonomyDef
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		types:      make(map[string]*ContentTypeDef),
		taxonomies: make(map[string]*TaxonomyDef),
	}
}

// Clear removes all registered content types and taxonomies.
//
// Engine calls Clear during theme switching before re-registering core and
// active-theme definitions. Plugins should generally extend existing registry
// entries instead of clearing the registry themselves.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types = make(map[string]*ContentTypeDef)
	r.taxonomies = make(map[string]*TaxonomyDef)
}

// RegisterType registers or replaces a content type definition by name.
//
// Names are stable machine identifiers such as "post", "product", or
// "showcase". They are used in database rows, API paths, rewrite resolution,
// and admin route generation.
func (r *Registry) RegisterType(def ContentTypeDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types[def.Name] = &def
}

// RegisterTaxonomy registers or replaces a taxonomy definition by name.
func (r *Registry) RegisterTaxonomy(def TaxonomyDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.taxonomies[def.Name] = &def
}

// GetType returns the content type definition by name.
func (r *Registry) GetType(name string) *ContentTypeDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.types[name]
}

// GetTaxonomy returns the taxonomy definition by name.
func (r *Registry) GetTaxonomy(name string) *TaxonomyDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.taxonomies[name]
}

// AllTypes returns a snapshot of all registered content type definitions.
//
// The returned slice is newly allocated, but the definitions themselves are the
// registry-owned pointers. Treat them as read-only outside registry methods.
func (r *Registry) AllTypes() []*ContentTypeDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*ContentTypeDef, 0, len(r.types))
	for _, t := range r.types {
		result = append(result, t)
	}
	return result
}

// AllTaxonomies returns a snapshot of all registered taxonomy definitions.
//
// The returned slice is newly allocated, but the definitions themselves are the
// registry-owned pointers. Treat them as read-only outside registry methods.
func (r *Registry) AllTaxonomies() []*TaxonomyDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*TaxonomyDef, 0, len(r.taxonomies))
	for _, t := range r.taxonomies {
		result = append(result, t)
	}
	return result
}

// AddContentTypeToTaxonomy appends one or more content types to an existing
// taxonomy without overwriting the full TaxonomyDef. This is the preferred
// way for themes to extend core taxonomies (category, tag) with their
// theme-specific content types.
func (r *Registry) AddContentTypeToTaxonomy(taxName string, types ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tax, ok := r.taxonomies[taxName]
	if !ok {
		return
	}
	existing := make(map[string]bool, len(tax.ContentTypes))
	for _, ct := range tax.ContentTypes {
		existing[ct] = true
	}
	for _, ct := range types {
		if !existing[ct] {
			tax.ContentTypes = append(tax.ContentTypes, ct)
			existing[ct] = true
		}
	}
}

// TaxonomiesForType returns all taxonomies currently attached to a content type.
//
// Admin forms and theme archive helpers use this to discover which term pickers
// or term links should be shown for a given content model.
func (r *Registry) TaxonomiesForType(typeName string) []*TaxonomyDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*TaxonomyDef
	for _, tax := range r.taxonomies {
		for _, ct := range tax.ContentTypes {
			if ct == typeName {
				result = append(result, tax)
				break
			}
		}
	}
	return result
}
