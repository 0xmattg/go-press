package admin

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/taxonomy"
	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
)

func TestTaxonomyListRequiresReadPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := content.NewRegistry()
	registry.RegisterTaxonomy(content.TaxonomyDef{
		Name:        "product_tag",
		Label:       "Product tag",
		LabelPlural: "Product tags",
	})
	h := &Handler{
		svc:      &Service{rbac: user.NewRBAC()},
		registry: registry,
	}

	router := gin.New()
	router.GET("/admin/product-tags", func(c *gin.Context) {
		c.Set("taxonomy_type", "product_tag")
		c.Set("admin_role", user.RoleSubscriber)
		h.TaxonomyList(c)
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/product-tags", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("permissionless taxonomy list status = %d, want %d", rec.Code, http.StatusFound)
	}
}

func TestTaxonomyMatchesTypePreventsCrossTaxonomyIDOR(t *testing.T) {
	item := &taxonomy.Taxonomy{ID: 42, Taxonomy: "tag"}
	if taxonomyMatchesType(item, "category") {
		t.Fatal("category route accepted a tag taxonomy ID")
	}
	if !taxonomyMatchesType(item, "tag") {
		t.Fatal("matching taxonomy type was rejected")
	}
	if taxonomyMatchesType(nil, "tag") || taxonomyMatchesType(item, "") {
		t.Fatal("empty taxonomy scope was accepted")
	}
}

func TestApplyTaxonomyReferenceCountsSortsPopularTermsFirst(t *testing.T) {
	views := []TaxonomyItemView{
		{ID: 1, Name: "First"},
		{ID: 2, Name: "Popular"},
		{ID: 3, Name: "Also popular"},
	}
	applyTaxonomyReferenceCounts(views, map[uint]int64{1: 1, 2: 8, 3: 8})

	if views[0].ID != 2 || views[1].ID != 3 || views[2].ID != 1 {
		t.Fatalf("unexpected popularity order: %#v", views)
	}
	if views[0].ReferenceCount != 8 || views[2].ReferenceCount != 1 {
		t.Fatalf("reference counts not attached: %#v", views)
	}
}

func TestTaxonomyListTemplateRendersReferenceCount(t *testing.T) {
	registry := content.NewRegistry()
	def := content.TaxonomyDef{Name: "tag", Label: "Tag", LabelPlural: "Tags"}
	registry.RegisterTaxonomy(def)
	h := &Handler{svc: &Service{rbac: user.NewRBAC()}, registry: registry}
	h.buildFuncMap()
	tmpl, err := template.New("taxonomy_list_test").Funcs(h.funcMap).
		ParseFiles(filepath.Join("templates", "pages", "taxonomy_list.tmpl"))
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = tmpl.ExecuteTemplate(&out, "content", gin.H{
		"AdminLanguage": "zh-CN",
		"CurrentRole":   user.RoleSuperAdmin,
		"Items":         []TaxonomyItemView{{ID: 7, Name: "热门", Slug: "popular", ReferenceCount: 12}},
		"TaxDef":        &def,
		"TaxType":       "tag",
		"Slug":          "tags",
	})
	if err != nil {
		t.Fatal(err)
	}
	html := out.String()
	if !strings.Contains(html, "引用次数") || !strings.Contains(html, "taxonomy-reference-count\">12</span>") {
		t.Fatalf("reference-count column missing from rendered taxonomy list: %s", html)
	}
}

func TestContentFormRendersSearchableTagPicker(t *testing.T) {
	registry := content.NewRegistry()
	categoryDef := content.TaxonomyDef{
		Name:         "category",
		Label:        "Category",
		LabelPlural:  "Categories",
		Hierarchical: true,
	}
	tagDef := content.TaxonomyDef{
		Name:        "tag",
		Label:       "Tag",
		LabelPlural: "Tags",
	}
	registry.RegisterTaxonomy(categoryDef)
	registry.RegisterTaxonomy(tagDef)

	h := &Handler{svc: &Service{rbac: user.NewRBAC()}, registry: registry}
	h.buildFuncMap()
	tmpl, err := template.New("content_form_test").Funcs(h.funcMap).
		ParseFiles(filepath.Join("templates", "pages", "content_form.tmpl"))
	if err != nil {
		t.Fatal(err)
	}

	typeDef := &content.ContentTypeDef{
		Name:        "post",
		Label:       "Post",
		LabelPlural: "Posts",
		Taxonomies:  []string{"category", "tag"},
		Rewrite:     content.RewriteRule{Slug: "blog"},
	}
	var out bytes.Buffer
	err = tmpl.ExecuteTemplate(&out, "content", gin.H{
		"AdminLanguage": "zh-CN",
		"CurrentRole":   user.RoleSuperAdmin,
		"BackURL":       "/admin/posts",
		"Title":         "编辑文章",
		"Slug":          "posts",
		"TypeName":      "post",
		"TypeDef":       typeDef,
		"CanPublish":    true,
		"TaxForms": []TaxonomyFormData{
			{
				TaxDef:     &categoryDef,
				AllItems:   []TaxonomyItemView{{ID: 3, Name: "新闻", Slug: "news"}},
				SelectedID: 3,
			},
			{
				TaxDef: &tagDef,
				AllItems: []TaxonomyItemView{
					{ID: 7, Name: "热门", Slug: "popular", ReferenceCount: 12},
					{ID: 8, Name: "普通", Slug: "normal", ReferenceCount: 1},
				},
				SelectedMap: map[uint]bool{7: true},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	html := out.String()
	for _, want := range []string{
		`data-content-edit-layout`,
		`data-content-edit-resizer`,
		`role="separator"`,
		`aria-valuenow="390"`,
		`data-content-edit-sidebar`,
		`data-taxonomy-picker`,
		`data-taxonomy-search`,
		`data-taxonomy-candidate`,
		`data-reference-count="12"`,
		`name="tag_ids" value="7"`,
		`name="category_id"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("content form is missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, `type="checkbox" name="tag_ids"`) {
		t.Fatalf("tag candidates unexpectedly rendered as checkboxes: %s", html)
	}
}
