package admin

import (
	"net/http"
	"net/url"

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
	items, err := h.svc.ListTaxonomyItemViews(taxType)
	if err != nil {
		c.String(http.StatusServiceUnavailable, adminT(lang, "status.load_failed"))
		return
	}

	h.render(c, "taxonomy_list", gin.H{
		"Title":   h.taxonomyLabel(lang, taxType, taxDef.LabelPlural),
		"Active":  slug,
		"Items":   items,
		"TaxDef":  taxDef,
		"TaxType": taxType,
		"Slug":    slug,
	})
}

func (h *Handler) TaxonomyCreate(c *gin.Context) {
	taxType, _ := h.getTaxonomyType(c)
	slug := AdminSlug(taxType)
	if !h.checkPermission(c, taxType, "create") {
		return
	}
	if err := h.svc.CreateTaxonomyTerm(c.PostForm("name"), c.PostForm("slug"), taxType); err != nil {
		c.Redirect(http.StatusFound, "/admin/"+slug+"?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.create_failed", err.Error())))
		return
	}
	h.invalidatePageCache()
	h.logAction(c, "create", taxType, 0, c.PostForm("name"))
	c.Redirect(http.StatusFound, "/admin/"+slug+"?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.created")))
}

func (h *Handler) TaxonomyUpdate(c *gin.Context) {
	taxType, _ := h.getTaxonomyType(c)
	slug := AdminSlug(taxType)
	if !h.checkPermission(c, taxType, "update") {
		return
	}
	if err := h.svc.UpdateTaxonomyTerm(getIDParam(c), taxType, c.PostForm("name"), c.PostForm("slug")); err != nil {
		c.Redirect(http.StatusFound, "/admin/"+slug+"?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.update_failed", err.Error())))
		return
	}
	h.invalidatePageCache()
	h.logAction(c, "update", taxType, getIDParam(c), c.PostForm("name"))
	c.Redirect(http.StatusFound, "/admin/"+slug+"?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.updated")))
}

func (h *Handler) TaxonomyDelete(c *gin.Context) {
	taxType, _ := h.getTaxonomyType(c)
	slug := AdminSlug(taxType)
	if !h.checkPermission(c, taxType, "delete") {
		return
	}
	id := getIDParam(c)
	if err := h.svc.DeleteTaxonomyTerm(id, taxType); err != nil {
		c.Redirect(http.StatusFound, "/admin/"+slug+"?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "error.not_found")))
		return
	}
	h.invalidatePageCache()
	h.logAction(c, "delete", taxType, id, "")
	c.Redirect(http.StatusFound, "/admin/"+slug+"?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "notice.deleted")))
}
