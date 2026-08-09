package monojournal

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/0xmattg/go-press/core/comment"
	"github.com/0xmattg/go-press/core/content"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/rewrite"
	coreTheme "github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/core/user"
)

type PageData struct {
	Ctx        *gin.Context
	Title      string
	ActivePage string
	Settings   map[string]string
	SEO        rewrite.SEOMeta
}

func (p *PageData) SetCtx(c *gin.Context) { p.Ctx = c }

func (p *PageData) TranslateSettings(c *gin.Context, manager *coreI18n.Manager) {
	if manager == nil {
		return
	}
	p.Settings = manager.TranslateSettings(c, p.Settings, option.IsTranslatable, option.AllTranslatableKeys())
}

type TermView struct {
	Name  string
	Slug  string
	Count int64
}

type PostView struct {
	ID          uint
	Type        string
	Title       string
	Slug        string
	Content     string
	Excerpt     string
	ImageURL    string
	PublishedAt *time.Time
	CreatedAt   time.Time
	Category    TermView
	Tags        []TermView
}

type HomeData struct {
	PageData
	Featured   *PostView
	Posts      []PostView
	Categories []TermView
}

type SearchData struct {
	PageData
	Query string
	Posts []PostView
}

type ProfileData struct {
	PageData
	Account      *user.PublicUserView
	CommentCount int64
	Recent       []comment.View
}

type PageService struct {
	coreTheme.SEOPageService
}

func NewPageService(app coreTheme.App) *PageService {
	base := coreTheme.NewBasePageService(app.Database(), app.ContentRepo(), app.TaxonomyRepo(), app.OptionsStore())
	return &PageService{SEOPageService: coreTheme.NewSEOPageService(base, app.SEOBuilder(), app.ContentRegistry(), app.HookBus(), app.I18nManager())}
}

func NewPageServiceDB(db *gorm.DB) *PageService {
	return &PageService{SEOPageService: coreTheme.NewSEOPageService(coreTheme.NewBasePageServiceDB(db), nil, nil, nil, nil)}
}

func (s *PageService) ForRequest(c *gin.Context) *PageService {
	clone := *s
	clone.BasePageService = s.BasePageService.ForRequest(c)
	return &clone
}

func (s *PageService) GetHomeData() (*HomeData, error) {
	items, err := content.NewQuery(s.DB).Type("post").Published().OrderBy("published_at", "DESC").Limit(7).Get()
	if err != nil {
		return nil, err
	}
	posts := s.toPostViews(items)
	data := &HomeData{
		PageData:   PageData{Title: "Home", ActivePage: "home", Settings: s.Settings(), SEO: s.BuildHomeSEO()},
		Categories: s.categoryViews(),
	}
	if len(posts) > 0 {
		data.Featured = &posts[0]
		data.Posts = posts[1:]
	}
	return data, nil
}

func (s *PageService) GetSearchData(rawQuery string) (*SearchData, error) {
	query := strings.TrimSpace(rawQuery)
	if utf8.RuneCountInString(query) > 80 {
		query = string([]rune(query)[:80])
	}
	data := &SearchData{
		PageData: PageData{Title: "Search", ActivePage: "search", Settings: s.Settings(), SEO: s.BuildHomeSEO()},
		Query:    query,
	}
	data.SEO.Title = "Search"
	data.SEO.Description = ""
	data.SEO.CanonicalURL = ""
	data.SEO.OGTitle = ""
	data.SEO.OGDescription = ""
	data.SEO.OGImage = ""
	data.SEO.JSONLD = ""
	data.SEO.Robots = "noindex,follow"
	if query == "" {
		return data, nil
	}
	items, err := content.NewQuery(s.DB).Type("post").Published().Search(query).OrderBy("published_at", "DESC").Limit(24).Get()
	if err != nil {
		return nil, err
	}
	data.Posts = s.toPostViews(items)
	return data, nil
}

func (s *PageService) GetProfileData(title string, account *user.PublicUserView, commentCount int64, recent []comment.View) *ProfileData {
	seo := s.BuildHomeSEO()
	seo.Title = title
	seo.Description = ""
	seo.CanonicalURL = ""
	seo.OGTitle = ""
	seo.OGDescription = ""
	seo.OGImage = ""
	seo.JSONLD = ""
	seo.Robots = "noindex,nofollow"
	return &ProfileData{
		PageData: PageData{Title: title, ActivePage: "profile", Settings: s.Settings(), SEO: seo},
		Account:  account, CommentCount: commentCount, Recent: recent,
	}
}

func (s *PageService) toPostViews(items []content.Content) []PostView {
	views := make([]PostView, 0, len(items))
	for _, item := range items {
		view := PostView{
			ID: item.ID, Type: item.Type, Title: item.Title, Slug: item.Slug,
			Content: item.Content, Excerpt: item.Excerpt, ImageURL: item.ImageURL,
			PublishedAt: item.PublishedAt, CreatedAt: item.CreatedAt,
		}
		if s.Tax != nil {
			if cats, err := s.Tax.GetContentTaxonomies(item.ID, "category"); err == nil && len(cats) > 0 {
				view.Category = TermView{Name: cats[0].Term.Name, Slug: cats[0].Term.Slug}
			}
			if tags, err := s.Tax.GetContentTaxonomies(item.ID, "tag"); err == nil {
				for _, tag := range tags {
					view.Tags = append(view.Tags, TermView{Name: tag.Term.Name, Slug: tag.Term.Slug})
				}
			}
		}
		views = append(views, view)
	}
	return views
}

func (s *PageService) categoryViews() []TermView {
	if s.Tax == nil {
		return nil
	}
	items, err := s.Tax.ListByTaxonomy("category")
	if err != nil {
		return nil
	}
	views := make([]TermView, 0, len(items))
	for _, item := range items {
		count, _ := content.NewQuery(s.DB).Type("post").Published().Taxonomy("category", item.Term.Slug).Count()
		if count > 0 {
			views = append(views, TermView{Name: item.Term.Name, Slug: item.Term.Slug, Count: count})
		}
	}
	return views
}
