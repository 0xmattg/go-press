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

// HookContentPermalinkPrefix lets plugins prepend a URL prefix (e.g. "/zh")
// to the permalink shown on the admin content edit form. The base permalink
// is `/<rewrite-or-type>/<slug>`; the prefix is prepended verbatim, so it
// should already include a leading slash and no trailing slash. Empty string
// (the default) means no prefix.
//
// Filter signature:
//
//	value:  string  (current prefix; pass-through if you don't apply)
//	args[0]: *gin.Context
//	args[1]: *content.Content (the content row being edited)
const HookContentPermalinkPrefix = "admin.content.permalink_prefix"
