package hook

// This file is the central registry of public GoPress extension point names.
//
// Keep hook names here when they are part of the framework contract and are
// expected to be used by plugins, themes, or core subsystems outside the package
// that fires them. Centralizing these constants makes it easier to discover the
// currently supported extension points and avoids string drift in Go code.
//
// Naming convention:
//   - The Go constant name starts with the owning domain: Theme*, Menu*, Admin*,
//     Content*, etc.
//   - The string value stays dot-delimited and stable for templates/config/docs:
//     "domain.area.event".
//   - Templates still call renderHook with the string value, for example
//     {{renderHook "header.nav.after" .}}. Go code should prefer the constant,
//     for example e.Hooks.AddFilter(hook.ThemeHeaderNavAfter, fn, 10).

// Theme template hook slots are rendered from front-end templates through the
// renderHook template function. Plugins can contribute HTML to these slots by
// registering a filter with the same hook name.
const (
	// ThemeHeaderNavAfter is emitted by themes at the end of the primary
	// navigation list. The filter value is template.HTML initialized to empty;
	// args[0] is the current page template data. Implementations should return
	// markup that matches the surrounding nav semantics, typically a <li>.
	ThemeHeaderNavAfter = "header.nav.after"

	// ThemeHeadEnd is emitted by themes immediately before the closing </head>
	// tag. The filter value is template.HTML initialized to empty; args[0] is
	// the current page template data. Use it to inject analytics scripts,
	// verification meta tags, or other <head>-scoped markup. Plugin-friendly
	// themes are expected to declare this slot once in their base layout so
	// any plugin (analytics, GTM, custom snippets, etc.) can target it.
	ThemeHeadEnd = "theme.head.end"

	// ThemeBodyOpen is emitted by themes immediately after the opening <body>
	// tag and before any visible content. The filter value is template.HTML
	// initialized to empty; args[0] is the current page template data. Typical
	// use: GTM noscript iframe, sitewide notice bars, A/B test bootstraps.
	ThemeBodyOpen = "theme.body.open"

	// ThemeFooterEnd is emitted by themes immediately before the closing
	// </body> tag, after all theme scripts. The filter value is template.HTML
	// initialized to empty; args[0] is the current page template data. Typical
	// use: deferred analytics, chat widgets, late-loading third-party scripts.
	ThemeFooterEnd = "theme.footer.end"
)

// Menu hooks are fired by core/menu while resolving or mutating menus. They are
// intentionally feature-neutral: a plugin may use them for translations,
// permissions, A/B navigation, cleanup, or other menu-related behavior without
// core/menu knowing about that plugin.
const (
	// MenuLocationResolve filters the menu returned for a registered location
	// after the default menu has been found and before it is returned to the
	// caller. The filter value is *menu.Menu; args[0] is the location string.
	MenuLocationResolve = "menu.location.resolve"

	// MenuDeleted fires after a menu row and its items are deleted. args[0] is
	// the deleted menu ID (uint). Plugins use it to clean up their own related
	// records.
	MenuDeleted = "menu.deleted"
)

// Option hooks fire after option/setting writes. They let plugins invalidate
// derived state (caches, i18n bundles, search indexes) without core/admin
// knowing which plugins care.
const (
	// OptionsBulkUpdated fires once after the admin saves a batch of options
	// (site settings, theme settings, plugin settings). It carries no arguments
	// — subscribers should re-read whatever they derive from Options.All().
	OptionsBulkUpdated = "options.bulk_updated"
)

// Admin content edit hooks let plugins inject custom fields into the content
// edit form (meta boxes) and react to saves without core knowing which fields
// each plugin owns. Pattern mirrors WordPress's add_meta_box / save_post.
const (
	// AdminContentFormFields is rendered as an HTML slot inside the admin
	// content edit form, after the built-in meta fields and before taxonomy
	// pickers. Filter value is template.HTML (initially empty); args are
	// (*gin.Context, *content.Content, *content.ContentTypeDef). Plugins
	// return additional <div class="form-group">...</div> markup; multiple
	// plugins compose by appending to the existing template.HTML value.
	AdminContentFormFields = "admin.content_form.fields"

	// AdminContentSaved fires once after a content row and its built-in
	// meta fields are persisted. Args: (*gin.Context, *content.Content).
	// Plugins read their own fields via c.PostForm(...) and persist them
	// to gp_content_meta with their own keys. Lets plugins own their data
	// without modifying the core save handler.
	AdminContentSaved = "admin.content.saved"
)

// SEO hooks let plugins override per-page SEOMeta after core SEOBuilder runs.
// Pattern mirrors WordPress + Yoast SEO: plugin reads override values from
// content meta and patches the SEOMeta before the template renders it.
const (
	// SEOContentMeta filters a SEOMeta value built for a content single page.
	// Args: (*content.Content, map[string]string contentMeta). Plugins return
	// a modified SEOMeta (or the unchanged value). Fires for content detail
	// pages only — home and archive use SEOContentMeta-equivalent overrides
	// only when a plugin extends them in the future.
	SEOContentMeta = "seo.content.meta"
)
