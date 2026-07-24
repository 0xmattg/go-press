package financialnews

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go-press/core"
	"go-press/core/content"
	coreI18n "go-press/core/i18n"
	"go-press/core/option"
	"go-press/core/rewrite"
	"go-press/core/taxonomy"
	coreTheme "go-press/core/theme"
)

// ======== View Models ========

type PageData struct {
	Ctx        *gin.Context `json:"-"`
	Title      string
	ActivePage string
	Settings   map[string]string
	LatestNews []ArticleView
	// SEO carries per-page SEO metadata for the seoHeadFor template helper.
	// Populated by PageService when the engine is available; left as the zero
	// value for static pages with no SEO model (about), in which case the
	// layout falls back to a plain meta description.
	SEO rewrite.SEOMeta
}

// SetCtx injects the gin.Context so templates can use {{T .Ctx "key"}}.
func (p *PageData) SetCtx(c *gin.Context) { p.Ctx = c }

// TranslateSettings replaces translatable option values with translated versions
// for the current request language.
func (p *PageData) TranslateSettings(c *gin.Context, mgr *coreI18n.Manager) {
	p.Settings = mgr.TranslateSettings(c, p.Settings, option.IsTranslatable, option.AllTranslatableKeys())
}

type ArticleView struct {
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

type MarketUpdateView struct {
	ID          uint
	Title       string
	Content     string
	Ticker      string
	PriceChange string
	Market      string
	PublishedAt *time.Time
}

type AnalysisView struct {
	ID          uint
	Title       string
	Slug        string
	Content     string
	Excerpt     string
	ImageURL    string
	Analyst     string
	Rating      string
	Category    CategoryView
	PublishedAt *time.Time
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

// ======== Page Data Structs ========

type HomeData struct {
	PageData
	FeaturedArticles []ArticleView
	MarketUpdates    []MarketUpdateView
	LatestAnalysis   []AnalysisView
}

type ArticlesData struct {
	PageData
	Articles   []ArticleView
	Categories []CategoryView
	Tags       []TagView
	ActiveCat  string
}

type MarketData struct {
	PageData
	Updates []MarketUpdateView
}

type AnalysisListData struct {
	PageData
	Analyses   []AnalysisView
	Categories []CategoryView
}

type AboutData struct {
	PageData
}

// ======== PageService ========

type PageService struct {
	coreTheme.SEOPageService
}

func NewPageService(engine *core.Engine) *PageService {
	return &PageService{coreTheme.NewSEOPageService(
		coreTheme.NewBasePageService(engine.DB, engine.Content, engine.Taxonomy, engine.Options),
		engine.SEO, engine.Registry, engine.Hooks, engine.I18n)}
}

func NewPageServiceDB(db *gorm.DB) *PageService {
	return &PageService{coreTheme.NewSEOPageService(coreTheme.NewBasePageServiceDB(db), nil, nil, nil, nil)}
}

// ForRequest returns a clone of PageService with request-scoped content filters applied.
// Core plugins can register content scopes (e.g. language filtering) via content.AddContentScope.
func (s *PageService) ForRequest(c *gin.Context) *PageService {
	clone := *s
	clone.BasePageService = s.BasePageService.ForRequest(c)
	return &clone
}

// ======== Helpers ========

func (s *PageService) getLatestNews(n int) []ArticleView {
	articles, _ := content.NewQuery(s.DB).
		Type("post").Published().
		OrderBy("published_at", "DESC").
		Limit(n).Get()
	views := make([]ArticleView, len(articles))
	for i, c := range articles {
		views[i] = toArticleView(c)
	}
	return views
}

func (s *PageService) buildPageData(title, activePage string) PageData {
	return PageData{
		Title:      title,
		ActivePage: activePage,
		Settings:   s.Settings(),
		LatestNews: s.getLatestNews(5),
	}
}

// ======== Page Data Methods ========

func (s *PageService) GetHomeData() (*HomeData, error) {
	featured, _ := content.NewQuery(s.DB).
		Type("post").Published().
		OrderBy("published_at", "DESC").
		Limit(6).Get()

	marketItems, _ := content.NewQuery(s.DB).
		Type("market_update").Published().
		OrderBy("published_at", "DESC").
		Limit(10).Get()

	analysisItems, _ := content.NewQuery(s.DB).
		Type("analysis").Published().
		OrderBy("published_at", "DESC").
		Limit(4).Get()

	data := &HomeData{
		PageData:         s.buildPageData("Financial News - 首页", "home"),
		FeaturedArticles: toArticleViews(featured),
		MarketUpdates:    s.toMarketUpdateViews(marketItems),
		LatestAnalysis:   s.toAnalysisViews(analysisItems),
	}
	data.SEO = s.BuildHomeSEO()
	return data, nil
}

func (s *PageService) GetArticlesData(categorySlug string) (*ArticlesData, error) {
	q := content.NewQuery(s.DB).
		Type("post").Published().
		OrderBy("published_at", "DESC")

	if categorySlug != "" {
		q = q.Taxonomy("category", categorySlug)
	}

	articles, err := q.Get()
	if err != nil {
		return nil, err
	}

	articleViews := make([]ArticleView, len(articles))
	for i, a := range articles {
		av := toArticleView(a)
		cats, _ := s.Tax.GetContentTaxonomies(a.ID, "category")
		if len(cats) > 0 {
			av.Category = toCategoryView(cats[0])
		}
		tags, _ := s.Tax.GetContentTaxonomies(a.ID, "tag")
		av.Tags = toTagViews(tags)
		articleViews[i] = av
	}

	allCats, _ := s.Tax.ListByTaxonomy("category")
	allTags, _ := s.Tax.ListByTaxonomy("tag")

	data := &ArticlesData{
		PageData:   s.buildPageData("新闻文章", "articles"),
		Articles:   articleViews,
		Categories: toCategoryViews(allCats),
		Tags:       toTagViews(allTags),
		ActiveCat:  categorySlug,
	}
	data.SEO = s.BuildArchiveSEO("post")
	return data, nil
}

func (s *PageService) GetMarketData() (*MarketData, error) {
	items, _ := content.NewQuery(s.DB).
		Type("market_update").Published().
		OrderBy("published_at", "DESC").
		Get()

	data := &MarketData{
		PageData: s.buildPageData("行情快讯", "market"),
		Updates:  s.toMarketUpdateViews(items),
	}
	data.SEO = s.BuildArchiveSEO("market_update")
	return data, nil
}

func (s *PageService) GetAnalysisData() (*AnalysisListData, error) {
	items, _ := content.NewQuery(s.DB).
		Type("analysis").Published().
		OrderBy("published_at", "DESC").
		Get()

	allCats, _ := s.Tax.ListByTaxonomy("category")

	data := &AnalysisListData{
		PageData:   s.buildPageData("深度分析", "analysis"),
		Analyses:   s.toAnalysisViews(items),
		Categories: toCategoryViews(allCats),
	}
	data.SEO = s.BuildArchiveSEO("analysis")
	return data, nil
}

func (s *PageService) GetAboutData() (*AboutData, error) {
	return &AboutData{
		PageData: s.buildPageData("关于我们", "about"),
	}, nil
}

// PostDetailData holds data for the single blog post page.
type PostDetailData struct {
	PageData
	Item        content.Content
	Categories  []CategoryView
	Tags        []TagView
	LatestPosts []ArticleView
}

func (s *PageService) GetPostDetailData(slug string) (*PostDetailData, error) {
	item, err := s.Content.FindBySlugScoped(s.ReqCtx, "post", slug)
	if err != nil || item == nil {
		return nil, fmt.Errorf("post %q not found", slug)
	}
	if item.Status != content.StatusPublished {
		return nil, fmt.Errorf("post %q not published", slug)
	}

	var categories []CategoryView
	var tags []TagView
	if s.Tax != nil {
		cats, _ := s.Tax.GetContentTaxonomies(item.ID, "category")
		categories = toCategoryViews(cats)
		tagItems, _ := s.Tax.GetContentTaxonomies(item.ID, "tag")
		tags = toTagViews(tagItems)
	}

	latestPosts := s.getLatestNews(5)

	data := &PostDetailData{
		PageData:    s.buildPageData(item.Title, "articles"),
		Item:        *item,
		Categories:  categories,
		Tags:        tags,
		LatestPosts: latestPosts,
	}
	data.SEO = s.BuildContentSEO(item, "post")
	return data, nil
}

// ======== Model Converters ========

func toArticleView(c content.Content) ArticleView {
	return ArticleView{
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

func toArticleViews(items []content.Content) []ArticleView {
	views := make([]ArticleView, len(items))
	for i, c := range items {
		views[i] = toArticleView(c)
	}
	return views
}

func (s *PageService) toMarketUpdateViews(items []content.Content) []MarketUpdateView {
	views := make([]MarketUpdateView, len(items))
	for i, c := range items {
		meta, _ := s.Content.GetMeta(c.ID)
		views[i] = MarketUpdateView{
			ID:          c.ID,
			Title:       c.Title,
			Content:     c.Content,
			Ticker:      meta["ticker"],
			PriceChange: meta["price_change"],
			Market:      meta["market"],
			PublishedAt: c.PublishedAt,
		}
	}
	return views
}

func (s *PageService) toAnalysisViews(items []content.Content) []AnalysisView {
	views := make([]AnalysisView, len(items))
	for i, c := range items {
		meta, _ := s.Content.GetMeta(c.ID)
		av := AnalysisView{
			ID:          c.ID,
			Title:       c.Title,
			Slug:        c.Slug,
			Content:     c.Content,
			Excerpt:     c.Excerpt,
			ImageURL:    c.ImageURL,
			Analyst:     meta["analyst"],
			Rating:      meta["rating"],
			PublishedAt: c.PublishedAt,
		}
		cats, _ := s.Tax.GetContentTaxonomies(c.ID, "category")
		if len(cats) > 0 {
			av.Category = toCategoryView(cats[0])
		}
		views[i] = av
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
