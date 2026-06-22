package admin

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// MenuList shows the menu management page.
func (h *Handler) MenuList(c *gin.Context) {
	if !h.checkPermission(c, "menu", "read") {
		return
	}
	if h.menuCallbacks == nil || h.menuCallbacks.AllFn == nil {
		c.Redirect(http.StatusFound, "/admin/?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.unavailable")))
		return
	}

	menus, err := h.menuCallbacks.AllFn()
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/?error="+err.Error())
		return
	}

	lang := h.svc.AdminLanguage()
	locations := h.localizeMenuLocations(h.menuCallbacks.LocationsFn(), lang)

	h.render(c, "menus", gin.H{
		"Active":    "menus",
		"Title":     adminT(lang, "nav.menus"),
		"PageTitle": adminT(lang, "nav.menus"),
		"Menus":     menus,
		"Locations": locations,
	})
}

// MenuCreate handles creating a new menu.
func (h *Handler) MenuCreate(c *gin.Context) {
	if !h.checkPermission(c, "menu", "update") {
		return
	}
	if h.menuCallbacks == nil || h.menuCallbacks.CreateFn == nil {
		c.Redirect(http.StatusFound, "/admin/menus?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.unavailable")))
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	location := strings.TrimSpace(c.PostForm("location"))

	if name == "" {
		c.Redirect(http.StatusFound, "/admin/menus?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.name_required")))
		return
	}

	if err := h.menuCallbacks.CreateFn(name, location); err != nil {
		c.Redirect(http.StatusFound, "/admin/menus?error="+err.Error())
		return
	}

	h.invalidatePageCache()
	c.Redirect(http.StatusFound, "/admin/menus?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.created")))
}

// MenuEdit shows the menu item editing page.
func (h *Handler) MenuEdit(c *gin.Context) {
	if !h.checkPermission(c, "menu", "read") {
		return
	}
	if h.menuCallbacks == nil || h.menuCallbacks.GetByIDFn == nil {
		c.Redirect(http.StatusFound, "/admin/menus?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.unavailable")))
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/menus?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.invalid_id")))
		return
	}

	m, err := h.menuCallbacks.GetByIDFn(uint(id))
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/menus?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.not_found")))
		return
	}

	// Get content items for the "add from content" selector
	contentItems := h.getContentItemsForMenu()

	// Serialize menu items to JSON to avoid html/template JS context escaping
	itemsJSON, _ := json.Marshal(flattenMenuItems(m.Items))
	lang := h.svc.AdminLanguage()
	locations := h.localizeMenuLocations(h.menuCallbacks.LocationsFn(), lang)

	h.render(c, "menu_edit", gin.H{
		"Active":        "menus",
		"Title":         adminT(lang, "menu.edit_title", m.Name),
		"PageTitle":     adminT(lang, "menu.edit_title", m.Name),
		"Menu":          m,
		"Locations":     locations,
		"ContentItems":  contentItems,
		"MenuItemsJSON": template.JS(string(itemsJSON)),
	})
}

// MenuUpdate handles updating a menu's name/location.
func (h *Handler) MenuUpdate(c *gin.Context) {
	if !h.checkPermission(c, "menu", "update") {
		return
	}
	if h.menuCallbacks == nil || h.menuCallbacks.UpdateFn == nil {
		c.Redirect(http.StatusFound, "/admin/menus?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.unavailable")))
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/menus?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.invalid_id")))
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	location := strings.TrimSpace(c.PostForm("location"))

	if name == "" {
		c.Redirect(http.StatusFound, "/admin/menus/"+c.Param("id")+"/edit?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.name_required")))
		return
	}

	if err := h.menuCallbacks.UpdateFn(uint(id), name, location); err != nil {
		c.Redirect(http.StatusFound, "/admin/menus/"+c.Param("id")+"/edit?error="+err.Error())
		return
	}

	h.invalidatePageCache()
	c.Redirect(http.StatusFound, "/admin/menus/"+c.Param("id")+"/edit?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.updated")))
}

func (h *Handler) localizeMenuLocations(locations []MenuLocationInfo, lang string) []MenuLocationInfo {
	if len(locations) == 0 {
		return locations
	}
	result := make([]MenuLocationInfo, len(locations))
	lang = normalizeAdminLanguage(lang)
	for i, loc := range locations {
		result[i] = loc
		if label := catalogMessage(h.activeThemeCatalog(), lang, "admin.menu.location."+loc.Name); label != "" {
			result[i].Label = label
			continue
		}
		if label := adminMessage(lang, "menu.location."+loc.Name); label != "" {
			result[i].Label = label
		}
	}
	return result
}

// MenuDelete handles deleting a menu.
func (h *Handler) MenuDelete(c *gin.Context) {
	if !h.checkPermission(c, "menu", "update") {
		return
	}
	if h.menuCallbacks == nil || h.menuCallbacks.DeleteFn == nil {
		c.Redirect(http.StatusFound, "/admin/menus?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.unavailable")))
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/menus?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.invalid_id")))
		return
	}

	if err := h.menuCallbacks.DeleteFn(uint(id)); err != nil {
		c.Redirect(http.StatusFound, "/admin/menus?error="+err.Error())
		return
	}

	h.invalidatePageCache()
	c.Redirect(http.StatusFound, "/admin/menus?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "menu.deleted")))
}

// MenuSaveItems handles saving menu items via JSON (AJAX).
func (h *Handler) MenuSaveItems(c *gin.Context) {
	if !h.checkPermission(c, "menu", "update") {
		return
	}
	if h.menuCallbacks == nil || h.menuCallbacks.SaveItemsFn == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": adminT(h.svc.AdminLanguage(), "menu.unavailable")})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": adminT(h.svc.AdminLanguage(), "menu.invalid_id")})
		return
	}

	var items []MenuItemInfo
	if err := c.ShouldBindJSON(&items); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": adminT(h.svc.AdminLanguage(), "menu.invalid_items", err.Error())})
		return
	}

	if err := h.menuCallbacks.SaveItemsFn(uint(id), items); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.invalidatePageCache()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": adminT(h.svc.AdminLanguage(), "menu.items_saved")})
}

// ContentOption is a simple content item for menu item selectors.
type ContentOption struct {
	ID    uint
	Title string
	Type  string
	URL   string
}

// getContentItemsForMenu returns published content items grouped by type for the menu editor.
func (h *Handler) getContentItemsForMenu() map[string][]ContentOption {
	result := make(map[string][]ContentOption)
	for _, typeDef := range h.registry.AllTypes() {
		if !typeDef.HasArchive {
			continue
		}
		items, err := h.svc.ListContent(typeDef.Name, "title", "ASC")
		if err != nil {
			continue
		}
		var options []ContentOption
		for _, item := range items {
			if item.Status != "publish" {
				continue
			}
			options = append(options, ContentOption{
				ID:    item.ID,
				Title: item.Title,
				Type:  typeDef.Label,
				URL:   "/" + item.Slug,
			})
		}
		if len(options) > 0 {
			result[typeDef.Label] = options
		}
	}
	return result
}

// menuItemJSON is a flat representation for JSON serialization to the template.
type menuItemJSON struct {
	ID        uint   `json:"id"`
	ParentID  *uint  `json:"parentId"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Target    string `json:"target"`
	ContentID *uint  `json:"contentId"`
	SortOrder int    `json:"sortOrder"`
}

// flattenMenuItems converts a tree of MenuItemInfo to a flat slice for JSON output.
func flattenMenuItems(items []MenuItemInfo) []menuItemJSON {
	result := make([]menuItemJSON, 0)
	for _, item := range items {
		result = append(result, menuItemJSON{
			ID:        item.ID,
			ParentID:  item.ParentID,
			Title:     item.Title,
			URL:       item.URL,
			Target:    item.Target,
			ContentID: item.ContentID,
			SortOrder: item.SortOrder,
		})
		if len(item.Children) > 0 {
			result = append(result, flattenMenuItems(item.Children)...)
		}
	}
	return result
}
