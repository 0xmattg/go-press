package admin

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/0xmattg/go-press/core/taxonomy"
	"github.com/gin-gonic/gin"
)

// ==================== Generic Taxonomy Handlers ====================

// getTaxonomyType reads the taxonomy type from the gin context (set by middleware).
func (h *Handler) getTaxonomyType(c *gin.Context) (string, string) {
	taxType := c.GetString("taxonomy_type")
	taxDef := h.registry.GetTaxonomy(taxType)
	if taxDef == nil {
		return taxType, ""
	}
	return taxType, taxDef.Label
}

func (h *Handler) TaxonomyList(c *gin.Context) {
	taxType, _ := h.getTaxonomyType(c)
	taxDef := h.registry.GetTaxonomy(taxType)
	if taxDef == nil {
		c.String(http.StatusNotFound, "Unknown taxonomy type")
		return
	}
	if !h.checkPermission(c, taxType, "read") {
		return
	}

	lang := h.svc.AdminLanguage()
	slug := AdminSlug(taxType)
	items, err := h.svc.ListTaxonomyItemViewsContext(taxonomy.RequestContext(c), taxType)
	if err != nil {
		c.String(http.StatusServiceUnavailable, adminT(lang, "status.load_failed"))
		return
	}

	var tabs []ContentListTab
	if h.hooks != nil {
		if value := h.hooks.ApplyFilter(HookTaxonomyListTabs, tabs, c, taxType); value != nil {
			tabs, _ = value.([]ContentListTab)
		}
	}
	currentQuery := url.Values{}
	if selectedLang := c.Query("lang"); selectedLang != "" {
		currentQuery.Set("lang", selectedLang)
	}

	h.render(c, "taxonomy_list", gin.H{
		"Title":        h.taxonomyLabel(lang, taxType, taxDef.LabelPlural),
		"Active":       slug,
		"Items":        items,
		"TaxDef":       taxDef,
		"TaxType":      taxType,
		"Slug":         slug,
		"Tabs":         tabs,
		"CurrentQuery": currentQuery.Encode(),
	})
}

func (h *Handler) TaxonomyCreate(c *gin.Context) {
	taxType, _ := h.getTaxonomyType(c)
	slug := AdminSlug(taxType)
	if !h.checkPermission(c, taxType, "create") {
		return
	}
	if err := h.svc.CreateTaxonomyContext(taxonomy.RequestContext(c), taxonomyItemFromForm(c, 0, taxType)); err != nil {
		c.Redirect(http.StatusFound, taxonomyRedirect(c, slug, "error", adminT(h.svc.AdminLanguage(), "error.create_failed", err.Error())))
		return
	}
	h.invalidatePageCache()
	h.logAction(c, "create", taxType, 0, c.PostForm("name"))
	c.Redirect(http.StatusFound, taxonomyRedirect(c, slug, "success", adminT(h.svc.AdminLanguage(), "notice.created")))
}

func (h *Handler) TaxonomyUpdate(c *gin.Context) {
	taxType, _ := h.getTaxonomyType(c)
	slug := AdminSlug(taxType)
	if !h.checkPermission(c, taxType, "update") {
		return
	}
	if err := h.svc.UpdateTaxonomyContext(taxonomy.RequestContext(c), taxType, taxonomyItemFromForm(c, getIDParam(c), taxType)); err != nil {
		c.Redirect(http.StatusFound, taxonomyRedirect(c, slug, "error", adminT(h.svc.AdminLanguage(), "error.update_failed", err.Error())))
		return
	}
	h.invalidatePageCache()
	h.logAction(c, "update", taxType, getIDParam(c), c.PostForm("name"))
	c.Redirect(http.StatusFound, taxonomyRedirect(c, slug, "success", adminT(h.svc.AdminLanguage(), "notice.updated")))
}

func taxonomyItemFromForm(c *gin.Context, id uint, taxonomyType string) *taxonomy.Taxonomy {
	item := &taxonomy.Taxonomy{
		ID: id, Taxonomy: taxonomyType, Description: c.PostForm("description"),
		Term: taxonomy.Term{Name: c.PostForm("name"), Slug: c.PostForm("slug")},
	}
	if parent, err := strconv.ParseUint(c.PostForm("parent_id"), 10, 64); err == nil && parent > 0 {
		parentID := uint(parent)
		item.ParentID = &parentID
	}
	return item
}

func (h *Handler) TaxonomyDelete(c *gin.Context) {
	taxType, _ := h.getTaxonomyType(c)
	slug := AdminSlug(taxType)
	if !h.checkPermission(c, taxType, "delete") {
		return
	}
	id := getIDParam(c)
	if err := h.svc.DeleteTaxonomyTermContext(taxonomy.RequestContext(c), id, taxType); err != nil {
		c.Redirect(http.StatusFound, taxonomyRedirect(c, slug, "error", adminT(h.svc.AdminLanguage(), "error.not_found")))
		return
	}
	h.invalidatePageCache()
	h.logAction(c, "delete", taxType, id, "")
	c.Redirect(http.StatusFound, taxonomyRedirect(c, slug, "success", adminT(h.svc.AdminLanguage(), "notice.deleted")))
}

func taxonomyRedirect(c *gin.Context, slug, key, message string) string {
	query := url.Values{}
	if lang := c.Query("lang"); lang != "" {
		query.Set("lang", lang)
	}
	query.Set(key, message)
	return "/admin/" + slug + "?" + query.Encode()
}
