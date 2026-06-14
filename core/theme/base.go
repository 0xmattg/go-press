package theme

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"

	"go-press/core/content"
	"go-press/core/hook"
	coreI18n "go-press/core/i18n"
	"go-press/core/mail"
	"go-press/core/menu"
	"go-press/core/rewrite"
	"go-press/core/taxonomy"
	"go-press/pkg/logger"

	"github.com/gin-gonic/gin"
)

// RouteHandler describes an optional theme-owned route.
//
// BaseTheme stores custom routes internally, but this struct is useful for
// themes that want to keep a declarative route list before registration.
type RouteHandler struct {
	Method  string // "GET" or "POST"
	Path    string // e.g. "/about", "/contact"
	Handler gin.HandlerFunc
}

// BaseTheme provides the default front-end runtime for themes.
//
// Themes embed BaseTheme and gain:
//   - URL resolution via core rewrite engine
//   - Template hierarchy (WordPress-style fallback)
//   - SEO meta injection
//   - Common template functions
//
// Themes only need to:
//  1. Register their content types in Setup()
//  2. Provide templates following the naming convention
//  3. Optionally register custom static-page routes (e.g. /about, /contact)
type BaseTheme struct {
	App           App
	ThemeDir      string
	Templates     *TemplateEngine
	PageTemplates map[string]*template.Template
	CustomRoutes  map[string]map[string]gin.HandlerFunc // path → method → handler
	customFuncMap template.FuncMap
}

// InitBase initializes the BaseTheme. Call this from the theme constructor.
//
// extraFuncs are merged after common engine functions, so theme-specific
// helpers can add names without reimplementing the shared FuncMap. Avoid
// overriding core helper names unless a theme deliberately needs different
// behavior.
func (b *BaseTheme) InitBase(app App, themeDir string, extraFuncs template.FuncMap) {
	b.App = app
	b.ThemeDir = themeDir
	b.PageTemplates = make(map[string]*template.Template)
	b.CustomRoutes = make(map[string]map[string]gin.HandlerFunc)
	b.customFuncMap = extraFuncs
}

// AddRoute registers a custom theme route such as /about or /contact.
//
// Custom routes take priority over rewrite-engine resolution. Use them for
// static pages or special workflows that are part of a theme's front-end
// experience but are not represented as Content rows.
func (b *BaseTheme) AddRoute(method, path string, handler gin.HandlerFunc) {
	if b.CustomRoutes[path] == nil {
		b.CustomRoutes[path] = make(map[string]gin.HandlerFunc)
	}
	b.CustomRoutes[path][method] = handler
}

// MailSender returns the core mail sender for theme-owned workflows.
//
// Prefer using it from handlers or services after user input has been validated.
// Templates should not send mail directly.
func (b *BaseTheme) MailSender() mail.Sender {
	if b == nil || b.App == nil {
		return nil
	}
	return b.App.MailSender()
}

// LoadTemplates compiles all templates using the core TemplateEngine
// with merged common + theme-specific function maps.
func (b *BaseTheme) LoadTemplates(t Theme) {
	pageTemplates, err := LoadAllPageBundles(t)
	if err != nil {
		logger.Error("BaseTheme: failed to load page templates", "error", err)
	} else {
		b.PageTemplates = pageTemplates
	}

	b.Templates = NewTemplateEngine(t)
	if err := b.Templates.Load(); err != nil {
		logger.Error("BaseTheme: failed to load templates", "error", err)
	}
}

// BaseFuncMap returns shared template helpers plus theme-specific functions.
//
// The merged map includes URL builders, SEO renderers, menu lookup, hook slots,
// responsive image helpers, and i18n helpers when their backing engine services
// are available. Missing services degrade to omitted helpers rather than
// panicking during template load.
func (b *BaseTheme) BaseFuncMap() template.FuncMap {
	engineFuncs := template.FuncMap{}
	if b.App != nil {
		siteLoc := b.App.SiteLocation()
		if siteLoc == nil {
			siteLoc = time.Local
		}
		engineFuncs["formatDate"] = func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.In(siteLoc).Format("Jan 2, 2006")
		}
		engineFuncs["formatDateTime"] = func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.In(siteLoc).Format("2006-01-02 15:04")
		}
		rw := b.App.RewriteEngine()
		seo := b.App.SEOBuilder()
		if rw != nil {
			engineFuncs["buildURL"] = rw.BuildURL
			engineFuncs["archiveURL"] = rw.BuildArchiveURL
			engineFuncs["contentURL"] = func(item interface{}, fallbackType string) string {
				if url := stringField(item, "URL"); url != "" {
					return url
				}
				slug := stringField(item, "Slug")
				if slug == "" {
					return "/"
				}
				contentType := stringField(item, "Type")
				if contentType == "" {
					contentType = fallbackType
				}
				if contentType == "" {
					return "/" + strings.TrimPrefix(slug, "/")
				}
				return rw.BuildURL(contentType, slug)
			}
		}
		if seo != nil {
			engineFuncs["seoHead"] = func(meta rewrite.SEOMeta) template.HTML {
				return seo.RenderHead(meta)
			}
			// seoHeadFor accepts the entire template root context and renders
			// SEO head HTML if a SEO field/key exists with a SEOMeta value.
			// Returns empty string otherwise. Works on both map[string]any
			// (gin.H) and custom struct data — themes that wrap data in their
			// own struct don't need to add a SEO field for templates to be
			// safe; missing field just renders nothing.
			engineFuncs["seoHeadFor"] = func(data interface{}) template.HTML {
				if data == nil {
					return ""
				}
				v := reflect.ValueOf(data)
				if v.Kind() == reflect.Ptr {
					if v.IsNil() {
						return ""
					}
					v = v.Elem()
				}
				var seoVal reflect.Value
				switch v.Kind() {
				case reflect.Map:
					seoVal = v.MapIndex(reflect.ValueOf("SEO"))
				case reflect.Struct:
					seoVal = v.FieldByName("SEO")
				default:
					return ""
				}
				if !seoVal.IsValid() {
					return ""
				}
				if seoVal.Kind() == reflect.Interface {
					seoVal = seoVal.Elem()
				}
				if !seoVal.IsValid() {
					return ""
				}
				meta, ok := seoVal.Interface().(rewrite.SEOMeta)
				if !ok {
					return ""
				}
				return seo.RenderHead(meta)
			}
		}
		ms := b.App.MenuStore()
		if ms != nil {
			engineFuncs["menuByLocation"] = func(location string) *menu.Menu {
				return ms.GetByLocation(location)
			}
		}
		if hooks := b.App.HookBus(); hooks != nil {
			engineFuncs["renderHook"] = func(name string, data interface{}) template.HTML {
				if name == "" {
					return ""
				}
				args := []interface{}{data}
				if ctx := templateGinContext(data); ctx != nil {
					args = append(args, ctx)
				}
				output := hooks.ApplyFilter(name, template.HTML(""), args...)
				switch v := output.(type) {
				case template.HTML:
					return v
				case string:
					return template.HTML(v)
				default:
					return template.HTML(fmt.Sprint(v))
				}
			}
		}
		if mediaRepo := b.App.MediaRepo(); mediaRepo != nil {
			engineFuncs["responsiveImage"] = func(src, alt, className, sizes, loading string) template.HTML {
				return renderResponsiveImage(mediaRepo, src, alt, imageAttrs{
					Class:   className,
					Sizes:   sizes,
					Loading: loading,
				})
			}
			engineFuncs["responsiveImagePriority"] = func(src, alt, className, sizes string) template.HTML {
				return renderResponsiveImage(mediaRepo, src, alt, imageAttrs{
					Class:         className,
					Sizes:         sizes,
					Loading:       "eager",
					FetchPriority: "high",
				})
			}
			engineFuncs["responsiveImagePreload"] = func(src, sizes string) template.HTML {
				return renderResponsivePreload(mediaRepo, src, sizes)
			}
		}
		i18nMgr := b.App.I18nManager()
		if i18nMgr != nil {
			engineFuncs["T"] = func(c *gin.Context, msgID string) string {
				return i18nMgr.Translate(c, msgID)
			}
			// currentLang returns the current request language code (e.g. "zh")
			// or the configured default if no language middleware has set it.
			engineFuncs["currentLang"] = func(c *gin.Context) string {
				if c != nil {
					if v, ok := c.Get("current_lang"); ok {
						if s, ok := v.(string); ok && s != "" {
							return s
						}
					}
				}
				return i18nMgr.DefaultLang()
			}
			// langPrefixURL prepends the current request language prefix to a path
			// (no prefix for the default language). Themes use this for SEO-clean
			// internal links like /zh/products/xxx so the language stays consistent.
			engineFuncs["langPrefixURL"] = func(c *gin.Context, path string) string {
				if path == "" {
					return path
				}
				if !isLanguagePrefixableURL(path) {
					return path
				}
				defLang := i18nMgr.DefaultLang()
				lang := defLang
				if c != nil {
					if v, ok := c.Get("current_lang"); ok {
						if s, ok := v.(string); ok && s != "" {
							lang = s
						}
					}
				}
				if lang == defLang {
					return path
				}
				if path[0] != '/' {
					path = "/" + path
				}
				if path == "/" {
					return "/" + lang + "/"
				}
				return "/" + lang + path
			}
		}
	}
	return MergeFuncMap(CommonFuncMap(), engineFuncs, b.customFuncMap)
}

func templateGinContext(data interface{}) *gin.Context {
	if data == nil {
		return nil
	}
	if c, ok := data.(*gin.Context); ok {
		return c
	}

	v := reflect.ValueOf(data)
	for v.IsValid() && v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Map:
		key := reflect.ValueOf("Ctx")
		if key.Type().AssignableTo(v.Type().Key()) {
			val := v.MapIndex(key)
			if val.IsValid() {
				if val.Kind() == reflect.Interface {
					val = val.Elem()
				}
				if val.IsValid() && val.CanInterface() {
					if c, ok := val.Interface().(*gin.Context); ok {
						return c
					}
				}
			}
		}
	case reflect.Struct:
		field := v.FieldByName("Ctx")
		if field.IsValid() && field.CanInterface() {
			if c, ok := field.Interface().(*gin.Context); ok {
				return c
			}
		}
	}
	return nil
}

// ServeHTTP implements the default front-end request handling:
//  1. Check custom routes (static pages)
//  2. Resolve URL via rewrite engine → content type
//  3. Find template via hierarchy
//  4. Render with SEO meta
func (b *BaseTheme) ServeHTTP(c *gin.Context) {
	path := c.Request.URL.Path
	method := c.Request.Method

	// 1. Check custom static-page routes
	if methods, ok := b.CustomRoutes[path]; ok {
		if handler, ok := methods[method]; ok {
			handler(c)
			return
		}
		// Try GET fallback for HEAD
		if method == "HEAD" {
			if handler, ok := methods["GET"]; ok {
				handler(c)
				return
			}
		}
	}

	// 2. Home page
	if path == "/" {
		b.renderHome(c)
		return
	}

	// 3. Resolve URL via rewrite engine
	if b.App == nil || b.App.RewriteEngine() == nil {
		b.render404(c)
		return
	}
	route := b.App.RewriteEngine().Resolve(path)
	if route == nil {
		b.render404(c)
		return
	}

	if route.IsTaxonomy {
		b.renderTaxonomy(c, route)
	} else if route.IsArchive {
		b.renderArchive(c, route)
	} else {
		b.renderSingle(c, route)
	}
}

// renderHome renders the homepage using front-page.tmpl → home.tmpl → index.tmpl.
func (b *BaseTheme) renderHome(c *gin.Context) {
	candidates := ResolveTemplate("", "", "", "", false, true, false)
	tmpl := b.Templates.Resolve(candidates)
	if tmpl == nil {
		c.String(http.StatusInternalServerError, "No home template found")
		return
	}

	data := b.buildBaseData(c, "Home")

	// Inject SEO
	if b.App != nil && b.App.SEOBuilder() != nil {
		desc := ""
		if b.App.OptionsStore() != nil {
			desc = b.App.OptionsStore().Get("site_description")
		}
		seo := b.App.SEOBuilder().ForHome(desc)
		ApplySiteOptionOverrides(b.App, &seo)
		data["SEO"] = seo
	}

	b.executeTemplate(c, tmpl, data)
}

// renderArchive renders an archive listing page with template hierarchy.
func (b *BaseTheme) renderArchive(c *gin.Context, route *rewrite.ResolvedRoute) {
	typeDef := b.App.ContentRegistry().GetType(route.ContentType)
	pageTmpl := b.resolvePageTemplate(archivePageCandidates(route.ContentType, typeDef))
	var tmpl *template.Template
	if pageTmpl == nil && b.Templates != nil {
		candidates := ResolveTemplate(route.ContentType, "", "", "", true, false, false)
		tmpl = b.Templates.Resolve(candidates)
	}

	// Query published content of this type
	page := route.Page
	if page < 1 {
		page = 1
	}
	perPage := 20

	q := content.NewQuery(content.ScopedDB(c, b.App.Database())).
		Type(route.ContentType).Published()
	activeTaxonomy, activeTerm := archiveQueryTaxonomyFilter(c, typeDef)
	if activeTaxonomy != "" && activeTerm != "" {
		q = q.Taxonomy(activeTaxonomy, activeTerm)
	}
	result, err := q.
		OrderBy(archiveOrderField(typeDef), archiveOrderDir(typeDef)).
		Paginate(page, perPage)
	if err != nil {
		logger.Error("BaseTheme: archive query failed", "type", route.ContentType, "error", err)
		b.render404(c)
		return
	}

	data := b.buildBaseData(c, "")
	if typeDef != nil {
		data["Title"] = LocalizedArchiveTitle(c, b.App.I18nManager(), typeDef)
	}
	data["ActivePage"] = route.ContentType
	items := b.contentViews(c, result.Items)
	data["ContentType"] = route.ContentType
	data["TypeDef"] = typeDef
	data["ActiveTaxonomy"] = activeTaxonomy
	data["ActiveTerm"] = activeTerm
	data["ActiveTermSlug"] = activeTerm
	data["ActiveCat"] = ""
	data["ActiveTag"] = ""
	if activeTaxonomy == "category" {
		data["ActiveCat"] = activeTerm
	}
	if activeTaxonomy == "tag" {
		data["ActiveTag"] = activeTerm
	}
	data["Items"] = items
	data[pluralAlias(route.ContentType)] = items
	addLegacyListAliases(data, items)
	data["Pagination"] = result
	b.addArchiveTaxonomyData(data)
	if rw := b.App.RewriteEngine(); rw != nil {
		data["ArchiveURL"] = rw.BuildArchiveURL(route.ContentType)
	}

	// Inject SEO
	if b.App.SEOBuilder() != nil && typeDef != nil {
		seo := b.App.SEOBuilder().ForArchiveTitle(typeDef, LocalizedArchiveTitle(c, b.App.I18nManager(), typeDef))
		ApplySiteOptionOverrides(b.App, &seo)
		data["SEO"] = seo
	}

	// If theme has a template, use it
	if pageTmpl != nil {
		b.executeTemplate(c, pageTmpl, data)
		return
	}
	if tmpl != nil {
		b.executeTemplate(c, tmpl, data)
		return
	}

	// Fallback: use built-in archive template
	if rw := b.App.RewriteEngine(); rw != nil {
		data["BuildURL"] = rw.BuildURL
	}
	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if ferr := FallbackArchiveTemplate().Execute(c.Writer, data); ferr != nil {
		logger.Error("BaseTheme: fallback archive render error", "error", ferr)
	}
}

// renderSingle renders a single content item with template hierarchy.
func (b *BaseTheme) renderSingle(c *gin.Context, route *rewrite.ResolvedRoute) {
	// Use scoped lookup so plugins like multilang can disambiguate same-slug
	// rows by language: /products/foo and /zh/products/foo each resolve to the
	// row that matches the active request scope.
	item, err := b.App.ContentRepo().FindBySlugScoped(c, route.ContentType, route.Slug)
	if err != nil || item == nil {
		b.render404(c)
		return
	}

	// Only show published content
	if item.Status != content.StatusPublished {
		b.render404(c)
		return
	}

	typeDef := b.App.ContentRegistry().GetType(route.ContentType)
	pageTmpl := b.resolvePageTemplate(singlePageCandidates(route.ContentType, route.Slug, typeDef))
	var tmpl *template.Template
	if pageTmpl == nil && b.Templates != nil {
		candidates := ResolveTemplate(route.ContentType, route.Slug, "", "", false, false, false)
		tmpl = b.Templates.Resolve(candidates)
	}

	// Load meta
	meta, _ := b.App.ContentRepo().GetMeta(item.ID)

	// Load taxonomies
	var categories, tags []map[string]interface{}
	if b.App.TaxonomyRepo() != nil {
		cats, _ := b.App.TaxonomyRepo().GetContentTaxonomies(item.ID, "category")
		for _, cat := range cats {
			categories = append(categories, map[string]interface{}{
				"ID": cat.ID, "Name": cat.Term.Name, "Slug": cat.Term.Slug,
			})
		}
		tagItems, _ := b.App.TaxonomyRepo().GetContentTaxonomies(item.ID, "tag")
		for _, tag := range tagItems {
			tags = append(tags, map[string]interface{}{
				"ID": tag.ID, "Name": tag.Term.Name, "Slug": tag.Term.Slug,
			})
		}
	}

	data := b.buildBaseData(c, item.Title)
	view := b.contentView(c, *item)
	related := b.relatedContentViews(c, route.ContentType, item.ID, 3)
	data["ActivePage"] = route.ContentType
	data["Item"] = view
	data[singularAlias(route.ContentType)] = view
	addLegacySingleAliases(data, view)
	data["Meta"] = meta
	data["Categories"] = categories
	data["Tags"] = tags
	data["Related"] = related
	data["ContentType"] = route.ContentType
	data["TypeDef"] = typeDef
	if rw := b.App.RewriteEngine(); rw != nil {
		data["ArchiveURL"] = rw.BuildArchiveURL(route.ContentType)
		data["Permalink"] = rw.BuildURL(route.ContentType, item.Slug)
	}

	// Inject SEO
	if b.App.SEOBuilder() != nil && typeDef != nil {
		seo := b.App.SEOBuilder().ForContent(item, typeDef)
		ApplySiteOptionOverrides(b.App, &seo)
		ApplyContentMetaSEO(b.App.HookBus(), b.App.ContentRepo(), &seo, item)
		data["SEO"] = seo
	}

	// If theme has a template, use it
	if pageTmpl != nil {
		b.executeTemplate(c, pageTmpl, data)
		return
	}
	if tmpl != nil {
		b.executeTemplate(c, tmpl, data)
		return
	}

	// Fallback: use built-in single template
	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if ferr := FallbackSingleTemplate().Execute(c.Writer, data); ferr != nil {
		logger.Error("BaseTheme: fallback single render error", "error", ferr)
	}
}

// renderTaxonomy renders a taxonomy term archive.
func (b *BaseTheme) renderTaxonomy(c *gin.Context, route *rewrite.ResolvedRoute) {
	pageTmpl := b.resolvePageTemplate([]string{
		"taxonomy-" + route.TaxSlug + "-" + route.TermSlug,
		"taxonomy-" + route.TaxSlug,
		"taxonomy-archive",
		"taxonomy",
		"archive",
	})
	var tmpl *template.Template
	if pageTmpl == nil && b.Templates != nil {
		candidates := ResolveTemplate("", "", route.TaxSlug, route.TermSlug, false, false, false)
		tmpl = b.Templates.Resolve(candidates)
	}

	items, err := content.NewQuery(content.ScopedDB(c, b.App.Database())).
		Published().
		Types(b.registeredTypeNames()).
		Taxonomy(route.TaxSlug, route.TermSlug).
		OrderBy("published_at", "DESC").
		Get()
	if err != nil {
		b.render404(c)
		return
	}

	// Resolve term display name
	termName := route.TermSlug
	taxLabel := route.TaxSlug
	if b.App.TaxonomyRepo() != nil {
		if term, terr := b.App.TaxonomyRepo().GetTermBySlug(route.TermSlug); terr == nil && term != nil {
			termName = term.Name
		}
	}
	if taxDef := b.App.ContentRegistry().GetTaxonomy(route.TaxSlug); taxDef != nil {
		taxLabel = taxDef.Label
	}

	data := b.buildBaseData(c, termName)
	data["TaxSlug"] = route.TaxSlug
	data["TermSlug"] = route.TermSlug
	data["TaxLabel"] = taxLabel
	data["TermName"] = termName
	data["Items"] = b.taxonomyArchiveViews(c, items)

	// If theme has a template, use it (with "base" block)
	if pageTmpl != nil {
		b.executeTemplate(c, pageTmpl, data)
		return
	}
	if tmpl != nil {
		b.executeTemplate(c, tmpl, data)
		return
	}

	// Fallback: use built-in template (standalone, no "base" block)
	if rw := b.App.RewriteEngine(); rw != nil {
		data["BuildURL"] = rw.BuildURL
	}
	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if ferr := FallbackTaxonomyTemplate().Execute(c.Writer, data); ferr != nil {
		logger.Error("BaseTheme: fallback taxonomy render error", "error", ferr)
	}
}

// render404 renders the 404 page using the template hierarchy.
func (b *BaseTheme) render404(c *gin.Context) {
	candidates := ResolveTemplate("", "", "", "", false, false, true)
	tmpl := b.Templates.Resolve(candidates)
	if tmpl == nil {
		c.String(http.StatusNotFound, "404 - Page Not Found")
		return
	}
	data := b.buildBaseData(c, "Page Not Found")
	c.Status(http.StatusNotFound)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(c.Writer, data); err != nil {
		logger.Error("BaseTheme: 404 template render error", "error", err)
	}
}

// registeredTypeNames returns the names of all content types currently in the Registry.
// Used to scope queries so that content from unregistered (previous theme) types is excluded.
func (b *BaseTheme) registeredTypeNames() []string {
	allTypes := b.App.ContentRegistry().AllTypes()
	names := make([]string, len(allTypes))
	for i, t := range allTypes {
		names[i] = t.Name
	}
	return names
}

// buildBaseData returns common template data shared by all pages.
func (b *BaseTheme) buildBaseData(c *gin.Context, title string) gin.H {
	data := gin.H{
		"Title": title,
	}
	if b.App != nil && b.App.OptionsStore() != nil {
		data["Settings"] = b.App.OptionsStore().All()
	}
	data["RecentPosts"] = b.recentPostViews(c, 3)
	return data
}

// executeTemplate renders a resolved template with the given data.
func (b *BaseTheme) executeTemplate(c *gin.Context, tmpl *template.Template, data gin.H) {
	data["Ctx"] = c
	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "base", data); err != nil {
		logger.Error("BaseTheme: template render error", "template", tmpl.Name(), "error", err)
	}
}

func (b *BaseTheme) resolvePageTemplate(candidates []string) *template.Template {
	if len(b.PageTemplates) == 0 {
		return nil
	}
	for _, name := range candidates {
		if tmpl := b.PageTemplates[name]; tmpl != nil {
			return tmpl
		}
	}
	return nil
}

func archivePageCandidates(contentType string, typeDef *content.ContentTypeDef) []string {
	candidates := []string{"archive-" + contentType}
	if typeDef != nil && typeDef.Rewrite.Slug != "" {
		candidates = append(candidates, strings.Trim(typeDef.Rewrite.Slug, "/"))
	}
	candidates = append(candidates, contentType, pluralName(contentType))
	if typeDef != nil && typeDef.Templates.Archive != "" {
		candidates = append(candidates, typeDef.Templates.Archive)
	}
	candidates = append(candidates, "archive")
	return uniqueStrings(candidates)
}

func singlePageCandidates(contentType, slug string, typeDef *content.ContentTypeDef) []string {
	candidates := []string{}
	if slug != "" {
		candidates = append(candidates, "single-"+contentType+"-"+slug)
	}
	candidates = append(candidates, "single-"+contentType, contentType+"-detail", contentType+"_detail")
	if typeDef != nil && typeDef.Rewrite.Slug != "" {
		candidates = append(candidates, strings.Trim(typeDef.Rewrite.Slug, "/")+"-detail")
	}
	if typeDef != nil && typeDef.Templates.Single != "" {
		candidates = append(candidates, typeDef.Templates.Single)
	}
	candidates = append(candidates, "single")
	return uniqueStrings(candidates)
}

func archiveOrderField(typeDef *content.ContentTypeDef) string {
	if contentTypeSupports(typeDef, "sort_order") {
		return "sort_order"
	}
	return "published_at"
}

func archiveOrderDir(typeDef *content.ContentTypeDef) string {
	if contentTypeSupports(typeDef, "sort_order") {
		return "ASC"
	}
	return "DESC"
}

func archiveQueryTaxonomyFilter(c *gin.Context, typeDef *content.ContentTypeDef) (string, string) {
	if c == nil || typeDef == nil {
		return "", ""
	}
	for _, taxName := range typeDef.Taxonomies {
		if term := strings.TrimSpace(c.Query(taxName)); term != "" {
			return taxName, term
		}
	}
	return "", ""
}

func contentTypeSupports(typeDef *content.ContentTypeDef, feature string) bool {
	if typeDef == nil {
		return false
	}
	for _, support := range typeDef.Supports {
		if support == feature {
			return true
		}
	}
	return false
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (b *BaseTheme) contentViews(c *gin.Context, items []content.Content) []map[string]interface{} {
	views := make([]map[string]interface{}, len(items))
	for i, item := range items {
		views[i] = b.contentView(c, item)
	}
	return views
}

func (b *BaseTheme) contentView(c *gin.Context, item content.Content) map[string]interface{} {
	meta, _ := b.App.ContentRepo().GetMeta(item.ID)
	categories := b.termViews(item.ID, "category")
	tags := b.termViews(item.ID, "tag")

	view := map[string]interface{}{
		"ID":          item.ID,
		"Type":        item.Type,
		"Status":      item.Status,
		"Title":       item.Title,
		"Slug":        item.Slug,
		"Content":     item.Content,
		"Description": item.Content,
		"Excerpt":     item.Excerpt,
		"ImageURL":    item.ImageURL,
		"AuthorID":    item.AuthorID,
		"ParentID":    item.ParentID,
		"SortOrder":   item.SortOrder,
		"PublishedAt": item.PublishedAt,
		"CreatedAt":   item.CreatedAt,
		"UpdatedAt":   item.UpdatedAt,
		"Meta":        meta,
		"Categories":  categories,
		"Tags":        tags,
	}
	if len(categories) > 0 {
		view["Category"] = categories[0]
	} else {
		view["Category"] = map[string]interface{}{}
	}
	if rw := b.App.RewriteEngine(); rw != nil {
		view["URL"] = rw.BuildURL(item.Type, item.Slug)
	}

	keys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		val := meta[key]
		view[key] = val
		view[exportedKey(key)] = val
	}
	view["GalleryImages"] = splitCSV(meta["gallery_images"])

	return view
}

func (b *BaseTheme) taxonomyArchiveViews(c *gin.Context, items []content.Content) []map[string]interface{} {
	views := b.contentViews(c, items)
	for _, view := range views {
		contentType, _ := view["Type"].(string)
		slug, _ := view["Slug"].(string)
		view["ContentType"] = contentType
		view["TypeLabel"] = contentType
		if typeDef := b.App.ContentRegistry().GetType(contentType); typeDef != nil {
			view["TypeLabel"] = LocalizedContentTypeLabel(c, b.App.I18nManager(), typeDef)
		}
		if rw := b.App.RewriteEngine(); rw != nil {
			view["DetailURL"] = rw.BuildURL(contentType, slug)
		}
	}
	return views
}

func (b *BaseTheme) relatedContentViews(c *gin.Context, contentType string, excludeID uint, limit int) []map[string]interface{} {
	if limit <= 0 {
		return nil
	}
	items, err := content.NewQuery(content.ScopedDB(c, b.App.Database())).
		Type(contentType).
		Published().
		OrderBy("published_at", "DESC").
		Limit(limit + 1).
		Get()
	if err != nil {
		return nil
	}
	filtered := make([]content.Content, 0, limit)
	for _, item := range items {
		if item.ID == excludeID {
			continue
		}
		filtered = append(filtered, item)
		if len(filtered) == limit {
			break
		}
	}
	return b.contentViews(c, filtered)
}

func (b *BaseTheme) recentPostViews(c *gin.Context, limit int) []map[string]interface{} {
	if limit <= 0 || b.App == nil || b.App.Database() == nil {
		return nil
	}
	items, err := content.NewQuery(content.ScopedDB(c, b.App.Database())).
		Type("post").
		Published().
		OrderBy("published_at", "DESC").
		Limit(limit).
		Get()
	if err != nil {
		return nil
	}
	return b.contentViews(c, items)
}

func (b *BaseTheme) termViews(contentID uint, taxName string) []map[string]interface{} {
	if b.App == nil || b.App.TaxonomyRepo() == nil {
		return nil
	}
	items, _ := b.App.TaxonomyRepo().GetContentTaxonomies(contentID, taxName)
	return taxonomyViews(items)
}

func taxonomyViews(items []taxonomy.Taxonomy) []map[string]interface{} {
	views := make([]map[string]interface{}, len(items))
	for i, item := range items {
		views[i] = map[string]interface{}{
			"ID":   item.ID,
			"Name": item.Term.Name,
			"Slug": item.Term.Slug,
		}
	}
	return views
}

func (b *BaseTheme) addArchiveTaxonomyData(data gin.H) {
	if b.App == nil || b.App.TaxonomyRepo() == nil {
		return
	}
	if cats, err := b.App.TaxonomyRepo().ListByTaxonomy("category"); err == nil {
		data["Categories"] = taxonomyViews(cats)
	}
	if tags, err := b.App.TaxonomyRepo().ListByTaxonomy("tag"); err == nil {
		data["Tags"] = taxonomyViews(tags)
	}
}

func singularAlias(contentType string) string {
	return exportedKey(contentType)
}

func pluralAlias(contentType string) string {
	return exportedKey(pluralName(contentType))
}

func pluralName(name string) string {
	if strings.HasSuffix(name, "y") && len(name) > 1 {
		prev := name[len(name)-2]
		if !strings.ContainsRune("aeiou", rune(prev)) {
			return strings.TrimSuffix(name, "y") + "ies"
		}
	}
	if strings.HasSuffix(name, "s") || strings.HasSuffix(name, "x") || strings.HasSuffix(name, "ch") || strings.HasSuffix(name, "sh") {
		return name + "es"
	}
	return name + "s"
}

func exportedKey(key string) string {
	var b strings.Builder
	upperNext := true
	for _, r := range key {
		if r == '_' || r == '-' || r == ' ' {
			upperNext = true
			continue
		}
		if upperNext {
			b.WriteRune(unicode.ToUpper(r))
			upperNext = false
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func addLegacyListAliases(data gin.H, items []map[string]interface{}) {
	// Compatibility for bundled themes that predate dynamic content rendering.
	// These aliases do not imply required content types; they all point to the
	// current archive result so old page templates can keep working while new
	// themes use Items or the type-derived alias.
	for _, key := range []string{"Products", "Services", "Showcases", "Posts", "Articles", "Updates", "Analyses", "MarketUpdates", "LatestAnalysis"} {
		if _, exists := data[key]; !exists {
			data[key] = items
		}
	}
}

func addLegacySingleAliases(data gin.H, item map[string]interface{}) {
	for _, key := range []string{"Product", "Service", "Showcase", "Post", "Article", "MarketUpdate", "Analysis"} {
		if _, exists := data[key]; !exists {
			data[key] = item
		}
	}
}

// ApplySiteOptionOverrides reconciles SEO metadata with admin-managed
// site options. SEOBuilder is constructed once at boot from cfg.Site.Name,
// so generated titles may contain the static config value. When the admin
// updates site_name at runtime, only the site-name portion should change:
// archive/content titles keep their page-specific prefix. Empty descriptions
// are filled from site_description so meta tags are never blank when an admin
// value is available. Exported so themes with their own custom render paths
// can reuse the same logic.
func ApplySiteOptionOverrides(app App, seo *rewrite.SEOMeta) {
	if app == nil {
		return
	}
	ApplySiteOptionOverridesFromOptions(app.OptionsStore(), app.SEOBuilder(), seo)
}

// ApplySiteOptionOverridesFromOptions applies runtime site options to SEO
// metadata. It is exported for themes with custom PageService render paths so
// they do not need to duplicate core SEO override logic.
func ApplySiteOptionOverridesFromOptions(opts interface{ Get(string) string }, builder *rewrite.SEOBuilder, seo *rewrite.SEOMeta) {
	if opts == nil || seo == nil {
		return
	}
	if name := strings.TrimSpace(opts.Get("site_name")); name != "" {
		seo.Title = replaceSEOTitleSiteName(seo.Title, builder.SiteName(), name)
		seo.OGTitle = replaceSEOTitleSiteName(seo.OGTitle, builder.SiteName(), name)
	}
	if seo.Description == "" {
		if d := opts.Get("site_description"); d != "" {
			seo.Description = d
			if seo.OGDescription == "" {
				seo.OGDescription = d
			}
		}
	}
	// site_icon: system setting always wins when non-empty; otherwise leaves
	// whatever the theme may have set, falling back to no favicon.
	if icon := strings.TrimSpace(opts.Get("site_icon")); icon != "" {
		seo.SiteIcon = icon
	}
}

// LocalizedArchiveTitle returns the current-language presentation title for a
// content archive. Themes can set archive_title_key in theme.toml; otherwise
// core tries stable generic message IDs derived from rewrite/template/type
// names before falling back to the configured label_plural.
func LocalizedArchiveTitle(c *gin.Context, mgr *coreI18n.Manager, typeDef *content.ContentTypeDef) string {
	if typeDef == nil {
		return ""
	}
	fallback := typeDef.LabelPlural
	if fallback == "" {
		fallback = typeDef.Label
	}
	if mgr == nil || c == nil {
		return fallback
	}
	for _, key := range archiveTitleKeys(typeDef) {
		if key == "" {
			continue
		}
		if title := mgr.Translate(c, key); title != "" && title != key {
			return title
		}
	}
	return fallback
}

// LocalizedContentTypeLabel returns the current-language singular display label
// for a content type. Theme locale files can provide content_type.<name>; when
// absent, the registry label remains the source of truth.
func LocalizedContentTypeLabel(c *gin.Context, mgr *coreI18n.Manager, typeDef *content.ContentTypeDef) string {
	if typeDef == nil {
		return ""
	}
	fallback := typeDef.Label
	if fallback == "" {
		fallback = typeDef.Name
	}
	if mgr == nil || c == nil || typeDef.Name == "" {
		return fallback
	}
	key := "content_type." + typeDef.Name
	label := mgr.Translate(c, key)
	if label != "" && label != key {
		return label
	}
	return fallback
}

func archiveTitleKeys(typeDef *content.ContentTypeDef) []string {
	keys := []string{typeDef.ArchiveTitleKey}
	if slug := normalizeMessageKeyPart(typeDef.Rewrite.Slug); slug != "" {
		keys = append(keys, "page_title_"+slug)
	}
	if tmpl := normalizeMessageKeyPart(typeDef.Templates.Archive); tmpl != "" {
		keys = append(keys, "page_title_"+tmpl)
	}
	if name := normalizeMessageKeyPart(typeDef.Name); name != "" {
		keys = append(keys, "page_title_"+name)
	}
	return keys
}

func normalizeMessageKeyPart(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		isWord := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isWord {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func replaceSEOTitleSiteName(title, oldName, newName string) string {
	title = strings.TrimSpace(title)
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if title == "" || oldName == "" || newName == "" {
		return title
	}
	if title == oldName {
		return newName
	}
	for _, sep := range []string{" | ", " - "} {
		suffix := sep + oldName
		if strings.HasSuffix(title, suffix) {
			pageTitle := strings.TrimSpace(strings.TrimSuffix(title, suffix))
			if pageTitle == "" {
				return newName
			}
			return pageTitle + sep + newName
		}
	}
	return title
}

// ApplyContentMetaSEO runs the SEOContentMeta filter so plugins can override
// fields on a content single-page SEOMeta from per-content meta values
// (e.g. seo-extras reads seo_title / seo_description / seo_image / seo_robots
// from gp_content_meta and patches the meta accordingly). Loads content meta
// once and forwards both the item and the meta map to the filter. No-op when
// no plugin registers the filter, so default behavior is unchanged.
//
// Signature takes the hook bus and content repository directly (rather than
// the App interface) so themes with custom render paths can call it without
// implementing App — e.g. modern-company's PageService just stores these two
// references alongside its other engine handles.
func ApplyContentMetaSEO(hookBus *hook.Bus, contentRepo *content.Repository, seo *rewrite.SEOMeta, item *content.Content) {
	if hookBus == nil || seo == nil || item == nil {
		return
	}
	var metaMap map[string]string
	if contentRepo != nil {
		metaMap, _ = contentRepo.GetMeta(item.ID)
	}
	result := hookBus.ApplyFilter(hook.SEOContentMeta, *seo, item, metaMap)
	if updated, ok := result.(rewrite.SEOMeta); ok {
		*seo = updated
	}
}

func isLanguagePrefixableURL(raw string) bool {
	u := strings.TrimSpace(raw)
	if u == "" || strings.HasPrefix(u, "#") || strings.HasPrefix(u, "?") || strings.HasPrefix(u, "//") {
		return false
	}
	parsed, err := url.Parse(u)
	if err == nil && parsed.Scheme != "" {
		return false
	}
	return true
}
