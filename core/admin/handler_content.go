package admin

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

func cleanListQuery(c *gin.Context, dropPage bool) url.Values {
	q := c.Request.URL.Query()
	q.Del("success")
	q.Del("error")
	q.Del("screen_options")
	q.Del("columns")
	q.Del("per_page")
	if dropPage {
		q.Del("page")
	}
	return q
}

func cleanListQueryString(c *gin.Context, dropPage bool) string {
	return cleanListQuery(c, dropPage).Encode()
}

func redirectToCleanList(c *gin.Context, slug string) {
	q := cleanListQueryString(c, true)
	target := "/admin/" + slug
	if q != "" {
		target += "?" + q
	}
	c.Redirect(http.StatusFound, target)
}

func hiddenInputsFromQuery(q url.Values) []AdminHiddenInput {
	return hiddenInputsFromQueryExcept(q, nil)
}

func hiddenInputsFromQueryExcept(q url.Values, omit map[string]bool) []AdminHiddenInput {
	var inputs []AdminHiddenInput
	for key, values := range q {
		if key == "" {
			continue
		}
		if omit != nil && omit[key] {
			continue
		}
		for _, value := range values {
			inputs = append(inputs, AdminHiddenInput{Name: key, Value: value})
		}
	}
	return inputs
}

func parseAdminPage(c *gin.Context) int {
	page, err := strconv.Atoi(c.Query("page"))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

func (h *Handler) contentListColumns(lang string, typeName string, typeDef *content.ContentTypeDef, taxDefs []*content.TaxonomyDef) []AdminListColumn {
	columns := []AdminListColumn{
		{Key: "id", Label: "ID"},
		{Key: "title", Label: adminT(lang, "content.title")},
		{Key: "status", Label: adminT(lang, "content.status")},
	}
	if hasSupport(typeDef.Supports, "sort_order") {
		columns = append(columns, AdminListColumn{Key: "sort_order", Label: adminT(lang, "content.sort_order")})
	}
	columns = append(columns, AdminListColumn{Key: "author", Label: adminT(lang, "content.author")})
	for _, field := range typeDef.MetaFields {
		columns = append(columns, AdminListColumn{
			Key:   "meta:" + field.Key,
			Label: h.metaFieldLabel(lang, typeName, field.Key, field.Label),
		})
	}
	for _, tax := range taxDefs {
		columns = append(columns, AdminListColumn{
			Key:   "tax:" + tax.Name,
			Label: adminTaxonomyLabel(lang, tax.Name, tax.Label),
		})
	}
	if hasSupport(typeDef.Supports, "publish_date") {
		columns = append(columns, AdminListColumn{Key: "published_at", Label: adminT(lang, "content.publish_at")})
	} else {
		columns = append(columns, AdminListColumn{Key: "created_at", Label: adminT(lang, "content.created_at")})
	}
	columns = append(columns,
		AdminListColumn{Key: "updated_at", Label: adminT(lang, "content.updated_at")},
		AdminListColumn{Key: "actions", Label: adminT(lang, "field.actions")},
	)
	return columns
}

func contentListDateField(typeDef *content.ContentTypeDef) string {
	if hasSupport(typeDef.Supports, "publish_date") {
		return "published_at"
	}
	return "created_at"
}

func contentListFilterTaxonomy(taxDefs []*content.TaxonomyDef) *content.TaxonomyDef {
	if len(taxDefs) == 0 {
		return nil
	}
	for _, tax := range taxDefs {
		if tax.Hierarchical {
			return tax
		}
	}
	return taxDefs[0]
}

func monthOptionLabel(lang, value string) string {
	t, err := time.Parse("2006-01", value)
	if err != nil {
		return value
	}
	if strings.HasPrefix(lang, "zh") {
		return fmt.Sprintf("%d年 %d月", t.Year(), int(t.Month()))
	}
	return t.Format("January 2006")
}

func (h *Handler) contentListFilters(c *gin.Context, slug string, typeName string, dateField string, taxDef *content.TaxonomyDef) AdminContentListFilters {
	lang := h.svc.AdminLanguage()
	selectedDate := strings.TrimSpace(c.Query("date"))
	filters := AdminContentListFilters{
		ActionURL: "/admin/" + slug,
		DateOptions: []AdminListFilterOption{{
			Value:    "",
			Label:    adminT(lang, "screen.all_dates"),
			Selected: selectedDate == "",
		}},
	}
	if months, err := h.svc.ListContentMonthsScoped(c, typeName, dateField); err == nil {
		for _, month := range months {
			filters.DateOptions = append(filters.DateOptions, AdminListFilterOption{
				Value:    month,
				Label:    monthOptionLabel(lang, month),
				Selected: selectedDate == month,
			})
		}
	}

	if taxDef != nil {
		selectedTerm := strings.TrimSpace(c.Query("term"))
		filters.TaxonomyLabel = adminT(lang, "screen.all_categories")
		if taxDef.LabelPlural != "" {
			filters.TaxonomyLabel = adminT(lang, "screen.all_taxonomy", adminTaxonomyLabel(lang, taxDef.Name, taxDef.LabelPlural))
		}
		filters.TaxonomyOptions = append(filters.TaxonomyOptions, AdminListFilterOption{
			Value:    "",
			Label:    filters.TaxonomyLabel,
			Selected: selectedTerm == "",
		})
		if items, err := h.svc.ListTaxonomy(taxDef.Name); err == nil {
			for _, item := range h.svc.ToTaxonomyItemViews(items) {
				filters.TaxonomyOptions = append(filters.TaxonomyOptions, AdminListFilterOption{
					Value:    item.Slug,
					Label:    item.Name,
					Selected: selectedTerm == item.Slug,
				})
			}
		}
	}

	return filters
}

func buildAdminPagination(c *gin.Context, result *content.PaginatedResult) AdminPaginationView {
	totalPages := result.TotalPages
	if totalPages < 1 {
		totalPages = 1
	}
	page := result.Page
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	buildURL := func(targetPage int) string {
		if targetPage < 1 {
			targetPage = 1
		}
		if targetPage > totalPages {
			targetPage = totalPages
		}
		q := cleanListQuery(c, false)
		q.Set("page", strconv.Itoa(targetPage))
		path := c.Request.URL.Path
		if enc := q.Encode(); enc != "" {
			return path + "?" + enc
		}
		return path
	}

	var from, to int64
	if result.Total > 0 && len(result.Items) > 0 {
		from = int64((page-1)*result.PerPage) + 1
		to = from + int64(len(result.Items)) - 1
	}

	return AdminPaginationView{
		Total:      result.Total,
		Page:       page,
		PerPage:    result.PerPage,
		TotalPages: totalPages,
		From:       from,
		To:         to,
		Offset:     (page - 1) * result.PerPage,
		FirstURL:   buildURL(1),
		PrevURL:    buildURL(page - 1),
		NextURL:    buildURL(page + 1),
		LastURL:    buildURL(totalPages),
	}
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
	if !h.checkPermission(c, typeName, "read") {
		return
	}

	orderField := "created_at"
	orderDir := "DESC"
	if hasSupport(typeDef.Supports, "sort_order") {
		orderField = "sort_order"
		orderDir = "ASC"
	}

	slug := AdminSlug(typeName)

	// Collect taxonomy definitions for table header
	var taxDefs []*content.TaxonomyDef
	for _, taxName := range typeDef.Taxonomies {
		if td := h.registry.GetTaxonomy(taxName); td != nil {
			taxDefs = append(taxDefs, td)
		}
	}

	lang := h.svc.AdminLanguage()
	screenKey := "content." + typeName
	columns := h.contentListColumns(lang, typeName, typeDef, taxDefs)
	if c.Query("screen_options") == "1" {
		if err := h.svc.SaveAdminListOptions(screenKey, columns, c.QueryArray("columns"), c.Query("per_page"), defaultAdminListPerPage); err != nil {
			c.Redirect(http.StatusFound, "/admin/"+slug+"?error="+url.QueryEscape(err.Error()))
			return
		}
		redirectToCleanList(c, slug)
		return
	}

	screenOptions := h.svc.LoadAdminListOptions(screenKey, columns, defaultAdminListPerPage)
	screenOptions.ActionURL = "/admin/" + slug
	visibleColumns := adminVisibleColumnMap(screenOptions.Columns)
	visibleColumnCount := adminVisibleColumnCount(screenOptions.Columns)
	tableColumnCount := visibleColumnCount
	if hasSupport(typeDef.Supports, "sort_order") {
		tableColumnCount++
	}

	searchQuery := strings.TrimSpace(c.Query("q"))
	dateField := contentListDateField(typeDef)
	filterTax := contentListFilterTaxonomy(taxDefs)
	filterTaxName := ""
	if filterTax != nil {
		filterTaxName = filterTax.Name
	}
	selectedTerm := strings.TrimSpace(c.Query("term"))
	selectedDate := strings.TrimSpace(c.Query("date"))
	page := parseAdminPage(c)
	result, err := h.svc.ListContentPageScoped(c, typeName, orderField, orderDir, page, screenOptions.PerPage, searchQuery, filterTaxName, selectedTerm, selectedDate, dateField)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if result.TotalPages > 0 && result.Page > result.TotalPages {
		q := cleanListQuery(c, false)
		q.Set("page", strconv.Itoa(result.TotalPages))
		target := c.Request.URL.Path
		if enc := q.Encode(); enc != "" {
			target += "?" + enc
		}
		c.Redirect(http.StatusFound, target)
		return
	}

	views := h.svc.ToDynamicContentViews(result.Items, typeDef)
	pagination := buildAdminPagination(c, result)
	filters := h.contentListFilters(c, slug, typeName, dateField, filterTax)
	reorderQuery := cleanListQuery(c, false)
	reorderQuery.Set("offset", strconv.Itoa(pagination.Offset))

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

	h.render(c, "content_list", gin.H{
		"Title":               h.contentTypeLabel(lang, typeName, typeDef.LabelPlural),
		"Active":              slug,
		"Items":               views,
		"TypeDef":             typeDef,
		"TypeName":            typeName,
		"Slug":                slug,
		"TaxDefs":             taxDefs,
		"Tabs":                tabs,
		"BackQuery":           listFilterQuery(c),
		"SearchQuery":         searchQuery,
		"ScreenOptions":       screenOptions,
		"VisibleColumns":      visibleColumns,
		"VisibleColumnCount":  visibleColumnCount,
		"TableColumnCount":    tableColumnCount,
		"Pagination":          pagination,
		"SearchHiddenInputs":  hiddenInputsFromQueryExcept(cleanListQuery(c, true), map[string]bool{"q": true}),
		"OptionsHiddenInputs": hiddenInputsFromQuery(cleanListQuery(c, true)),
		"PageHiddenInputs":    hiddenInputsFromQuery(cleanListQuery(c, true)),
		"Filters":             filters,
		"FilterHiddenInputs":  hiddenInputsFromQueryExcept(cleanListQuery(c, true), map[string]bool{"date": true, "term": true}),
		"ReorderQuery":        template.URL(reorderQuery.Encode()),
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
			t, err := h.svc.ParseAdminDateTimeInput(pubDate)
			if err == nil {
				item.PublishedAt = &t
			}
		} else if item.Status == content.StatusPublished {
			// Auto-set publish time when status is published but no date given
			now := time.Now().UTC()
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
	now = now.UTC()
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
			t, err := h.svc.ParseAdminDateTimeInput(pubDate)
			if err == nil {
				item.PublishedAt = &t
			}
		} else if item.Status == content.StatusPublished {
			// Auto-set publish time when status is published but no date given
			if item.PublishedAt == nil {
				now := time.Now().UTC()
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

	offset, _ := strconv.Atoi(c.Query("offset"))
	if err := h.svc.ReorderContent(typeName, req.IDs, offset); err != nil {
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
