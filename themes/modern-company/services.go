package moderncompany

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go-press/core"
	"go-press/core/content"
	"go-press/core/hook"
	coreI18n "go-press/core/i18n"
	"go-press/core/option"
	"go-press/core/rewrite"
	"go-press/core/taxonomy"
	coreTheme "go-press/core/theme"
	"go-press/pkg/dbprefix"
)

// ======== View Models ========
// These match the field names expected by templates, so template files need zero changes.

// PageData is the base data shared by all pages.
type PageData struct {
	Ctx         *gin.Context `json:"-"`
	Title       string
	ActivePage  string
	Settings    map[string]string
	RecentPosts []PostView
	// SEO carries per-page SEO metadata for the seoHeadFor template helper.
	// Populated by PageService when the engine is available; left as the zero
	// value for static pages with no SEO model (about / contact), in which
	// case the layout falls back to a plain meta description.
	SEO rewrite.SEOMeta
}

// SetCtx injects the gin.Context so templates can use {{T .Ctx "key"}}.
func (p *PageData) SetCtx(c *gin.Context) { p.Ctx = c }

// TranslateSettings replaces translatable option values with translated versions
// for the current request language.
func (p *PageData) TranslateSettings(c *gin.Context, mgr *coreI18n.Manager) {
	p.Settings = mgr.TranslateSettings(c, p.Settings, option.IsTranslatable, option.AllTranslatableKeys())
}

type ProductView struct {
	ID            uint
	Title         string
	Slug          string
	Description   string
	Excerpt       string
	ImageURL      string
	GalleryImages []string
	SortOrder     int
}

type ServiceView struct {
	ID          uint
	Title       string
	Slug        string
	Description string
	Excerpt     string
	ImageURL    string
	SortOrder   int
}

type ShowcaseView struct {
	ID          uint
	Title       string
	Slug        string
	Description string
	ImageURL    string
	Client      string
	Location    string
	SortOrder   int
}

type CategoryView struct {
	ID   uint
	Name string
	Slug string
}

type TagView struct {
	ID   uint
	Name string
	Slug string
}

type PostView struct {
	ID          uint
	Title       string
	Slug        string
	Content     string
	Excerpt     string
	ImageURL    string
	Category    CategoryView
	Tags        []TagView
	PublishedAt *time.Time
	CreatedAt   time.Time
}

// ======== Page Data Structs ========

type HomeData struct {
	PageData
	Products []ProductView
	Services []ServiceView
	Partners []PartnerView
	// PartnersTop / PartnersBottom split the partner list for the two-row
	// marquee that kicks in when the visible count exceeds 12. Templates pick
	// based on len(Partners): ≤6 static grid, 7–12 single-row marquee, ≥13
	// two-row marquee using these halves.
	PartnersTop    []PartnerView
	PartnersBottom []PartnerView
}

// PartnerView represents one logo on the homepage "Trusted by" strip. The slot
// list comes from theme settings (home_partners_<i>_image / _name / _link).
type PartnerView struct {
	ImageURL string
	Name     string
	Link     string
}

type AboutData struct {
	PageData
}

type ProductsData struct {
	PageData
	Products []ProductView
}

type ServicesData struct {
	PageData
	Services []ServiceView
	Tags     []TagView
}

type ShowcaseData struct {
	PageData
	Showcases []ShowcaseView
}

type BlogData struct {
	PageData
	Posts      []PostView
	Categories []CategoryView
	Tags       []TagView
	ActiveCat  string
}

type ContactData struct {
	PageData
	Success bool
	Error   string
}

// ======== Detail Page Data Structs ========

type ProductDetailData struct {
	PageData
	Product ProductView
	Related []ProductView
	Tags    []TagView
}

type ServiceDetailData struct {
	PageData
	Service ServiceView
	Related []ServiceView
	Tags    []TagView
}

type ShowcaseDetailData struct {
	PageData
	Showcase ShowcaseView
	Related  []ShowcaseView
	Tags     []TagView
}

type PostDetailData struct {
	PageData
	Post       PostView
	Related    []PostView
	Categories []CategoryView
	Tags       []TagView
}

// TaxonomyArchiveItem represents a single content item in a taxonomy archive page (cross-type).
type TaxonomyArchiveItem struct {
	ID          uint
	Title       string
	Slug        string
	Excerpt     string
	ImageURL    string
	ContentType string // "product", "service", "showcase", "post"
	TypeLabel   string // localized display label, e.g. product/service/showcase/post
	DetailURL   string // full URL to the detail page
	PublishedAt *time.Time
	CreatedAt   time.Time
}

// TaxonomyArchiveData is the view data for /category/{slug} and /tag/{slug} pages.
type TaxonomyArchiveData struct {
	PageData
	TaxonomyType string // "category" or "tag"
	TermName     string
	TermSlug     string
	Items        []TaxonomyArchiveItem
	Total        int
}

// ======== PageService ========

// PageService assembles page data using the GoPress core engine.
type PageService struct {
	db          *gorm.DB
	contentRepo *content.Repository
	taxRepo     *taxonomy.Repository
	options     *option.Store
	// seoBuilder, registry and hookBus are populated when constructed from a
	// full Engine. They may be nil under NewPageServiceDB (CLI / tests), in
	// which case the SEO helpers gracefully return zero-value SEOMeta and the
	// per-content meta filter is skipped.
	seoBuilder *rewrite.SEOBuilder
	registry   *content.Registry
	hookBus    *hook.Bus
	i18nMgr    *coreI18n.Manager
	// reqCtx is set by ForRequest(c). It is needed by detail-page lookups
	// (FindBySlugScoped) so per-language slug disambiguation works. Nil for
	// non-request usages (CLI / tests) — scoped APIs treat nil as "no scope".
	reqCtx *gin.Context
}

// NewPageService creates a PageService backed by the full Engine.
func NewPageService(engine *core.Engine) *PageService {
	return &PageService{
		db:          engine.DB,
		contentRepo: engine.Content,
		taxRepo:     engine.Taxonomy,
		options:     engine.Options,
		seoBuilder:  engine.SEO,
		registry:    engine.Registry,
		hookBus:     engine.Hooks,
		i18nMgr:     engine.I18n,
	}
}

// NewPageServiceDB creates a PageService backed only by a DB connection.
func NewPageServiceDB(db *gorm.DB) *PageService {
	return &PageService{
		db:          db,
		contentRepo: content.NewRepository(db),
		taxRepo:     taxonomy.NewRepository(db),
		options:     option.NewStore(db),
	}
}

// ======== SEO Helpers ========
//
// These mirror BaseTheme's SEO injection: build per-page SEOMeta via the core
// SEOBuilder, then apply runtime option overrides so admin's site_name /
// site_description always win over the static cfg.Site.Name baked into the
// builder. Returns zero-value SEOMeta when the engine isn't wired in (DB-only
// service used by tests / CLI), which the template's seoHeadFor helper treats
// as "no SEO" and falls back to a plain meta description.

func (s *PageService) buildHomeSEO() rewrite.SEOMeta {
	if s.seoBuilder == nil {
		return rewrite.SEOMeta{}
	}
	seo := s.seoBuilder.ForHome(s.options.Get("site_description"))
	s.applySEOOverrides(&seo)
	return seo
}

func (s *PageService) buildArchiveSEO(typeName string) rewrite.SEOMeta {
	if s.seoBuilder == nil || s.registry == nil {
		return rewrite.SEOMeta{}
	}
	typeDef := s.registry.GetType(typeName)
	if typeDef == nil {
		return rewrite.SEOMeta{}
	}
	seo := s.seoBuilder.ForArchiveTitle(typeDef, coreTheme.LocalizedArchiveTitle(s.reqCtx, s.i18nMgr, typeDef))
	s.applySEOOverrides(&seo)
	return seo
}

func (s *PageService) buildContentSEO(item *content.Content, typeName string) rewrite.SEOMeta {
	if s.seoBuilder == nil || s.registry == nil || item == nil {
		return rewrite.SEOMeta{}
	}
	typeDef := s.registry.GetType(typeName)
	if typeDef == nil {
		return rewrite.SEOMeta{}
	}
	seo := s.seoBuilder.ForContent(item, typeDef)
	s.applySEOOverrides(&seo)
	coreTheme.ApplyContentMetaSEO(s.hookBus, s.contentRepo, &seo, item)
	return seo
}

// applySEOOverrides reconciles SEOBuilder output (which uses the static
// cfg.Site.Name baked in at boot) with runtime admin options (site_name,
// site_description). Mirrors core/theme.ApplySiteOptionOverrides so the two
// render paths produce identical SEO output.
func (s *PageService) applySEOOverrides(seo *rewrite.SEOMeta) {
	coreTheme.ApplySiteOptionOverridesFromOptions(s.options, s.seoBuilder, seo)
}

// ForRequest returns a clone of PageService with request-scoped content filters applied.
// Core plugins can register content scopes (e.g. language filtering) via content.AddContentScope.
// The clone also carries the gin.Context so per-row lookups (FindBySlugScoped) can
// honor the same scopes — critical for WPML-style same-slug-across-languages routing.
// This is a core pattern — no plugin-specific logic here.
func (s *PageService) ForRequest(c *gin.Context) *PageService {
	if c == nil {
		return s
	}
	scopedDB := content.ScopedDB(c, s.db)
	clone := *s
	clone.reqCtx = c
	if scopedDB != s.db {
		clone.db = scopedDB
	}
	return &clone
}

// ======== Helpers ========

func (s *PageService) getSettings() map[string]string {
	return s.options.All()
}

func (s *PageService) getRecentPosts(n int) []PostView {
	posts, _ := content.NewQuery(s.db).
		Type("post").Published().
		OrderBy("published_at", "DESC").
		Limit(n).Get()
	views := make([]PostView, len(posts))
	for i, c := range posts {
		views[i] = toPostView(c)
	}
	return views
}

func (s *PageService) buildPageData(title, activePage string) PageData {
	return PageData{
		Title:       title,
		ActivePage:  activePage,
		Settings:    s.getSettings(),
		RecentPosts: s.getRecentPosts(3),
	}
}

// ======== Page Data Methods ========

func (s *PageService) GetHomeData() (*HomeData, error) {
	products, err := s.getContentList("product", "sort_order", "ASC")
	if err != nil {
		return nil, err
	}
	services, err := s.getContentList("service", "sort_order", "ASC")
	if err != nil {
		return nil, err
	}

	// Limit products/services based on homepage settings
	pViews := toProductViews(products)
	sViews := toServiceViews(services)
	if maxStr := s.options.Get("home_products_max"); maxStr != "" {
		if max, err := strconv.Atoi(maxStr); err == nil && max > 0 && max < len(pViews) {
			pViews = pViews[:max]
		}
	}
	if maxStr := s.options.Get("home_services_max"); maxStr != "" {
		if max, err := strconv.Atoi(maxStr); err == nil && max > 0 && max < len(sViews) {
			sViews = sViews[:max]
		}
	}

	// Build the partners strip from settings: 24 numbered slots, skip empties.
	const partnerSlots = 24
	partners := make([]PartnerView, 0, partnerSlots)
	for i := 1; i <= partnerSlots; i++ {
		img := strings.TrimSpace(s.options.Get("home_partners_" + strconv.Itoa(i) + "_image"))
		if img == "" {
			continue
		}
		partners = append(partners, PartnerView{
			ImageURL: img,
			Name:     strings.TrimSpace(s.options.Get("home_partners_" + strconv.Itoa(i) + "_name")),
			Link:     strings.TrimSpace(s.options.Get("home_partners_" + strconv.Itoa(i) + "_link")),
		})
	}
	if maxStr := s.options.Get("home_partners_max"); maxStr != "" {
		if max, err := strconv.Atoi(maxStr); err == nil && max > 0 && max < len(partners) {
			partners = partners[:max]
		}
	}

	// Split for two-row marquee when there are enough logos. Top row gets the
	// ceiling half so an odd count puts the extra logo on top.
	var partnersTop, partnersBottom []PartnerView
	if len(partners) > 12 {
		half := (len(partners) + 1) / 2
		partnersTop = partners[:half]
		partnersBottom = partners[half:]
	}

	data := &HomeData{
		PageData:       s.buildPageData("Home", "home"),
		Products:       pViews,
		Services:       sViews,
		Partners:       partners,
		PartnersTop:    partnersTop,
		PartnersBottom: partnersBottom,
	}
	data.SEO = s.buildHomeSEO()
	return data, nil
}

func (s *PageService) GetAboutData() (*AboutData, error) {
	return &AboutData{
		PageData: s.buildPageData("About Us", "about"),
	}, nil
}

func (s *PageService) GetProductsData() (*ProductsData, error) {
	products, err := s.getContentList("product", "sort_order", "ASC")
	if err != nil {
		return nil, err
	}
	data := &ProductsData{
		PageData: s.buildPageData("Products", "products"),
		Products: toProductViews(products),
	}
	data.SEO = s.buildArchiveSEO("product")
	return data, nil
}

func (s *PageService) GetServicesData() (*ServicesData, error) {
	services, err := s.getContentList("service", "sort_order", "ASC")
	if err != nil {
		return nil, err
	}
	allTags, _ := s.taxRepo.ListByTaxonomy("tag")
	data := &ServicesData{
		PageData: s.buildPageData("Services", "services"),
		Services: toServiceViews(services),
		Tags:     toTagViews(allTags),
	}
	data.SEO = s.buildArchiveSEO("service")
	return data, nil
}

func (s *PageService) GetShowcaseData() (*ShowcaseData, error) {
	items, err := s.getContentList("showcase", "sort_order", "ASC")
	if err != nil {
		return nil, err
	}
	// Load meta (client, location) for each showcase
	views := make([]ShowcaseView, len(items))
	for i, c := range items {
		meta, _ := s.contentRepo.GetMeta(c.ID)
		views[i] = ShowcaseView{
			ID:          c.ID,
			Title:       c.Title,
			Slug:        c.Slug,
			Description: c.Content,
			ImageURL:    c.ImageURL,
			Client:      meta["client"],
			Location:    meta["location"],
			SortOrder:   c.SortOrder,
		}
	}
	data := &ShowcaseData{
		PageData:  s.buildPageData("Showcase", "showcase"),
		Showcases: views,
	}
	data.SEO = s.buildArchiveSEO("showcase")
	return data, nil
}

func (s *PageService) GetBlogData(categorySlug string) (*BlogData, error) {
	q := content.NewQuery(s.db).
		Type("post").Published().
		OrderBy("published_at", "DESC")

	if categorySlug != "" {
		q = q.Taxonomy("category", categorySlug)
	}

	posts, err := q.Get()
	if err != nil {
		return nil, err
	}

	// Load taxonomy info for each post
	postViews := make([]PostView, len(posts))
	for i, p := range posts {
		pv := toPostView(p)
		cats, _ := s.taxRepo.GetContentTaxonomies(p.ID, "category")
		if len(cats) > 0 {
			pv.Category = toCategoryView(cats[0])
		}
		tags, _ := s.taxRepo.GetContentTaxonomies(p.ID, "tag")
		pv.Tags = toTagViews(tags)
		postViews[i] = pv
	}

	allCats, _ := s.taxRepo.ListByTaxonomy("category")
	allTags, _ := s.taxRepo.ListByTaxonomy("tag")

	data := &BlogData{
		PageData:   s.buildPageData("Blog", "blog"),
		Posts:      postViews,
		Categories: toCategoryViews(allCats),
		Tags:       toTagViews(allTags),
		ActiveCat:  categorySlug,
	}
	data.SEO = s.buildArchiveSEO("post")
	return data, nil
}

func (s *PageService) GetContactData() (*ContactData, error) {
	return &ContactData{
		PageData: s.buildPageData("Contact", "contact"),
	}, nil
}

// ======== Detail Page Data Methods ========

func (s *PageService) GetProductDetail(slug string) (*ProductDetailData, error) {
	item, err := s.contentRepo.FindBySlugScoped(s.reqCtx, "product", slug)
	if err != nil || item == nil {
		return nil, err
	}

	// Load gallery images from meta
	meta, _ := s.contentRepo.GetMeta(item.ID)
	var gallery []string
	if raw := meta["gallery_images"]; raw != "" {
		for _, u := range strings.Split(raw, ",") {
			u = strings.TrimSpace(u)
			if u != "" {
				gallery = append(gallery, u)
			}
		}
	}

	// Get related products (all except current)
	all, _ := s.getContentList("product", "sort_order", "ASC")
	var related []ProductView
	for _, c := range all {
		if c.Slug != slug {
			related = append(related, ProductView{
				ID: c.ID, Title: c.Title, Slug: c.Slug,
				Description: c.Content, Excerpt: c.Excerpt,
				ImageURL: c.ImageURL, SortOrder: c.SortOrder,
			})
		}
	}
	if len(related) > 3 {
		related = related[:3]
	}
	// Load tags for this product
	tagItems, _ := s.taxRepo.GetContentTaxonomies(item.ID, "tag")

	data := &ProductDetailData{
		PageData: s.buildPageData(item.Title, "products"),
		Product: ProductView{
			ID: item.ID, Title: item.Title, Slug: item.Slug,
			Description: item.Content, Excerpt: item.Excerpt,
			ImageURL: item.ImageURL, GalleryImages: gallery,
			SortOrder: item.SortOrder,
		},
		Related: related,
		Tags:    toTagViews(tagItems),
	}
	data.SEO = s.buildContentSEO(item, "product")
	return data, nil
}

func (s *PageService) GetServiceDetail(slug string) (*ServiceDetailData, error) {
	item, err := s.contentRepo.FindBySlugScoped(s.reqCtx, "service", slug)
	if err != nil || item == nil {
		return nil, err
	}
	all, _ := s.getContentList("service", "sort_order", "ASC")
	var related []ServiceView
	for _, c := range all {
		if c.Slug != slug {
			related = append(related, ServiceView{
				ID: c.ID, Title: c.Title, Slug: c.Slug,
				Description: c.Content, Excerpt: c.Excerpt,
				ImageURL: c.ImageURL, SortOrder: c.SortOrder,
			})
		}
	}
	if len(related) > 3 {
		related = related[:3]
	}
	tagItems, _ := s.taxRepo.GetContentTaxonomies(item.ID, "tag")
	data := &ServiceDetailData{
		PageData: s.buildPageData(item.Title, "services"),
		Service: ServiceView{
			ID: item.ID, Title: item.Title, Slug: item.Slug,
			Description: item.Content, Excerpt: item.Excerpt,
			ImageURL: item.ImageURL, SortOrder: item.SortOrder,
		},
		Related: related,
		Tags:    toTagViews(tagItems),
	}
	data.SEO = s.buildContentSEO(item, "service")
	return data, nil
}

func (s *PageService) GetShowcaseDetail(slug string) (*ShowcaseDetailData, error) {
	item, err := s.contentRepo.FindBySlugScoped(s.reqCtx, "showcase", slug)
	if err != nil || item == nil {
		return nil, err
	}
	meta, _ := s.contentRepo.GetMeta(item.ID)
	// Get related showcases
	all, _ := s.getContentList("showcase", "sort_order", "ASC")
	var related []ShowcaseView
	for _, c := range all {
		if c.Slug != slug {
			m, _ := s.contentRepo.GetMeta(c.ID)
			related = append(related, ShowcaseView{
				ID: c.ID, Title: c.Title, Slug: c.Slug,
				Description: c.Content, ImageURL: c.ImageURL,
				Client: m["client"], Location: m["location"],
				SortOrder: c.SortOrder,
			})
		}
	}
	if len(related) > 3 {
		related = related[:3]
	}
	tagItems, _ := s.taxRepo.GetContentTaxonomies(item.ID, "tag")
	data := &ShowcaseDetailData{
		PageData: s.buildPageData(item.Title, "showcase"),
		Showcase: ShowcaseView{
			ID: item.ID, Title: item.Title, Slug: item.Slug,
			Description: item.Content, ImageURL: item.ImageURL,
			Client: meta["client"], Location: meta["location"],
		},
		Related: related,
		Tags:    toTagViews(tagItems),
	}
	data.SEO = s.buildContentSEO(item, "showcase")
	return data, nil
}

func (s *PageService) GetPostDetail(slug string) (*PostDetailData, error) {
	item, err := s.contentRepo.FindBySlugScoped(s.reqCtx, "post", slug)
	if err != nil || item == nil {
		return nil, err
	}
	pv := toPostView(*item)
	cats, _ := s.taxRepo.GetContentTaxonomies(item.ID, "category")
	if len(cats) > 0 {
		pv.Category = toCategoryView(cats[0])
	}
	tagItems, _ := s.taxRepo.GetContentTaxonomies(item.ID, "tag")
	pv.Tags = toTagViews(tagItems)

	// Related posts (same category if available)
	q := content.NewQuery(s.db).Type("post").Published().OrderBy("published_at", "DESC").Limit(4)
	if len(cats) > 0 {
		q = q.Taxonomy("category", cats[0].Term.Slug)
	}
	relatedPosts, _ := q.Get()
	var related []PostView
	for _, p := range relatedPosts {
		if p.Slug != slug {
			rv := toPostView(p)
			pc, _ := s.taxRepo.GetContentTaxonomies(p.ID, "category")
			if len(pc) > 0 {
				rv.Category = toCategoryView(pc[0])
			}
			related = append(related, rv)
		}
	}
	if len(related) > 3 {
		related = related[:3]
	}

	allCats, _ := s.taxRepo.ListByTaxonomy("category")
	allTags, _ := s.taxRepo.ListByTaxonomy("tag")

	data := &PostDetailData{
		PageData:   s.buildPageData(item.Title, "blog"),
		Post:       pv,
		Related:    related,
		Categories: toCategoryViews(allCats),
		Tags:       toTagViews(allTags),
	}
	data.SEO = s.buildContentSEO(item, "post")
	return data, nil
}

// GetTaxonomyArchive loads all content (across all content types) that belongs to a given term.
// taxonomyType is "category" or "tag", termSlug is the term's URL slug.
func (s *PageService) GetTaxonomyArchive(taxonomyType, termSlug string) (*TaxonomyArchiveData, error) {
	// Look up the term to get its display name
	term, err := s.taxRepo.GetTermBySlug(termSlug)
	if err != nil {
		return nil, err
	}

	// Query all published content that has this taxonomy term, across all types
	q := content.NewQuery(s.db).
		Status(content.StatusPublished).
		Taxonomy(taxonomyType, termSlug).
		OrderBy(dbprefix.Table("contents")+".created_at", "DESC")

	items, err := q.Get()
	if err != nil {
		return nil, err
	}

	// Type label map
	typeLabels := map[string]string{
		"product":  "Product",
		"service":  "Service",
		"showcase": "Project",
		"post":     "Article",
	}
	// Type → rewrite slug map for building detail URLs
	typeRewrite := map[string]string{
		"product":  "products",
		"service":  "services",
		"showcase": "showcase",
		"post":     "blog",
	}

	archiveItems := make([]TaxonomyArchiveItem, len(items))
	for i, c := range items {
		rewrite := typeRewrite[c.Type]
		if rewrite == "" {
			rewrite = c.Type
		}
		excerpt := c.Excerpt
		if excerpt == "" && len(c.Content) > 200 {
			excerpt = stripHTMLTags(c.Content[:200]) + "..."
		} else if excerpt == "" {
			excerpt = stripHTMLTags(c.Content)
		}
		archiveItems[i] = TaxonomyArchiveItem{
			ID:          c.ID,
			Title:       c.Title,
			Slug:        c.Slug,
			Excerpt:     excerpt,
			ImageURL:    c.ImageURL,
			ContentType: c.Type,
			TypeLabel:   typeLabels[c.Type],
			DetailURL:   "/" + rewrite + "/" + c.Slug,
			PublishedAt: c.PublishedAt,
			CreatedAt:   c.CreatedAt,
		}
	}

	// Build page title
	taxLabel := "分类"
	if taxonomyType == "tag" {
		taxLabel = "标签"
	}
	pageTitle := taxLabel + ": " + term.Name

	return &TaxonomyArchiveData{
		PageData:     s.buildPageData(pageTitle, ""),
		TaxonomyType: taxonomyType,
		TermName:     term.Name,
		TermSlug:     termSlug,
		Items:        archiveItems,
		Total:        len(archiveItems),
	}, nil
}

// stripHTMLTags removes HTML tags from a string.
func stripHTMLTags(s string) string {
	return strings.TrimSpace(reHTMLTags.ReplaceAllString(s, " "))
}

// SubmitContact saves a contact message as a Content entry with meta.
func (s *PageService) SubmitContact(name, email, phone, message string) error {
	c := &content.Content{
		Type:    "contact_message",
		Status:  content.StatusDraft,
		Title:   name,
		Content: message,
	}
	if err := s.contentRepo.Create(c); err != nil {
		return err
	}
	_ = s.contentRepo.SaveMeta(c.ID, "email", email)
	if phone != "" {
		_ = s.contentRepo.SaveMeta(c.ID, "phone", phone)
	}
	return nil
}

// ======== Internal Helpers ========

func (s *PageService) getContentList(contentType, orderField, orderDir string) ([]content.Content, error) {
	return content.NewQuery(s.db).
		Type(contentType).
		Status(content.StatusPublished).
		OrderBy(orderField, orderDir).
		Get()
}

// ======== Model Converters ========

func toPostView(c content.Content) PostView {
	return PostView{
		ID:          c.ID,
		Title:       c.Title,
		Slug:        c.Slug,
		Content:     c.Content,
		Excerpt:     c.Excerpt,
		ImageURL:    c.ImageURL,
		PublishedAt: c.PublishedAt,
		CreatedAt:   c.CreatedAt,
	}
}

func toProductViews(items []content.Content) []ProductView {
	views := make([]ProductView, len(items))
	for i, c := range items {
		views[i] = ProductView{
			ID:          c.ID,
			Title:       c.Title,
			Slug:        c.Slug,
			Description: c.Content,
			Excerpt:     c.Excerpt,
			ImageURL:    c.ImageURL,
			SortOrder:   c.SortOrder,
		}
	}
	return views
}

func toServiceViews(items []content.Content) []ServiceView {
	views := make([]ServiceView, len(items))
	for i, c := range items {
		views[i] = ServiceView{
			ID:          c.ID,
			Title:       c.Title,
			Slug:        c.Slug,
			Description: c.Content,
			Excerpt:     c.Excerpt,
			ImageURL:    c.ImageURL,
			SortOrder:   c.SortOrder,
		}
	}
	return views
}

func toCategoryView(t taxonomy.Taxonomy) CategoryView {
	return CategoryView{ID: t.ID, Name: t.Term.Name, Slug: t.Term.Slug}
}

func toCategoryViews(items []taxonomy.Taxonomy) []CategoryView {
	views := make([]CategoryView, len(items))
	for i, t := range items {
		views[i] = toCategoryView(t)
	}
	return views
}

func toTagViews(items []taxonomy.Taxonomy) []TagView {
	views := make([]TagView, len(items))
	for i, t := range items {
		views[i] = TagView{ID: t.ID, Name: t.Term.Name, Slug: t.Term.Slug}
	}
	return views
}
