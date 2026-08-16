package communa

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/0xmattg/go-press/core"
	"github.com/0xmattg/go-press/core/content"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/rewrite"
	coreTheme "github.com/0xmattg/go-press/core/theme"
)

// ======== View Models ========

// PageData is the base data shared by all custom-routed pages.
type PageData struct {
	Ctx         *gin.Context `json:"-"`
	Title       string
	ActivePage  string
	Settings    map[string]string
	RecentPosts []PostView
	SEO         rewrite.SEOMeta
}

// SetCtx injects the gin.Context so templates can use {{T .Ctx "key"}}.
func (p *PageData) SetCtx(c *gin.Context) { p.Ctx = c }

// TranslateSettings replaces translatable option values with translated versions
// for the current request language.
func (p *PageData) TranslateSettings(c *gin.Context, mgr *coreI18n.Manager) {
	p.Settings = mgr.TranslateSettings(c, p.Settings, option.IsTranslatable, option.AllTranslatableKeys())
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

// MemberView is one community member profile.
type MemberView struct {
	ID          uint
	Title       string // display name
	Slug        string
	Excerpt     string // short tagline / headline
	Content     string // bio (HTML)
	ImageURL    string // avatar
	CoverImage  string
	Role        string
	Location    string
	Joined      string
	Website     string
	PostsCount  string
	GroupsCount string
	Tags        []TagView
	SortOrder   int
	URL         string
}

// GroupView is one community group / circle.
type GroupView struct {
	ID           uint
	Title        string
	Slug         string
	Excerpt      string
	Content      string
	ImageURL     string // cover
	MembersCount string
	Privacy      string
	Activity     string
	Lead         string
	Category     CategoryView
	SortOrder    int
	URL          string
}

// DiscussionView is one forum topic / thread.
type DiscussionView struct {
	ID           uint
	Title        string
	Slug         string
	Excerpt      string
	Content      string
	ImageURL     string
	AuthorName   string
	AuthorAvatar string
	GroupName    string
	RepliesCount string
	Category     CategoryView
	Tags         []TagView
	PublishedAt  *time.Time
	CreatedAt    time.Time
	URL          string
}

// PostView is one blog / activity article.
type PostView struct {
	ID           uint
	Title        string
	Slug         string
	Content      string
	Excerpt      string
	ImageURL     string
	AuthorName   string
	AuthorAvatar string
	Category     CategoryView
	Tags         []TagView
	PublishedAt  *time.Time
	CreatedAt    time.Time
	URL          string
}

// ActivityItem is a unified feed row that blends recent posts and discussions,
// mirroring the community "Latest Activity" stream.
type ActivityItem struct {
	Kind         string // "post" | "discussion"
	Title        string
	Excerpt      string
	URL          string
	AuthorName   string
	AuthorAvatar string
	GroupName    string
	Count        int // comments (posts) or replies (discussions)
	PublishedAt  *time.Time
}

// CommunityStats holds the headline counts shown in hero chips and the stats
// widget.
type CommunityStats struct {
	Members     int
	Groups      int
	Discussions int
	Posts       int
}

// ======== Page Data Structs ========

type HomeData struct {
	PageData
	Members     []MemberView
	Groups      []GroupView
	Discussions []DiscussionView
	Activity    []ActivityItem
	// Featured drives the homepage banner carousel: recent posts that carry an
	// image, each linking to its article.
	Featured []PostView
	Stats    CommunityStats
}

type AboutData struct {
	PageData
	Stats CommunityStats
}

type ContactData struct {
	PageData
	Success bool
	Error   string
}

// ======== PageService ========

// PageService assembles typed page data for the custom-routed community pages
// (home / about / contact). Archive and single pages are rendered generically by
// BaseTheme and do not go through this service.
type PageService struct {
	coreTheme.SEOPageService
	rewriteEngine *rewrite.Engine
}

// NewPageService creates a PageService backed by the full engine.
func NewPageService(engine *core.Engine) *PageService {
	return &PageService{
		SEOPageService: coreTheme.NewSEOPageService(
			coreTheme.NewBasePageService(engine.DB, engine.Content, engine.Taxonomy, engine.Options),
			engine.SEO, engine.Registry, engine.Hooks, engine.I18n),
		rewriteEngine: engine.Rewrite,
	}
}

// NewPageServiceDB creates a PageService backed only by a DB connection.
func NewPageServiceDB(db *gorm.DB) *PageService {
	return &PageService{SEOPageService: coreTheme.NewSEOPageService(coreTheme.NewBasePageServiceDB(db), nil, nil, nil, nil)}
}

// ForRequest returns a clone with request-scoped content filters applied (e.g.
// language scoping from the multilang plugin). This is a core pattern — no
// plugin-specific logic here.
func (s *PageService) ForRequest(c *gin.Context) *PageService {
	clone := *s
	clone.BasePageService = s.BasePageService.ForRequest(c)
	return &clone
}

// ======== URL / query helpers ========

func (s *PageService) contentURL(contentType, slug string) string {
	if s != nil && s.rewriteEngine != nil {
		return s.rewriteEngine.BuildURL(contentType, slug)
	}
	slug = strings.Trim(slug, "/")
	if slug == "" {
		return "/"
	}
	return "/" + strings.Trim(contentType, "/") + "/" + slug
}

func (s *PageService) getContentList(contentType, orderField, orderDir string, limit int) ([]content.Content, error) {
	q := content.NewQuery(content.ScopedDB(s.ReqCtx, s.DB)).
		Type(contentType).
		Status(content.StatusPublished).
		OrderBy(orderField, orderDir)
	if limit > 0 {
		q = q.Limit(limit)
	}
	return q.Get()
}

func (s *PageService) countPublished(contentType string) int {
	if s == nil || s.DB == nil {
		return 0
	}
	var n int64
	err := content.ScopedDB(s.ReqCtx, s.DB).
		Model(&content.Content{}).
		Where("type = ? AND status = ? AND deleted_at IS NULL", contentType, content.StatusPublished).
		Count(&n).Error
	if err != nil {
		return 0
	}
	return int(n)
}

func (s *PageService) getRecentPosts(n int) []PostView {
	posts, _ := content.NewQuery(content.ScopedDB(s.ReqCtx, s.DB)).
		Type("post").Published().
		OrderBy("published_at", "DESC").
		Limit(n).Get()
	views := make([]PostView, len(posts))
	for i, c := range posts {
		views[i] = s.toPostView(c, false)
	}
	return views
}

func (s *PageService) buildPageData(title, activePage string) PageData {
	return PageData{
		Title:       title,
		ActivePage:  activePage,
		Settings:    s.Settings(),
		RecentPosts: s.getRecentPosts(4),
	}
}

func (s *PageService) communityStats() CommunityStats {
	return CommunityStats{
		Members:     s.countPublished("member"),
		Groups:      s.countPublished("group"),
		Discussions: s.countPublished("discussion"),
		Posts:       s.countPublished("post"),
	}
}

// ======== Model converters ========

func (s *PageService) categoryOf(id uint) CategoryView {
	cats, _ := s.Tax.GetContentTaxonomies(id, "category")
	if len(cats) > 0 {
		return CategoryView{ID: cats[0].ID, Name: cats[0].Term.Name, Slug: cats[0].Term.Slug}
	}
	return CategoryView{}
}

func (s *PageService) tagsOf(id uint) []TagView {
	tags, _ := s.Tax.GetContentTaxonomies(id, "tag")
	views := make([]TagView, len(tags))
	for i, t := range tags {
		views[i] = TagView{ID: t.ID, Name: t.Term.Name, Slug: t.Term.Slug}
	}
	return views
}

func (s *PageService) toMemberView(c content.Content) MemberView {
	meta, _ := s.Content.GetMeta(c.ID)
	return MemberView{
		ID:          c.ID,
		Title:       c.Title,
		Slug:        c.Slug,
		Excerpt:     c.Excerpt,
		Content:     c.Content,
		ImageURL:    c.ImageURL,
		CoverImage:  meta["cover_image"],
		Role:        meta["role"],
		Location:    meta["location"],
		Joined:      meta["joined"],
		Website:     meta["website"],
		PostsCount:  meta["posts_count"],
		GroupsCount: meta["groups_count"],
		Tags:        s.tagsOf(c.ID),
		SortOrder:   c.SortOrder,
		URL:         s.contentURL("member", c.Slug),
	}
}

func (s *PageService) toGroupView(c content.Content) GroupView {
	meta, _ := s.Content.GetMeta(c.ID)
	return GroupView{
		ID:           c.ID,
		Title:        c.Title,
		Slug:         c.Slug,
		Excerpt:      c.Excerpt,
		Content:      c.Content,
		ImageURL:     c.ImageURL,
		MembersCount: meta["members_count"],
		Privacy:      meta["privacy"],
		Activity:     meta["activity"],
		Lead:         meta["lead"],
		Category:     s.categoryOf(c.ID),
		SortOrder:    c.SortOrder,
		URL:          s.contentURL("group", c.Slug),
	}
}

func (s *PageService) toDiscussionView(c content.Content) DiscussionView {
	meta, _ := s.Content.GetMeta(c.ID)
	return DiscussionView{
		ID:           c.ID,
		Title:        c.Title,
		Slug:         c.Slug,
		Excerpt:      c.Excerpt,
		Content:      c.Content,
		ImageURL:     c.ImageURL,
		AuthorName:   meta["author_name"],
		AuthorAvatar: meta["author_avatar"],
		GroupName:    meta["group_name"],
		RepliesCount: meta["replies_count"],
		Category:     s.categoryOf(c.ID),
		Tags:         s.tagsOf(c.ID),
		PublishedAt:  c.PublishedAt,
		CreatedAt:    c.CreatedAt,
		URL:          s.contentURL("discussion", c.Slug),
	}
}

// toPostView converts a post row. When withTax is false, taxonomy lookups are
// skipped (used for lightweight footer/recent lists).
func (s *PageService) toPostView(c content.Content, withTax bool) PostView {
	pv := PostView{
		ID:          c.ID,
		Title:       c.Title,
		Slug:        c.Slug,
		Content:     c.Content,
		Excerpt:     c.Excerpt,
		ImageURL:    c.ImageURL,
		PublishedAt: c.PublishedAt,
		CreatedAt:   c.CreatedAt,
		URL:         s.contentURL("post", c.Slug),
	}
	meta, _ := s.Content.GetMeta(c.ID)
	pv.AuthorName = meta["author_name"]
	pv.AuthorAvatar = meta["author_avatar"]
	if withTax {
		pv.Category = s.categoryOf(c.ID)
		pv.Tags = s.tagsOf(c.ID)
	}
	return pv
}

// ======== Home ========

func (s *PageService) GetHomeData() (*HomeData, error) {
	memberRows, err := s.getContentList("member", "sort_order", "ASC", 6)
	if err != nil {
		return nil, err
	}
	groupRows, err := s.getContentList("group", "sort_order", "ASC", 6)
	if err != nil {
		return nil, err
	}
	discussionRows, err := s.getContentList("discussion", "published_at", "DESC", 6)
	if err != nil {
		return nil, err
	}
	postRows, err := s.getContentList("post", "published_at", "DESC", 8)
	if err != nil {
		return nil, err
	}

	members := make([]MemberView, len(memberRows))
	for i, c := range memberRows {
		members[i] = s.toMemberView(c)
	}
	groups := make([]GroupView, len(groupRows))
	for i, c := range groupRows {
		groups[i] = s.toGroupView(c)
	}
	discussions := make([]DiscussionView, len(discussionRows))
	for i, c := range discussionRows {
		discussions[i] = s.toDiscussionView(c)
	}

	// Featured banner: recent posts that carry an image, most recent first.
	featured := make([]PostView, 0, 5)
	for _, c := range postRows {
		if strings.TrimSpace(c.ImageURL) == "" {
			continue
		}
		pv := s.toPostView(c, true)
		pv.Excerpt = compactExcerpt(pv.Excerpt, pv.Content, 130)
		featured = append(featured, pv)
		if len(featured) == 5 {
			break
		}
	}

	data := &HomeData{
		PageData:    s.buildPageData("Home", "home"),
		Members:     members,
		Groups:      groups,
		Discussions: discussions,
		Activity:    s.buildActivity(postRows, discussionRows, 7),
		Featured:    featured,
		Stats:       s.communityStats(),
	}
	data.SEO = s.BuildHomeSEO()
	return data, nil
}

// buildActivity blends recent posts and discussions into one reverse-chron feed.
func (s *PageService) buildActivity(posts, discussions []content.Content, limit int) []ActivityItem {
	items := make([]ActivityItem, 0, len(posts)+len(discussions))
	for _, p := range posts {
		meta, _ := s.Content.GetMeta(p.ID)
		items = append(items, ActivityItem{
			Kind:         "post",
			Title:        p.Title,
			Excerpt:      compactExcerpt(p.Excerpt, p.Content, 180),
			URL:          s.contentURL("post", p.Slug),
			AuthorName:   meta["author_name"],
			AuthorAvatar: meta["author_avatar"],
			PublishedAt:  activityTime(p),
		})
	}
	for _, d := range discussions {
		meta, _ := s.Content.GetMeta(d.ID)
		items = append(items, ActivityItem{
			Kind:         "discussion",
			Title:        d.Title,
			Excerpt:      compactExcerpt(d.Excerpt, d.Content, 180),
			URL:          s.contentURL("discussion", d.Slug),
			AuthorName:   meta["author_name"],
			AuthorAvatar: meta["author_avatar"],
			GroupName:    meta["group_name"],
			Count:        atoiOr(meta["replies_count"], 0),
			PublishedAt:  activityTime(d),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return activityStamp(items[i].PublishedAt).After(activityStamp(items[j].PublishedAt))
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func activityTime(c content.Content) *time.Time {
	if c.PublishedAt != nil {
		return c.PublishedAt
	}
	t := c.CreatedAt
	return &t
}

func activityStamp(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// ======== About ========

func (s *PageService) GetAboutData() (*AboutData, error) {
	return &AboutData{
		PageData: s.buildPageData("About", "about"),
		Stats:    s.communityStats(),
	}, nil
}

// ======== Contact ========

func (s *PageService) GetContactData() (*ContactData, error) {
	return &ContactData{
		PageData: s.buildPageData("Contact", "contact"),
	}, nil
}

// SubmitContact stores a contact message as a core contact_message content row.
func (s *PageService) SubmitContact(c *gin.Context, name, email, phone, message string) error {
	ctx := context.Background()
	remoteIP := ""
	if c != nil {
		ctx = c.Request.Context()
		remoteIP = c.ClientIP()
	}
	return s.Content.CreateContactMessage(ctx, content.ContactMessageInput{
		Name:     name,
		Email:    email,
		Phone:    phone,
		Message:  message,
		RemoteIP: remoteIP,
	})
}
