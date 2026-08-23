package multilang

import (
	"database/sql"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core"
	"github.com/0xmattg/go-press/core/admin"
	"github.com/0xmattg/go-press/core/content"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/menu"
	"github.com/0xmattg/go-press/core/option"
	corePlugin "github.com/0xmattg/go-press/core/plugin"
	"github.com/0xmattg/go-press/core/rewrite"
	"github.com/0xmattg/go-press/core/taxonomy"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type activeMultilangPluginStub struct{}

func (activeMultilangPluginStub) Name() string        { return PluginName }
func (activeMultilangPluginStub) Version() string     { return "test" }
func (activeMultilangPluginStub) Description() string { return "" }
func (activeMultilangPluginStub) Activate(corePlugin.App) {
}
func (activeMultilangPluginStub) Deactivate(corePlugin.App) {
}

func TestSettingsTemplateParses(t *testing.T) {
	passthrough := func(...interface{}) string { return "" }
	_, err := template.New("multilang-settings").Funcs(template.FuncMap{
		"X": passthrough, "toJSON": passthrough,
		"adminContentTypeLabel": passthrough, "adminTaxonomyLabel": passthrough,
		"settingOr": passthrough,
	}).ParseFiles("templates/admin/settings.tmpl")
	if err != nil {
		t.Fatalf("parse multilang settings template: %v", err)
	}
}

func TestRewriteItemURLSkipsNonPageLinks(t *testing.T) {
	p := &Plugin{}
	tests := []string{
		"https://github.com/0xmattg/go-press",
		"http://example.com",
		"//cdn.example.com/app.js",
		"mailto:hello@example.com",
		"tel:+15550100",
		"#features",
		"?preview=1",
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			item := menu.Item{URL: rawURL}
			if got := p.rewriteItemURL(item, "en"); got != rawURL {
				t.Fatalf("rewriteItemURL(%q) = %q, want unchanged", rawURL, got)
			}
		})
	}
}

func TestTaxonomyTranslationRouteRejectsRoleWithoutTaxonomyPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("taxonomy-route-secret", 1, nil)
	token, err := auth.GenerateToken(&user.User{ID: 2, Username: "reader", Role: user.RoleSubscriber})
	if err != nil {
		t.Fatal(err)
	}
	p := &Plugin{engine: &core.Engine{RBAC: user.NewRBAC()}}
	r := gin.New()
	r.POST("/admin/plugins/multi-language/taxonomy-translate", admin.AuthMiddleware(auth), p.handleCreateTaxonomyTranslation)
	form := url.Values{"taxonomy_id": {"1"}, "target_lang": {"zh"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/plugins/multi-language/taxonomy-translate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func multilangTaxonomyTestPlugin(t *testing.T) (*Plugin, *gorm.DB) {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE gp_terms (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, slug TEXT NOT NULL)`,
		`CREATE TABLE gp_taxonomies (id INTEGER PRIMARY KEY AUTOINCREMENT, term_id INTEGER NOT NULL, taxonomy TEXT NOT NULL, description TEXT, parent_id INTEGER, count INTEGER DEFAULT 0)`,
		`CREATE TABLE gp_term_relationships (content_id INTEGER NOT NULL, taxonomy_id INTEGER NOT NULL, sort_order INTEGER DEFAULT 0, PRIMARY KEY (content_id, taxonomy_id))`,
		`CREATE TABLE gp_plgn_multilang_languages (id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT NOT NULL, name TEXT NOT NULL, flag TEXT, is_default BOOLEAN, sort_order INTEGER, active BOOLEAN)`,
		`CREATE TABLE gp_plgn_multilang_taxonomy_translation_groups (id INTEGER PRIMARY KEY AUTOINCREMENT, taxonomy_type TEXT NOT NULL, created_at DATETIME)`,
		`CREATE TABLE gp_plgn_multilang_taxonomy_translations (id INTEGER PRIMARY KEY AUTOINCREMENT, group_id INTEGER NOT NULL, taxonomy_id INTEGER NOT NULL, language_code TEXT NOT NULL, source_language_code TEXT, created_at DATETIME, updated_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("multilang taxonomy tests require CGO-backed SQLite")
			}
			t.Fatal(err)
		}
	}
	registry := content.NewRegistry()
	registry.RegisterTaxonomy(content.TaxonomyDef{Name: "category", Label: "Category", LabelPlural: "Categories", Hierarchical: true})
	engine := &core.Engine{
		DB: db, Registry: registry, Taxonomy: taxonomy.NewRepository(db),
		Options: option.NewMemoryStore(map[string]string{taxonomyModeOption("category"): taxonomyModeTranslatedOnly}),
	}
	engine.Rewrite = rewrite.NewEngine(registry)
	engine.Sitemap = rewrite.NewSitemapGenerator("https://example.test", registry, content.NewRepository(db), engine.Rewrite)
	repo := NewRepository(db)
	p := &Plugin{engine: engine, repo: repo, defaultTag: "en"}
	if err := db.Create(&Language{Code: "en", Name: "English", IsDefault: true, Active: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&Language{Code: "zh", Name: "中文", Active: true}).Error; err != nil {
		t.Fatal(err)
	}
	return p, db
}

func activateMultilangMiddleware(t *testing.T, p *Plugin) {
	t.Helper()
	manager := corePlugin.NewManager()
	manager.Register(activeMultilangPluginStub{})
	if !manager.Activate(PluginName, nil) {
		t.Fatal("activate multilang middleware test stub")
	}
	p.engine.PluginManager = manager
}

func runLanguagePrefixMiddleware(t *testing.T, p *Plugin, method, target, cookie, acceptLanguage string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, nil)
	if cookie != "" {
		c.Request.AddCookie(&http.Cookie{Name: CookieName, Value: cookie})
	}
	if acceptLanguage != "" {
		c.Request.Header.Set("Accept-Language", acceptLanguage)
	}
	p.LanguagePrefixMiddleware()(c)
	return c, recorder
}

func TestLanguagePrefixMiddlewareUsesURLAsCanonicalLanguage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p, _ := multilangTaxonomyTestPlugin(t)
	activateMultilangMiddleware(t, p)

	c, recorder := runLanguagePrefixMiddleware(t, p, http.MethodGet, "/blog/english-post", "zh", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := c.GetString(coreI18n.CtxKeyLang); got != "en" {
		t.Fatalf("unprefixed URL language = %q, want en", got)
	}
	if got := c.Request.URL.Path; got != "/blog/english-post" {
		t.Fatalf("unprefixed URL path = %q, want unchanged", got)
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("unprefixed URL unexpectedly overwrote language cookie: %#v", cookies)
	}

	c, recorder = runLanguagePrefixMiddleware(t, p, http.MethodGet, "/zh/blog/chinese-post", "en", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("prefixed status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := c.GetString(coreI18n.CtxKeyLang); got != "zh" {
		t.Fatalf("prefixed URL language = %q, want zh", got)
	}
	if got := c.Request.URL.Path; got != "/blog/chinese-post" {
		t.Fatalf("stripped URL path = %q, want /blog/chinese-post", got)
	}
	responseCookies := recorder.Result().Cookies()
	if len(responseCookies) != 1 || responseCookies[0].Name != CookieName || responseCookies[0].Value != "zh" {
		t.Fatalf("prefixed URL cookie = %#v, want %s=zh", responseCookies, CookieName)
	}
}

func TestLanguagePrefixMiddlewareRedirectsPreferredLanguageOnlyAtRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p, _ := multilangTaxonomyTestPlugin(t)
	activateMultilangMiddleware(t, p)

	c, recorder := runLanguagePrefixMiddleware(t, p, http.MethodGet, "/?preview=1&lang=zh", "", "")
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/zh/?preview=1" {
		t.Fatalf("query preference redirect = (%d, %q), want (%d, %q)", recorder.Code, recorder.Header().Get("Location"), http.StatusFound, "/zh/?preview=1")
	}
	if !c.IsAborted() {
		t.Fatal("root preference redirect did not abort middleware chain")
	}

	_, recorder = runLanguagePrefixMiddleware(t, p, http.MethodGet, "/", "", "zh-CN,zh;q=0.9,en;q=0.8")
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/zh/" {
		t.Fatalf("header preference redirect = (%d, %q), want (%d, %q)", recorder.Code, recorder.Header().Get("Location"), http.StatusFound, "/zh/")
	}

	c, recorder = runLanguagePrefixMiddleware(t, p, http.MethodGet, "/", "en", "zh")
	if recorder.Code != http.StatusOK || recorder.Header().Get("Location") != "" {
		t.Fatalf("default root response = (%d, %q), want 200 without redirect", recorder.Code, recorder.Header().Get("Location"))
	}
	if got := c.GetString(coreI18n.CtxKeyLang); got != "en" {
		t.Fatalf("default root language = %q, want en", got)
	}
}

func TestTaxonomyTranslationRouteCreatesScopedChildWithTranslatedParent(t *testing.T) {
	p, db := multilangTaxonomyTestPlugin(t)
	p.engine.RBAC = user.NewRBAC()
	p.engine.TaxonomyCommands = taxonomy.NewCommandService(db, p.engine.Registry)

	createTaxonomy := func(name, slug string, parentID *uint) taxonomy.Taxonomy {
		term := taxonomy.Term{Name: name, Slug: slug}
		if err := db.Create(&term).Error; err != nil {
			t.Fatal(err)
		}
		item := taxonomy.Taxonomy{TermID: term.ID, Term: term, Taxonomy: "category", ParentID: parentID}
		if err := db.Omit("Term", "Children").Create(&item).Error; err != nil {
			t.Fatal(err)
		}
		return item
	}
	enParent := createTaxonomy("Parent", "parent", nil)
	zhParent := createTaxonomy("父级", "fuji", nil)
	enChild := createTaxonomy("News", "news", &enParent.ID)
	parentGroup := TaxonomyTranslationGroup{TaxonomyType: "category"}
	if err := db.Create(&parentGroup).Error; err != nil {
		t.Fatal(err)
	}
	for _, link := range []TaxonomyTranslation{
		{GroupID: parentGroup.ID, TaxonomyID: enParent.ID, LanguageCode: "en", SourceLanguageCode: "en"},
		{GroupID: parentGroup.ID, TaxonomyID: zhParent.ID, LanguageCode: "zh", SourceLanguageCode: "en"},
	} {
		if err := db.Create(&link).Error; err != nil {
			t.Fatal(err)
		}
	}

	r := gin.New()
	r.POST("/admin/plugins/multi-language/taxonomy-translate", func(c *gin.Context) {
		c.Set("admin_role", user.RoleEditor)
		p.handleCreateTaxonomyTranslation(c)
	})
	form := url.Values{
		"taxonomy_id": {strconv.FormatUint(uint64(enChild.ID), 10)}, "target_lang": {"zh"},
		"name": {"新闻"}, "slug": {"xinwen"}, "description": {"中文描述"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/plugins/multi-language/taxonomy-translate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	sourceLink, err := p.repo.GetTaxonomyTranslation(enChild.ID)
	if err != nil {
		t.Fatal(err)
	}
	targetLink, err := p.repo.FindTaxonomyTranslation(sourceLink.GroupID, "zh")
	if err != nil {
		t.Fatal(err)
	}
	target, err := p.engine.Taxonomy.GetTaxonomy(targetLink.TaxonomyID)
	if err != nil {
		t.Fatal(err)
	}
	if target.Term.Name != "新闻" || target.Term.Slug != "xinwen" || target.Description != "中文描述" || target.ParentID == nil || *target.ParentID != zhParent.ID {
		t.Fatalf("translated child = %#v", target)
	}
}

func TestTaxonomyTranslationScopesAndSwitchesToTranslatedSlug(t *testing.T) {
	p, db := multilangTaxonomyTestPlugin(t)
	enTerm := taxonomy.Term{Name: "News", Slug: "news"}
	zhTerm := taxonomy.Term{Name: "新闻", Slug: "xinwen"}
	if err := db.Create(&enTerm).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&zhTerm).Error; err != nil {
		t.Fatal(err)
	}
	enTax := taxonomy.Taxonomy{TermID: enTerm.ID, Taxonomy: "category"}
	zhTax := taxonomy.Taxonomy{TermID: zhTerm.ID, Taxonomy: "category"}
	if err := db.Create(&enTax).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&zhTax).Error; err != nil {
		t.Fatal(err)
	}
	group := TaxonomyTranslationGroup{TaxonomyType: "category"}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	for _, link := range []TaxonomyTranslation{
		{GroupID: group.ID, TaxonomyID: enTax.ID, LanguageCode: "en", SourceLanguageCode: "en"},
		{GroupID: group.ID, TaxonomyID: zhTax.ID, LanguageCode: "zh", SourceLanguageCode: "en"},
	} {
		if err := db.Create(&link).Error; err != nil {
			t.Fatal(err)
		}
	}

	ctx := &gin.Context{}
	p.registerLangTaxonomyScope(ctx, "zh")
	items, err := p.engine.Taxonomy.ListByTaxonomyContext(taxonomy.RequestContext(ctx), "category")
	if err != nil || len(items) != 1 || items[0].Term.Slug != "xinwen" {
		t.Fatalf("zh taxonomy scope = %#v err=%v", items, err)
	}
	path, matched := p.resolveTaxonomyTranslation("/category/news", "en", "zh")
	if !matched || path != "/zh/category/xinwen" {
		t.Fatalf("translated path = %q matched=%v", path, matched)
	}
	entry := &rewrite.SitemapEntry{
		TaxonomyType: "category", TaxonomyID: enTax.ID,
		URL: rewrite.SitemapURL{Loc: "https://example.test/category/news"},
	}
	p.sitemapTransformer(entry)
	if entry.Skip || len(entry.Extra) != 1 || entry.Extra[0].Loc != "https://example.test/zh/category/xinwen" {
		t.Fatalf("taxonomy sitemap entry = %#v", entry)
	}
	if len(entry.URL.Alternates) != 3 {
		t.Fatalf("taxonomy sitemap alternates = %#v", entry.URL.Alternates)
	}
	if err := p.ValidateSettings(map[string]string{taxonomyModeOption("category"): taxonomyModeShared}); err == nil {
		t.Fatal("translated taxonomy switched back to shared mode")
	}
	if err := p.CanDeactivate(nil); err == nil {
		t.Fatal("plugin deactivation was allowed while taxonomy translations exist")
	}
	if err := p.repo.DeleteTaxonomyTranslation(zhTax.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.CanDeactivate(nil); err != nil {
		t.Fatalf("plugin deactivation remained blocked after the last translated variant was removed: %v", err)
	}
	if err := p.ValidateSettings(map[string]string{taxonomyModeOption("category"): taxonomyModeShared}); err != nil {
		t.Fatalf("shared mode remained blocked after translation group cleanup: %v", err)
	}
}

func TestSharedTaxonomyModeKeepsLegacyResolution(t *testing.T) {
	p, _ := multilangTaxonomyTestPlugin(t)
	p.engine.Options = option.NewMemoryStore(map[string]string{taxonomyModeOption("category"): taxonomyModeShared})
	if path, matched := p.resolveTaxonomyTranslation("/category/news", "en", "zh"); matched || path != "" {
		t.Fatalf("shared mode intercepted legacy taxonomy path: path=%q matched=%v", path, matched)
	}
}

func TestRewriteItemURLPrefixesLocalLinks(t *testing.T) {
	p := &Plugin{}
	tests := map[string]string{
		"/about": "en/about",
		"about":  "en/about",
		"/":      "en/",
	}

	for rawURL, wantSuffix := range tests {
		t.Run(rawURL, func(t *testing.T) {
			item := menu.Item{URL: rawURL}
			want := "/" + wantSuffix
			if got := p.rewriteItemURL(item, "en"); got != want {
				t.Fatalf("rewriteItemURL(%q) = %q, want %q", rawURL, got, want)
			}
		})
	}
}

func TestSiteOptionTranslationRouteRejectsSubscriber(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("test-secret", 1, nil)
	token, err := auth.GenerateToken(&user.User{ID: 1, Username: "reader", Role: user.RoleSubscriber})
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	p := &Plugin{}
	r := gin.New()
	r.POST(
		"/admin/plugins/multi-language/site-option-translate",
		admin.RequirePermission(auth, user.NewRBAC(), "plugin", "update"),
		p.handleSiteOptionTranslationSave,
	)
	req := httptest.NewRequest(http.MethodPost, "/admin/plugins/multi-language/site-option-translate", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
