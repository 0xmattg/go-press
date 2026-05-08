package theme

import (
	"fmt"
	"html/template"
	"net/http"
	"reflect"
	"strings"

	"go-press/core/content"
	"go-press/core/hook"
	"go-press/core/menu"
	"go-press/core/rewrite"
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

// LoadTemplates compiles all templates using the core TemplateEngine
// with merged common + theme-specific function maps.
func (b *BaseTheme) LoadTemplates(t Theme) {
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
		rw := b.App.RewriteEngine()
		seo := b.App.SEOBuilder()
		if rw != nil {
			engineFuncs["buildURL"] = rw.BuildURL
			engineFuncs["archiveURL"] = rw.BuildArchiveURL
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
				output := hooks.ApplyFilter(name, template.HTML(""), data)
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

	data := b.buildBaseData("Home")

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
	candidates := ResolveTemplate(route.ContentType, "", "", "", true, false, false)
	tmpl := b.Templates.Resolve(candidates)

	// Query published content of this type
	page := route.Page
	if page < 1 {
		page = 1
	}
	perPage := 20

	result, err := content.NewQuery(b.App.Database()).
		Type(route.ContentType).Published().
		OrderBy("published_at", "DESC").
		Paginate(page, perPage)
	if err != nil {
		logger.Error("BaseTheme: archive query failed", "type", route.ContentType, "error", err)
		b.render404(c)
		return
	}

	typeDef := b.App.ContentRegistry().GetType(route.ContentType)

	data := b.buildBaseData("")
	if typeDef != nil {
		data["Title"] = typeDef.LabelPlural
	}
	data["ContentType"] = route.ContentType
	data["Items"] = result.Items
	data["Pagination"] = result

	// Inject SEO
	if b.App.SEOBuilder() != nil && typeDef != nil {
		seo := b.App.SEOBuilder().ForArchive(typeDef)
		ApplySiteOptionOverrides(b.App, &seo)
		data["SEO"] = seo
	}

	// If theme has a template, use it
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

	candidates := ResolveTemplate(route.ContentType, route.Slug, "", "", false, false, false)
	tmpl := b.Templates.Resolve(candidates)

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

	data := b.buildBaseData(item.Title)
	data["Item"] = item
	data["Meta"] = meta
	data["Categories"] = categories
	data["Tags"] = tags
	data["ContentType"] = route.ContentType

	// Inject SEO
	typeDef := b.App.ContentRegistry().GetType(route.ContentType)
	if b.App.SEOBuilder() != nil && typeDef != nil {
		seo := b.App.SEOBuilder().ForContent(item, typeDef)
		ApplySiteOptionOverrides(b.App, &seo)
		ApplyContentMetaSEO(b.App.HookBus(), b.App.ContentRepo(), &seo, item)
		data["SEO"] = seo
	}

	// If theme has a template, use it
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
	candidates := ResolveTemplate("", "", route.TaxSlug, route.TermSlug, false, false, false)
	tmpl := b.Templates.Resolve(candidates)

	items, err := content.NewQuery(b.App.Database()).
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

	data := b.buildBaseData(termName)
	data["TaxSlug"] = route.TaxSlug
	data["TermSlug"] = route.TermSlug
	data["TaxLabel"] = taxLabel
	data["TermName"] = termName
	data["Items"] = items

	// If theme has a template, use it (with "base" block)
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
	data := b.buildBaseData("Page Not Found")
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
func (b *BaseTheme) buildBaseData(title string) gin.H {
	data := gin.H{
		"Title": title,
	}
	if b.App != nil && b.App.OptionsStore() != nil {
		data["Settings"] = b.App.OptionsStore().All()
	}
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

// ApplySiteOptionOverrides reconciles SEO metadata with admin-managed
// site options. SEOBuilder is constructed once at boot from cfg.Site.Name,
// so its Title/OGTitle/JSON-LD reference the static config value. When the
// admin updates site_name or site_description at runtime, those should win
// over the SEOBuilder defaults; this helper applies that override and
// fills empty descriptions from site_description so meta tags are never
// blank when an admin value is available. Exported so themes with their
// own custom render paths (e.g. modern-company) can reuse the same logic.
func ApplySiteOptionOverrides(app App, seo *rewrite.SEOMeta) {
	if app == nil || seo == nil || app.OptionsStore() == nil {
		return
	}
	opts := app.OptionsStore()
	if name := opts.Get("site_name"); name != "" {
		if seo.OGType == "website" {
			seo.Title = name
			seo.OGTitle = name
		}
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
