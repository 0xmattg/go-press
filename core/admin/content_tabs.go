package admin

// ContentListTab represents a filter tab rendered above the admin content list
// table. Plugins contribute tabs via the "admin.content_list.tabs" filter hook.
//
// Filter signature:
//
//	e.Hooks.AddFilter("admin.content_list.tabs",
//	    func(value interface{}, args ...interface{}) interface{} {
//	        tabs := value.([]admin.ContentListTab)
//	        c := args[0].(*gin.Context)
//	        typeName := args[1].(string)
//	        // ... append tabs
//	        return tabs
//	    }, 10)
//
// When no plugin contributes tabs, the slice stays empty and the template
// renders nothing — identical to the pre-multilang behavior.
type ContentListTab struct {
	Key    string // stable identifier (e.g. "all", "en", "zh")
	Label  string // display label
	Count  int    // optional item count (rendered as a small badge when > 0)
	Active bool   // whether this tab is the currently selected one
	URL    string // href the tab links to (keeps existing query params if desired)
}

// HookContentListTabs is the filter hook name plugins use to contribute tabs.
const HookContentListTabs = "admin.content_list.tabs"

// HookTaxonomyListTabs lets plugins add request-aware tabs above a taxonomy
// list without core knowing what those variants represent. The filter value is
// []ContentListTab; args are (*gin.Context, taxonomyType string).
const HookTaxonomyListTabs = "admin.taxonomy_list.tabs"

// HookContentPermalinkPrefix lets plugins prepend a row-specific URL prefix
// (e.g. "/zh") to public content links rendered throughout the admin. The base
// permalink comes from the registered rewrite definition; the prefix is
// prepended verbatim, so it should already include a leading slash and no
// trailing slash. Empty string (the default) means no prefix.
//
// Filter signature:
//
//	value:  string  (current prefix; pass-through if you don't apply)
//	args[0]: *gin.Context
//	args[1]: *content.Content (the content row being edited)
const HookContentPermalinkPrefix = "admin.content.permalink_prefix"
