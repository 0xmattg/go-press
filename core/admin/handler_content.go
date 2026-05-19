package admin

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"go-press/core/content"
	"go-press/core/hook"

	"github.com/gin-gonic/gin"
)

// getContentType reads the content type from the gin context (set by middleware).
func (h *Handler) getContentType(c *gin.Context) (*content.ContentTypeDef, string) {
	typeName := c.GetString("content_type")
	typeDef := h.registry.GetType(typeName)
	return typeDef, typeName
}

// listFilterQuery returns the request's query string with flash params stripped,
// so it can be round-tripped through edit forms — letting users return to the
// same filter tab (e.g. ?lang=zh) after saving instead of dropping back to the
// default list.
//
// Returned as template.URL so html/template won't percent-encode "=" / "&" when
// the value is interpolated inside an href query string.
func listFilterQuery(c *gin.Context) template.URL {
	q := c.Request.URL.Query()
	q.Del("success")
	q.Del("error")
	return template.URL(q.Encode())
}

// listURLWithFilter joins the list path with the preserved filter query.
func listURLWithFilter(slug string, filterQuery template.URL) template.URL {
	url := "/admin/" + slug
	if filterQuery != "" {
		url += "?" + string(filterQuery)
	}
	return template.URL(url)
}

// listRedirectURL builds a list-page redirect that preserves the filter and
// appends a success/error flash param. Returns a plain string for c.Redirect.
func listRedirectURL(slug string, filterQuery template.URL, flashKey, flashValue string) string {
	u := "/admin/" + slug
	sep := "?"
	if filterQuery != "" {
		u += "?" + string(filterQuery)
		sep = "&"
	}
	u += sep + flashKey + "=" + url.QueryEscape(flashValue)
	return u
}

func (h *Handler) resolveCurrentAdminUserID(c *gin.Context) uint {
	var userID uint
	if uid, exists := c.Get("admin_user_id"); exists {
		switch v := uid.(type) {
		case uint:
			userID = v
		case uint64:
			userID = uint(v)
		case uint32:
			userID = uint(v)
		case int:
			if v > 0 {
				userID = uint(v)
			}
		case int64:
			if v > 0 {
				userID = uint(v)
			}
		case int32:
			if v > 0 {
				userID = uint(v)
			}
		case string:
			if parsed, err := strconv.ParseUint(v, 10, 32); err == nil {
				userID = uint(parsed)
			}
		}
	}

	if userID > 0 {
		if _, err := h.svc.userRepo.FindByID(userID); err == nil {
			return userID
		}
	}

	username := c.GetString("admin_username")
	if username != "" {
		if u, err := h.svc.userRepo.FindByUsername(username); err == nil {
			return u.ID
		}
	}

	return 0
}

// ==================== Content List ====================

func (h *Handler) ContentList(c *gin.Context) {
	typeDef, typeName := h.getContentType(c)
	if typeDef == nil {
		c.String(http.StatusNotFound, "Unknown content type")
		return
	}

	orderField := "created_at"
	orderDir := "DESC"
	if hasSupport(typeDef.Supports, "sort_order") {
		orderField = "sort_order"
		orderDir = "ASC"
	}

	items, _ := h.svc.ListContentScoped(c, typeName, orderField, orderDir)
	views := h.svc.ToDynamicContentViews(items, typeDef)
	slug := AdminSlug(typeName)

	// Collect taxonomy definitions for table header
	var taxDefs []*content.TaxonomyDef
	for _, taxName := range typeDef.Taxonomies {
		if td := h.registry.GetTaxonomy(taxName); td != nil {
			taxDefs = append(taxDefs, td)
		}
	}

	// Collect filter tabs from any plugin that hooks admin.content_list.tabs
	// (e.g. multilang contributes per-language tabs). Empty when no plugin
	// contributes, and the template then renders nothing.
	var tabs []ContentListTab
	if h.hooks != nil {
		if v := h.hooks.ApplyFilter(HookContentListTabs, tabs, c, typeName); v != nil {
			if arr, ok := v.([]ContentListTab); ok {
				tabs = arr
			}
		}
	}

	lang := h.svc.AdminLanguage()
	h.render(c, "content_list", gin.H{
		"Title":     h.contentTypeLabel(lang, typeName, typeDef.LabelPlural),
		"Active":    slug,
		"Items":     views,
		"TypeDef":   typeDef,
		"TypeName":  typeName,
		"Slug":      slug,
		"TaxDefs":   taxDefs,
		"Tabs":      tabs,
		"BackQuery": listFilterQuery(c),
	})
}

// ==================== Content New ====================

func (h *Handler) ContentNew(c *gin.Context) {
	typeDef, typeName := h.getContentType(c)
	if typeDef == nil {
		c.String(http.StatusNotFound, "Unknown content type")
		return
	}
	if !h.checkPermission(c, typeName, "create") {
		return
	}

	slug := AdminSlug(typeName)
	lang := h.svc.AdminLanguage()
	label := h.contentTypeLabel(lang, typeName, typeDef.Label)
	filterQuery := listFilterQuery(c)
	data := gin.H{
		"Title":     adminT(lang, "content.new", label),
		"Active":    slug,
		"TypeDef":   typeDef,
		"TypeName":  typeName,
		"Slug":      slug,
		"HookItem":  (*content.Content)(nil),
		"BackURL":   listURLWithFilter(slug, filterQuery),
		"BackQuery": filterQuery,
	}

	// Load taxonomy forms for selectors
	h.loadTaxonomyForms(typeDef, nil, data)

	h.render(c, "content_form", data)
}

// ==================== Content Create ====================

func (h *Handler) ContentCreate(c *gin.Context) {
	typeDef, typeName := h.getContentType(c)
	if typeDef == nil {
		c.String(http.StatusNotFound, "Unknown content type")
		return
	}
	if !h.checkPermission(c, typeName, "create") {
		return
	}

	slug := AdminSlug(typeName)
	lang := h.svc.AdminLanguage()
	label := h.contentTypeLabel(lang, typeName, typeDef.Label)

	item := &content.Content{
		Type:   typeName,
		Status: content.StatusPublished,
		Title:  c.PostForm("title"),
		Slug:   c.PostForm("slug"),
	}
	item.AuthorID = h.resolveCurrentAdminUserID(c)

	// Set status from form
	if st := c.PostForm("status"); st != "" {
		item.Status = st
	}

	// Standard fields based on Supports
	if hasSupport(typeDef.Supports, "content") {
		item.Content = c.PostForm("content")
	}
	if hasSupport(typeDef.Supports, "excerpt") {
		item.Excerpt = c.PostForm("excerpt")
	}
	if hasSupport(typeDef.Supports, "thumbnail") {
		item.ImageURL = c.PostForm("image_url")
	}
	if hasSupport(typeDef.Supports, "sort_order") {
		item.SortOrder, _ = strconv.Atoi(c.PostForm("sort_order"))
	}
	if hasSupport(typeDef.Supports, "publish_date") {
		if pubDate := c.PostForm("published_at"); pubDate != "" {
			t, err := time.Parse("2006-01-02T15:04", pubDate)
			if err == nil {
				item.PublishedAt = &t
			}
		} else if item.Status == content.StatusPublished {
			// Auto-set publish time when status is published but no date given
			now := time.Now()
			item.PublishedAt = &now
		}
	}
	ensurePublishedAtForPublished(item)

	filterQuery := listFilterQuery(c)
	if err := h.svc.CreateContent(item); err != nil {
		view := h.svc.ToDynamicContentView(*item, typeDef)
		data := gin.H{
			"Title": adminT(lang, "content.new", label), "Active": slug,
			"TypeDef": typeDef, "TypeName": typeName, "Slug": slug,
			"Item":      view,
			"Error":     adminT(lang, "error.create_failed", err.Error()),
			"HookItem":  item,
			"BackURL":   listURLWithFilter(slug, filterQuery),
			"BackQuery": filterQuery,
		}
		h.loadTaxonomyForms(typeDef, &view, data)
		h.render(c, "content_form", data)
		return
	}

	// Save meta fields
	for _, mf := range typeDef.MetaFields {
		val := c.PostForm(mf.Key)
		if val != "" {
			h.svc.contentRepo.SaveMeta(item.ID, mf.Key, val)
		}
	}

	// Save gallery images (multi-image support)
	if hasSupport(typeDef.Supports, "thumbnail") {
		h.svc.contentRepo.SaveMeta(item.ID, "gallery_images", c.PostForm("gallery_images"))
	}

	// Save taxonomy relationships
	h.saveTaxonomyRelations(c, typeDef, item.ID)

	// Fire admin.content.saved so plugins can persist their own meta fields.
	if h.hooks != nil {
		h.hooks.DoAction(context.Background(), hook.AdminContentSaved, c, item)
	}

	h.invalidatePageCache()
	h.logAction(c, "create", typeName, item.ID, item.Title)
	c.Redirect(http.StatusFound, listRedirectURL(slug, filterQuery, "success", adminT(lang, "notice.created")))
}

func ensurePublishedAtForPublished(item *content.Content) {
	if item == nil || item.Status != content.StatusPublished || item.PublishedAt != nil {
		return
	}
	now := time.Now()
	item.PublishedAt = &now
}

// ==================== Content Edit ====================

func (h *Handler) ContentEdit(c *gin.Context) {
	typeDef, typeName := h.getContentType(c)
	if typeDef == nil {
		c.String(http.StatusNotFound, "Unknown content type")
		return
	}
	if !h.checkPermission(c, typeName, "update") {
		return
	}

	slug := AdminSlug(typeName)
	lang := h.svc.AdminLanguage()
	label := h.contentTypeLabel(lang, typeName, typeDef.Label)
	filterQuery := listFilterQuery(c)
	item, err := h.svc.GetContent(getIDParam(c))
	if err != nil {
		c.Redirect(http.StatusFound, listRedirectURL(slug, filterQuery, "error", adminT(lang, "error.not_found")))
		return
	}

	view := h.svc.ToDynamicContentView(*item, typeDef)

	// Resolve a permalink prefix via plugin filter (e.g. multilang prepends
	// "/zh" for Chinese content). Empty when no plugin contributes — keeps
	// single-language behavior unchanged.
	permalinkPrefix := ""
	if h.hooks != nil {
		if v := h.hooks.ApplyFilter(HookContentPermalinkPrefix, "", c, item); v != nil {
			if s, ok := v.(string); ok {
				permalinkPrefix = s
			}
		}
	}

	data := gin.H{
		"Title":           adminT(lang, "content.edit", label),
		"Active":          slug,
		"TypeDef":         typeDef,
		"TypeName":        typeName,
		"Slug":            slug,
		"Item":            view,
		"PermalinkPrefix": permalinkPrefix,
		"HookItem":        item,
		"BackURL":         listURLWithFilter(slug, filterQuery),
		"BackQuery":       filterQuery,
	}

	// Load taxonomy forms with selection state
	h.loadTaxonomyForms(typeDef, &view, data)

	h.render(c, "content_form", data)
}

// ==================== Content Update ====================

func (h *Handler) ContentUpdate(c *gin.Context) {
	typeDef, typeName := h.getContentType(c)
	if typeDef == nil {
		c.String(http.StatusNotFound, "Unknown content type")
		return
	}
	if !h.checkPermission(c, typeName, "update") {
		return
	}

	slug := AdminSlug(typeName)
	lang := h.svc.AdminLanguage()
	label := h.contentTypeLabel(lang, typeName, typeDef.Label)
	filterQuery := listFilterQuery(c)
	item, err := h.svc.GetContent(getIDParam(c))
	if err != nil {
		c.Redirect(http.StatusFound, listRedirectURL(slug, filterQuery, "error", adminT(lang, "error.not_found")))
		return
	}

	item.Title = c.PostForm("title")
	item.Slug = c.PostForm("slug")

	// Set status from form
	if st := c.PostForm("status"); st != "" {
		item.Status = st
	}

	if hasSupport(typeDef.Supports, "content") {
		item.Content = c.PostForm("content")
	}
	if hasSupport(typeDef.Supports, "excerpt") {
		item.Excerpt = c.PostForm("excerpt")
	}
	if hasSupport(typeDef.Supports, "thumbnail") {
		item.ImageURL = c.PostForm("image_url")
	}
	if hasSupport(typeDef.Supports, "sort_order") {
		item.SortOrder, _ = strconv.Atoi(c.PostForm("sort_order"))
	}
	if hasSupport(typeDef.Supports, "publish_date") {
		if pubDate := c.PostForm("published_at"); pubDate != "" {
			t, err := time.Parse("2006-01-02T15:04", pubDate)
			if err == nil {
				item.PublishedAt = &t
			}
		} else if item.Status == content.StatusPublished {
			// Auto-set publish time when status is published but no date given
			if item.PublishedAt == nil {
				now := time.Now()
				item.PublishedAt = &now
			}
		} else {
			item.PublishedAt = nil
		}
	}
	ensurePublishedAtForPublished(item)

	if err := h.svc.UpdateContent(item); err != nil {
		view := h.svc.ToDynamicContentView(*item, typeDef)
		data := gin.H{
			"Title": adminT(lang, "content.edit", label), "Active": slug,
			"TypeDef": typeDef, "TypeName": typeName, "Slug": slug,
			"Item":      view,
			"Error":     adminT(lang, "error.update_failed", err.Error()),
			"HookItem":  item,
			"BackURL":   listURLWithFilter(slug, filterQuery),
			"BackQuery": filterQuery,
		}
		h.loadTaxonomyForms(typeDef, &view, data)
		h.render(c, "content_form", data)
		return
	}

	// Update meta fields
	for _, mf := range typeDef.MetaFields {
		h.svc.contentRepo.SaveMeta(item.ID, mf.Key, c.PostForm(mf.Key))
	}

	// Save gallery images (multi-image support)
	if hasSupport(typeDef.Supports, "thumbnail") {
		h.svc.contentRepo.SaveMeta(item.ID, "gallery_images", c.PostForm("gallery_images"))
	}

	// Update taxonomy relationships
	h.saveTaxonomyRelations(c, typeDef, item.ID)

	// Fire admin.content.saved so plugins can persist their own meta fields
	// (e.g. seo-extras stores seo_title / seo_description / seo_image / seo_robots).
	if h.hooks != nil {
		h.hooks.DoAction(context.Background(), hook.AdminContentSaved, c, item)
	}

	h.invalidatePageCache()
	h.logAction(c, "update", typeName, item.ID, item.Title)
	c.Redirect(http.StatusFound, listRedirectURL(slug, filterQuery, "success", adminT(lang, "notice.updated")))
}

// ==================== Content Detail (read-only, e.g. messages) ====================

func (h *Handler) ContentDetail(c *gin.Context) {
	typeDef, typeName := h.getContentType(c)
	if typeDef == nil {
		c.String(http.StatusNotFound, "Unknown content type")
		return
	}
	if !h.checkPermission(c, typeName, "read") {
		return
	}

	slug := AdminSlug(typeName)
	filterQuery := listFilterQuery(c)
	lang := h.svc.AdminLanguage()
	label := h.contentTypeLabel(lang, typeName, typeDef.Label)
	item, err := h.svc.GetContent(getIDParam(c))
	if err != nil {
		c.Redirect(http.StatusFound, listRedirectURL(slug, filterQuery, "error", adminT(lang, "error.not_found")))
		return
	}

	// Mark as read (change status from draft to published)
	if item.Status != content.StatusPublished {
		item.Status = content.StatusPublished
		_ = h.svc.UpdateContent(item)
	}

	view := h.svc.ToDynamicContentView(*item, typeDef)

	h.render(c, "content_detail", gin.H{
		"Title":     adminT(lang, "content.view", label),
		"Active":    slug,
		"TypeDef":   typeDef,
		"TypeName":  typeName,
		"Slug":      slug,
		"Item":      view,
		"BackURL":   listURLWithFilter(slug, filterQuery),
		"BackQuery": filterQuery,
	})
}

// ==================== Content Delete ====================

func (h *Handler) ContentDelete(c *gin.Context) {
	typeDef, typeName := h.getContentType(c)
	if typeDef == nil {
		c.String(http.StatusNotFound, "Unknown content type")
		return
	}
	if !h.checkPermission(c, typeName, "delete") {
		return
	}

	slug := AdminSlug(typeName)
	id := getIDParam(c)
	_ = h.svc.DeleteContent(id)
	h.invalidatePageCache()
	h.logAction(c, "delete", typeName, id, "")
	c.Redirect(http.StatusFound, listRedirectURL(slug, listFilterQuery(c), "success", adminT(h.svc.AdminLanguage(), "notice.deleted")))
}

// ==================== Content Reorder (drag & drop) ====================

// ContentReorder accepts a JSON body {"ids":[...]} with the desired final
// row order and rewrites sort_order to 1..N in a single transaction. Only
// types that declare Supports=["sort_order",...] get this route registered.
func (h *Handler) ContentReorder(c *gin.Context) {
	typeDef, typeName := h.getContentType(c)
	if typeDef == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Unknown content type"})
		return
	}
	if !hasSupport(typeDef.Supports, "sort_order") {
		c.JSON(http.StatusBadRequest, gin.H{"error": adminT(h.svc.AdminLanguage(), "error.reorder_unsupported")})
		return
	}
	if !h.checkPermission(c, typeName, "update") {
		return
	}

	var req struct {
		IDs []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": adminT(h.svc.AdminLanguage(), "error.invalid_request_body")})
		return
	}

	if err := h.svc.ReorderContent(typeName, req.IDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.invalidatePageCache()
	h.logAction(c, "reorder", typeName, 0, fmt.Sprintf("reordered %d items", len(req.IDs)))
	c.JSON(http.StatusOK, gin.H{"ok": true, "count": len(req.IDs)})
}

// ==================== Taxonomy Helpers ====================

// loadTaxonomyForms loads all taxonomy data for form selectors.
func (h *Handler) loadTaxonomyForms(typeDef *content.ContentTypeDef, view *DynamicContentView, data gin.H) {
	var forms []TaxonomyFormData
	for _, taxName := range typeDef.Taxonomies {
		taxDef := h.registry.GetTaxonomy(taxName)
		if taxDef == nil {
			continue
		}
		items, _ := h.svc.ListTaxonomy(taxName)
		fd := TaxonomyFormData{
			TaxDef:      taxDef,
			AllItems:    h.svc.ToTaxonomyItemViews(items),
			SelectedMap: make(map[uint]bool),
		}
		// Populate selections if editing
		if view != nil {
			if taxDef.Hierarchical {
				items := view.Taxonomies[taxName]
				if len(items) > 0 {
					fd.SelectedID = items[0].ID
				}
			} else {
				for _, item := range view.Taxonomies[taxName] {
					fd.SelectedMap[item.ID] = true
				}
			}
		}
		forms = append(forms, fd)
	}
	data["TaxForms"] = forms
}

// saveTaxonomyRelations saves taxonomy relations from form submission.
func (h *Handler) saveTaxonomyRelations(c *gin.Context, typeDef *content.ContentTypeDef, contentID uint) {
	var allTaxIDs []uint
	for _, taxName := range typeDef.Taxonomies {
		taxDef := h.registry.GetTaxonomy(taxName)
		if taxDef == nil {
			continue
		}
		if taxDef.Hierarchical {
			// Category-like: single select
			catID, _ := strconv.ParseUint(c.PostForm(taxName+"_id"), 10, 32)
			if catID > 0 {
				allTaxIDs = append(allTaxIDs, uint(catID))
			}
		} else {
			// Tag-like: multi select
			tagIDs := parseUintSlice(c.PostFormArray(taxName + "_ids"))
			allTaxIDs = append(allTaxIDs, tagIDs...)
		}
	}
	if len(allTaxIDs) > 0 {
		_ = h.svc.taxRepo.SetContentTaxonomies(contentID, allTaxIDs)
	} else if len(typeDef.Taxonomies) > 0 {
		_ = h.svc.taxRepo.SetContentTaxonomies(contentID, []uint{})
	}
}
