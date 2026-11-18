// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"bytes"
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/mail"
	coreMedia "github.com/0xmattg/go-press/core/media"
	"github.com/0xmattg/go-press/core/menu"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/rewrite"
	"github.com/0xmattg/go-press/core/taxonomy"
	"github.com/0xmattg/go-press/pkg/dbprefix"

	"github.com/gin-gonic/gin"
	"github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// sqliteWithNow is a go-sqlite3 driver with a now() function. SQLite has none,
// and ContentQuery.Published compares published_at with NOW(). The function
// returns the format go-sqlite3 uses for bound time.Time values, so for UTC
// times the text comparison orders them correctly.
const sqliteWithNow = "sqlite3_gopress_now"

var registerSQLiteWithNow sync.Once

func dataFuncTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	registerSQLiteWithNow.Do(func() {
		sql.Register(sqliteWithNow, &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("now", func() string {
				return time.Now().UTC().Format(sqlite3.SQLiteTimestampFormats[0])
			}, false)
		}})
	})
	dbprefix.Set(dbprefix.DefaultPrefix)
	sqlDB, err := sql.Open(sqliteWithNow, "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		DisableAutomaticPing: true, SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE gp_contents (
			id INTEGER PRIMARY KEY AUTOINCREMENT, type TEXT NOT NULL, status TEXT NOT NULL,
			title TEXT NOT NULL, slug TEXT NOT NULL, content TEXT, excerpt TEXT, image_url TEXT,
			author_id INTEGER, parent_id INTEGER, sort_order INTEGER DEFAULT 0,
			comment_status TEXT DEFAULT 'open', published_at DATETIME,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE gp_content_meta (id INTEGER PRIMARY KEY AUTOINCREMENT, content_id INTEGER NOT NULL, meta_key TEXT NOT NULL, meta_value TEXT)`,
		`CREATE TABLE gp_terms (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, slug TEXT NOT NULL)`,
		`CREATE TABLE gp_taxonomies (id INTEGER PRIMARY KEY AUTOINCREMENT, term_id INTEGER NOT NULL, taxonomy TEXT NOT NULL, description TEXT, parent_id INTEGER, count INTEGER DEFAULT 0)`,
		`CREATE TABLE gp_term_relationships (content_id INTEGER NOT NULL, taxonomy_id INTEGER NOT NULL, sort_order INTEGER DEFAULT 0, PRIMARY KEY (content_id, taxonomy_id))`,
		`CREATE TABLE gp_test_content_lang (content_id INTEGER PRIMARY KEY, lang TEXT NOT NULL)`,
		`CREATE TABLE gp_test_taxonomy_lang (taxonomy_id INTEGER PRIMARY KEY, lang TEXT NOT NULL)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("data function tests require CGO-backed SQLite")
			}
			t.Fatalf("create schema: %v", err)
		}
	}
	return db
}

// dataTestApp is the smallest App the data functions and BaseTheme pages need.
type dataTestApp struct {
	db       *gorm.DB
	contents *content.Repository
	terms    *taxonomy.Repository
	registry *content.Registry
	rewrite  *rewrite.Engine
}

func (a *dataTestApp) Database() *gorm.DB                 { return a.db }
func (a *dataTestApp) ContentRepo() *content.Repository   { return a.contents }
func (a *dataTestApp) TaxonomyRepo() *taxonomy.Repository { return a.terms }
func (a *dataTestApp) ContentRegistry() *content.Registry { return a.registry }
func (a *dataTestApp) OptionsStore() *option.Store        { return nil }
func (a *dataTestApp) RewriteEngine() *rewrite.Engine     { return a.rewrite }
func (a *dataTestApp) SEOBuilder() *rewrite.SEOBuilder    { return nil }
func (a *dataTestApp) MenuStore() *menu.Store             { return nil }
func (a *dataTestApp) MediaRepo() *coreMedia.Repository   { return nil }
func (a *dataTestApp) I18nManager() *coreI18n.Manager     { return nil }
func (a *dataTestApp) HookBus() *hook.Bus                 { return nil }
func (a *dataTestApp) MailSender() mail.Sender            { return nil }
func (a *dataTestApp) SiteLocation() *time.Location       { return time.UTC }

// Fixture content IDs.
const (
	idWidget     = 1  // product, sort 2, meta price=10, product_cat widgets
	idGadget     = 2  // product, sort 1
	idDraft      = 3  // product, draft
	idScheduled  = 4  // product, published an hour from now
	idDeleted    = 5  // product, soft deleted
	idTrashed    = 6  // product, trash
	idOldPost    = 7  // post, category news
	idNewPost    = 8  // post, category news, tag hot
	idAbout      = 9  // page "about"
	idMessage    = 10 // contact_message, forced to published
	idFeatured   = 11 // product, sort 3, meta featured=1, product_cat widgets
	idOtherLang  = 12 // product in the "zh" scope only
	idUntitled   = 13 // product of a type nobody registered
	termWidgets  = 1
	termNews     = 2
	termHot      = 3
	termZhNews   = 4
	termEmptyCat = 5
)

// dataFuncTheme builds a BaseTheme over a seeded database. Every content row
// is "en" except idOtherLang ("zh"); every taxonomy row is "en" except
// termZhNews.
func dataFuncTheme(t *testing.T) (*BaseTheme, *gorm.DB) {
	t.Helper()
	db := dataFuncTestDB(t)
	return seedDataFuncTheme(t, db), db
}

// seedDataFuncTheme seeds the fixture into db, whose tables already exist,
// and returns a BaseTheme reading it.
func seedDataFuncTheme(t *testing.T, db *gorm.DB) *BaseTheme {
	t.Helper()
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{Name: "product", HasArchive: true, Supports: []string{"sort_order"}, Taxonomies: []string{"product_cat"}, Rewrite: content.RewriteRule{Slug: "products"}})
	registry.RegisterType(content.ContentTypeDef{Name: "post", HasArchive: true, Taxonomies: []string{"category", "tag"}, Rewrite: content.RewriteRule{Slug: "blog"}})
	registry.RegisterType(content.ContentTypeDef{Name: "page", Rewrite: content.RewriteRule{Rootless: true}})
	registry.RegisterType(content.ContentTypeDef{Name: "contact_message", ReadOnly: true})
	registry.RegisterTaxonomy(content.TaxonomyDef{Name: "product_cat", ContentTypes: []string{"product"}})
	registry.RegisterTaxonomy(content.TaxonomyDef{Name: "category", ContentTypes: []string{"post"}})
	registry.RegisterTaxonomy(content.TaxonomyDef{Name: "tag", ContentTypes: []string{"post"}})

	now := time.Now().UTC()
	hoursAgo := func(h int) *time.Time { v := now.Add(-time.Duration(h) * time.Hour); return &v }
	rows := []content.Content{
		{ID: idWidget, Type: "product", Status: content.StatusPublished, Title: "Widget", Slug: "widget", SortOrder: 2, PublishedAt: hoursAgo(5)},
		{ID: idGadget, Type: "product", Status: content.StatusPublished, Title: "Gadget", Slug: "gadget", SortOrder: 1, PublishedAt: hoursAgo(4)},
		{ID: idDraft, Type: "product", Status: content.StatusDraft, Title: "Draft", Slug: "draft", PublishedAt: hoursAgo(4)},
		{ID: idScheduled, Type: "product", Status: content.StatusPublished, Title: "Scheduled", Slug: "scheduled", PublishedAt: hoursAgo(-1)},
		{ID: idDeleted, Type: "product", Status: content.StatusPublished, Title: "Deleted", Slug: "deleted", PublishedAt: hoursAgo(4), DeletedAt: gorm.DeletedAt{Time: now, Valid: true}},
		{ID: idTrashed, Type: "product", Status: content.StatusTrash, Title: "Trashed", Slug: "trashed", PublishedAt: hoursAgo(4)},
		{ID: idOldPost, Type: "post", Status: content.StatusPublished, Title: "Old post", Slug: "old", PublishedAt: hoursAgo(3)},
		{ID: idNewPost, Type: "post", Status: content.StatusPublished, Title: "New post", Slug: "new", PublishedAt: hoursAgo(1)},
		{ID: idAbout, Type: "page", Status: content.StatusPublished, Title: "About", Slug: "about", Content: "<p>About us</p>"},
		{ID: idMessage, Type: "contact_message", Status: content.StatusPublished, Title: "Message", Slug: "message", PublishedAt: hoursAgo(1)},
		{ID: idFeatured, Type: "product", Status: content.StatusPublished, Title: "Featured", Slug: "featured", SortOrder: 3, PublishedAt: hoursAgo(2)},
		{ID: idOtherLang, Type: "product", Status: content.StatusPublished, Title: "小部件", Slug: "widget", SortOrder: 0, PublishedAt: hoursAgo(2)},
		{ID: idUntitled, Type: "retired", Status: content.StatusPublished, Title: "Retired", Slug: "retired", PublishedAt: hoursAgo(2)},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed content: %v", err)
		}
	}
	for _, stmt := range []string{
		`INSERT INTO gp_content_meta (content_id, meta_key, meta_value) VALUES (1, 'price', '10'), (11, 'featured', '1'), (2, 'featured', '0')`,
		`INSERT INTO gp_test_content_lang (content_id, lang) SELECT id, 'en' FROM gp_contents WHERE id <> 12`,
		`INSERT INTO gp_test_content_lang (content_id, lang) VALUES (12, 'zh')`,
		`INSERT INTO gp_terms (id, name, slug) VALUES (1, 'Widgets', 'widgets'), (2, 'News', 'news'), (3, 'Hot', 'hot'), (4, '新闻', 'news'), (5, 'Empty', 'empty')`,
		`INSERT INTO gp_taxonomies (id, term_id, taxonomy, count) VALUES (1, 1, 'product_cat', 2), (2, 2, 'category', 2), (3, 3, 'tag', 1), (4, 4, 'category', 9), (5, 5, 'category', 0)`,
		`INSERT INTO gp_test_taxonomy_lang (taxonomy_id, lang) VALUES (1, 'en'), (2, 'en'), (3, 'en'), (4, 'zh'), (5, 'en')`,
		`INSERT INTO gp_term_relationships (content_id, taxonomy_id) VALUES (1, 1), (11, 1), (7, 2), (8, 2), (8, 3), (12, 4)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	app := &dataTestApp{
		db:       db,
		contents: content.NewRepository(db),
		terms:    taxonomy.NewRepository(db),
		registry: registry,
		rewrite:  rewrite.NewEngine(registry),
	}
	b := &BaseTheme{App: app, Templates: &TemplateEngine{}}
	b.meta.Name = "Data Test"
	return b
}

// dataRequest returns a request context carrying content and taxonomy scopes
// for lang, the way a language extension would register them.
func dataRequest(lang string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	if lang != "" {
		content.AddContentScope(c, func(db *gorm.DB) *gorm.DB {
			return db.Where("gp_contents.id IN (SELECT content_id FROM gp_test_content_lang WHERE lang = ?)", lang)
		})
		taxonomy.AddScope(c, taxonomy.Scope{Key: lang, Apply: func(db *gorm.DB) *gorm.DB {
			return db.Where("gp_taxonomies.id IN (SELECT taxonomy_id FROM gp_test_taxonomy_lang WHERE lang = ?)", lang)
		}})
	}
	return c
}

// captureDataWarnings records data function warnings as "function: reason".
func captureDataWarnings(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	got := []string{}
	previous := warnDataFunc
	warnDataFunc = func(msg string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		fields := map[string]string{}
		for i := 0; i+1 < len(args); i += 2 {
			fields[fmt.Sprint(args[i])] = fmt.Sprint(args[i+1])
		}
		if reason, ok := fields["reason"]; ok {
			got = append(got, fields["function"]+": "+reason)
		} else {
			got = append(got, fields["function"]+": "+msg)
		}
	}
	t.Cleanup(func() { warnDataFunc = previous })
	return &got
}

// countQueries counts the SQL statements GORM runs on db.
func countQueries(t *testing.T, db *gorm.DB) *int {
	t.Helper()
	n := 0
	name := "count_" + strings.ReplaceAll(t.Name(), "/", "_")
	if err := db.Callback().Query().After("gorm:query").Register(name, func(*gorm.DB) { n++ }); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(name, func(*gorm.DB) { n++ }); err != nil {
		t.Fatal(err)
	}
	return &n
}

func viewIDs(views []map[string]interface{}) []uint {
	ids := make([]uint, len(views))
	for i, view := range views {
		ids[i], _ = view["ID"].(uint)
	}
	return ids
}

func renderWith(t *testing.T, b *BaseTheme, src string, data interface{}) string {
	t.Helper()
	tmpl, err := template.New("page").Funcs(b.BaseFuncMap()).Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		t.Fatalf("render: %v", err)
	}
	return out.String()
}

func TestContentListReturnsOnlyPublicRowsInTheRequestScope(t *testing.T) {
	b, _ := dataFuncTheme(t)
	c := dataRequest("en")

	// product supports sort_order, so the default order is sort_order ASC.
	got := viewIDs(b.contentListFunc(c, "product", 10))
	if want := []uint{idGadget, idWidget, idFeatured}; !reflect.DeepEqual(got, want) {
		t.Fatalf("en products = %v, want %v (no draft, scheduled, deleted, trashed or zh rows)", got, want)
	}
	if got := viewIDs(b.contentListFunc(dataRequest("zh"), "product", 10)); !reflect.DeepEqual(got, []uint{idOtherLang}) {
		t.Fatalf("zh products = %v, want only the zh row", got)
	}
	// post has no sort_order, so newest first.
	if got := viewIDs(b.contentListFunc(c, "post", 10)); !reflect.DeepEqual(got, []uint{idNewPost, idOldPost}) {
		t.Fatalf("posts = %v", got)
	}
}

func TestContentListRejectsTypesWithoutAPublicRoute(t *testing.T) {
	b, _ := dataFuncTheme(t)
	warnings := captureDataWarnings(t)
	c := dataRequest("en")
	for _, typeName := range []string{"contact_message", "retired", "", "page "} {
		if got := b.contentListFunc(c, typeName, 10); len(got) != 0 && typeName != "page " {
			t.Errorf("%q returned %v", typeName, viewIDs(got))
		}
	}
	// "page " is trimmed to the rootless page type, which is public.
	if got := viewIDs(b.contentListFunc(c, "page ", 10)); !reflect.DeepEqual(got, []uint{idAbout}) {
		t.Fatalf("page = %v", got)
	}
	if len(*warnings) != 1 || (*warnings)[0] != "contentList: content type is not registered or has no public route" {
		t.Fatalf("warnings = %v, want one per request and reason", *warnings)
	}
}

func TestContentListOptions(t *testing.T) {
	b, _ := dataFuncTheme(t)
	c := dataRequest("en")
	for _, tc := range []struct {
		name string
		args []interface{}
		want []uint
	}{
		{"order newest first", []interface{}{"order", "published_at desc"}, []uint{idFeatured, idGadget, idWidget}},
		{"order by title", []interface{}{"order", "title"}, []uint{idWidget, idGadget, idFeatured}},
		{"order ascending", []interface{}{"order", "TITLE asc"}, []uint{idFeatured, idGadget, idWidget}},
		{"taxonomy filter", []interface{}{"taxonomy", "product_cat", "term", "widgets"}, []uint{idWidget, idFeatured}},
		{"unknown term", []interface{}{"taxonomy", "product_cat", "term", "nope"}, []uint{}},
		{"exclude", []interface{}{"exclude", idGadget}, []uint{idWidget, idFeatured}},
		{"exclude as string", []interface{}{"exclude", "2"}, []uint{idWidget, idFeatured}},
		{"offset", []interface{}{"offset", 1}, []uint{idWidget, idFeatured}},
		{"meta", []interface{}{"meta_key", "featured", "meta_value", "1"}, []uint{idFeatured}},
	} {
		got := viewIDs(b.contentListFunc(c, "product", 10, tc.args...))
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestContentListBadArgumentsReturnEmptyAndWarn(t *testing.T) {
	b, _ := dataFuncTheme(t)
	warnings := captureDataWarnings(t)
	c := dataRequest("en")
	cases := []struct {
		limit interface{}
		args  []interface{}
		want  string
	}{
		{0, nil, "limit must be a positive integer"},
		{"six", nil, "limit must be a positive integer"},
		{nil, nil, "limit must be a positive integer"},
		{3, []interface{}{"order"}, "options must be key/value pairs"},
		{3, []interface{}{"color", "red"}, "unknown option"},
		{3, []interface{}{"order", "title", "order", "title"}, "option given twice"},
		{3, []interface{}{"order", "id desc"}, "order must be a sortable field with an optional asc or desc"},
		{3, []interface{}{"order", "title; drop table x"}, "order must be a sortable field with an optional asc or desc"},
		{3, []interface{}{"taxonomy", "product_cat"}, "taxonomy and term must be given together"},
		{3, []interface{}{"taxonomy", "category", "term", "news"}, "taxonomy is not declared on the content type"},
		{3, []interface{}{"exclude", -1}, "exclude must be a content ID"},
		{3, []interface{}{"offset", 51}, "offset must be between 0 and 50"},
		{3, []interface{}{"meta_key", "featured"}, "meta_key and meta_value must be given together"},
	}
	for _, tc := range cases {
		if got := b.contentListFunc(c, "product", tc.limit, tc.args...); len(got) != 0 {
			t.Errorf("limit %v args %v returned %v", tc.limit, tc.args, viewIDs(got))
		}
	}
	seen := map[string]bool{}
	for _, w := range *warnings {
		seen[w] = true
	}
	for _, tc := range cases {
		if !seen["contentList: "+tc.want] {
			t.Errorf("missing warning %q in %v", tc.want, *warnings)
		}
	}
}

func TestContentListLowersLargeLimits(t *testing.T) {
	b, db := dataFuncTheme(t)
	for i := 0; i < DataFuncMaxItems+5; i++ {
		row := content.Content{Type: "post", Status: content.StatusPublished, Title: fmt.Sprint("bulk ", i), Slug: fmt.Sprint("bulk-", i)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	warnings := captureDataWarnings(t)
	got := b.contentListFunc(dataRequest(""), "post", "60")
	if len(got) != DataFuncMaxItems {
		t.Fatalf("got %d items, want %d", len(got), DataFuncMaxItems)
	}
	if len(*warnings) != 1 || (*warnings)[0] != "contentList: limit lowered to the maximum" {
		t.Fatalf("warnings = %v", *warnings)
	}
}

func TestDataFunctionBudgetCountsDistinctCallsPerRequest(t *testing.T) {
	b, db := dataFuncTheme(t)
	warnings := captureDataWarnings(t)
	queries := countQueries(t, db)
	c := dataRequest("en")

	first := b.contentListFunc(c, "product", 1)
	before := *queries
	again := b.contentListFunc(c, "product", "1")
	if *queries != before || !reflect.DeepEqual(first, again) {
		t.Fatal("an identical call ran queries again instead of reusing the result")
	}
	for i := 2; i <= DataFuncMaxCalls; i++ {
		if got := b.contentListFunc(c, "product", i); len(got) == 0 {
			t.Fatalf("call %d returned nothing", i)
		}
	}
	if got := b.contentListFunc(c, "post", 1); got != nil {
		t.Fatalf("call %d returned %v, want nothing", DataFuncMaxCalls+1, viewIDs(got))
	}
	if got := b.taxonomyTermsFunc(c, "category"); got != nil {
		t.Fatal("the budget is shared by all data functions")
	}
	if got := b.contentListFunc(c, "product", 3); len(got) == 0 {
		t.Fatal("a cached call must still answer after the budget is spent")
	}
	if !reflect.DeepEqual(*warnings, []string{"contentList: call budget exhausted", "taxonomyTerms: call budget exhausted"}) {
		t.Fatalf("warnings = %v", *warnings)
	}
	if got := b.contentListFunc(dataRequest("en"), "post", 1); len(got) == 0 {
		t.Fatal("a new request must start with a fresh budget")
	}
}

func TestDataFunctionsRunAFixedNumberOfQueries(t *testing.T) {
	b, db := dataFuncTheme(t)
	queries := countQueries(t, db)
	c := dataRequest("en")
	for _, tc := range []struct {
		name string
		call func()
		max  int
	}{
		{"contentList", func() { b.contentListFunc(c, "product", 50) }, 3},
		{"contentList with taxonomy", func() { b.contentListFunc(c, "post", 50, "taxonomy", "category", "term", "news") }, 5},
		{"contentBySlug", func() { b.contentBySlugFunc(c, "product", "widget") }, 3},
		{"taxonomyTerms", func() { b.taxonomyTermsFunc(c, "category") }, 2},
	} {
		before := *queries
		tc.call()
		if n := *queries - before; n > tc.max || n == 0 {
			t.Errorf("%s ran %d queries, want 1–%d", tc.name, n, tc.max)
		}
	}
}

func TestContentBySlug(t *testing.T) {
	b, _ := dataFuncTheme(t)
	warnings := captureDataWarnings(t)
	en, zh := dataRequest("en"), dataRequest("zh")

	if view := b.contentBySlugFunc(en, "product", "widget"); view == nil || view["ID"] != uint(idWidget) || view["Title"] != "Widget" {
		t.Fatalf("en widget = %v", view)
	}
	if view := b.contentBySlugFunc(zh, "product", "widget"); view == nil || view["ID"] != uint(idOtherLang) {
		t.Fatalf("zh widget = %v, want the zh row with the same slug", view)
	}
	for _, slug := range []string{"draft", "scheduled", "deleted", "trashed", "missing"} {
		if view := b.contentBySlugFunc(en, "product", slug); view != nil {
			t.Errorf("%s returned %v", slug, view["ID"])
		}
	}
	if view := b.contentBySlugFunc(en, "contact_message", "message"); view != nil {
		t.Fatal("a type without a public route was readable")
	}
	if view := b.contentBySlugFunc(en, "page", " "); view != nil {
		t.Fatal("an empty slug returned a row")
	}
	if !reflect.DeepEqual(*warnings, []string{
		"contentBySlug: content type is not registered or has no public route",
		"contentBySlug: slug must not be empty",
	}) {
		t.Fatalf("warnings = %v", *warnings)
	}
}

func TestTaxonomyTerms(t *testing.T) {
	b, _ := dataFuncTheme(t)
	warnings := captureDataWarnings(t)
	c := dataRequest("en")
	names := func(views []map[string]interface{}) string {
		out := []string{}
		for _, view := range views {
			out = append(out, fmt.Sprintf("%v(%v)", view["Name"], view["Count"]))
		}
		return strings.Join(out, ",")
	}
	if got := names(b.taxonomyTermsFunc(c, "category")); got != "News(2),Empty(0)" {
		t.Fatalf("categories = %s (zh rows must be out of scope)", got)
	}
	if got := names(b.taxonomyTermsFunc(c, "category", "hide_empty", "true", "order", "name")); got != "News(2)" {
		t.Fatalf("non-empty categories by name = %s", got)
	}
	if got := names(b.taxonomyTermsFunc(c, "category", "limit", 1, "order", "name")); got != "Empty(0)" {
		t.Fatalf("first category by name = %s", got)
	}
	view := b.taxonomyTermsFunc(c, "tag")[0]
	if view["Slug"] != "hot" || view["Taxonomy"] != "tag" || view["URL"] != "/tag/hot" {
		t.Fatalf("tag view = %v", view)
	}
	for _, bad := range [][]interface{}{{"order", "random"}, {"hide_empty", "maybe"}, {"limit", 0}} {
		if got := b.taxonomyTermsFunc(c, "category", bad...); got != nil {
			t.Errorf("%v returned %s", bad, names(got))
		}
	}
	if got := b.taxonomyTermsFunc(c, "genre"); got != nil {
		t.Fatal("an unregistered taxonomy was listed")
	}
	sort.Strings(*warnings)
	if !reflect.DeepEqual(*warnings, []string{
		"taxonomyTerms: hide_empty must be true or false",
		"taxonomyTerms: limit must be a positive integer",
		"taxonomyTerms: order must be count or name",
		"taxonomyTerms: taxonomy is not registered",
	}) {
		t.Fatalf("warnings = %v", *warnings)
	}
}

func TestDataFunctionsNeedTheRequest(t *testing.T) {
	b, _ := dataFuncTheme(t)
	warnings := captureDataWarnings(t)
	if got := b.contentListFunc(nil, "product", 3); got != nil {
		t.Fatal("contentList without a request returned rows")
	}
	if got := b.contentBySlugFunc(map[string]interface{}{"Title": "no ctx"}, "page", "about"); got != nil {
		t.Fatal("contentBySlug without a request returned a row")
	}
	if got := b.taxonomyTermsFunc((*gin.Context)(nil), "category"); got != nil {
		t.Fatal("taxonomyTerms with a nil request returned rows")
	}
	if len(*warnings) != 3 {
		t.Fatalf("warnings = %v", *warnings)
	}
}

func TestDataFunctionsInTemplates(t *testing.T) {
	b, _ := dataFuncTheme(t)
	c := dataRequest("en")
	data := map[string]interface{}{"Ctx": c, "Settings": map[string]string{"home_products_max": "2"}}
	out := renderWith(t, b, `
{{- range contentList .Ctx "product" (settingOr .Settings "home_products_max" "6")}}[{{.Title}} {{.URL}}]{{end -}}
{{- with contentBySlug $ "page" "about"}}{{safeHTML .Content}}{{end -}}
{{- range taxonomyTerms .Ctx "tag"}}#{{.Name}};{{end -}}
{{- range contentList .Ctx "post" 5}}({{.Title}}:{{range index .Terms "category"}}{{.Slug}}{{end}}){{end -}}
{{- with contentBySlug .Ctx "page" "missing"}}SHOULD NOT RENDER{{end -}}
{{- range contentList .Ctx "nope" 3}}SHOULD NOT RENDER{{end -}}`, data)
	want := `[Gadget /products/gadget][Widget /products/widget]<p>About us</p>#Hot;(New post:news)(Old post:news)`
	if out != want {
		t.Fatalf("rendered\n%s\nwant\n%s", out, want)
	}
	// Inside range, . is the item; $ still reaches the page data.
	out = renderWith(t, b, `{{range contentList .Ctx "post" 1}}{{range contentList $ "product" 1 "exclude" .ID}}{{.Title}}{{end}}{{end}}`, data)
	if out != "Gadget" {
		t.Fatalf("nested call rendered %q", out)
	}
}

// A theme function with a data function's name must not replace it: the core
// version enforces visibility and the budget.
func TestThemesCannotReplaceDataFunctions(t *testing.T) {
	b, _ := dataFuncTheme(t)
	b.customFuncMap = template.FuncMap{
		"contentList": func(...interface{}) string { return "theme" },
	}
	out := renderWith(t, b, `{{range contentList .Ctx "product" 1}}{{.Title}}{{end}}`, map[string]interface{}{"Ctx": dataRequest("en")})
	if out != "Gadget" {
		t.Fatalf("rendered %q, want the core contentList", out)
	}
}

func TestCommonFuncMapStubsLetTemplatesParseWithoutAnEngine(t *testing.T) {
	tmpl, err := template.New("pkg").Funcs(CommonFuncMap()).Parse(
		`{{range contentList .Ctx "product" 3 "order" "title"}}x{{end}}{{with contentBySlug .Ctx "page" "about"}}x{{end}}{{range taxonomyTerms .Ctx "tag" "limit" 5}}x{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, map[string]interface{}{}); err != nil || out.Len() != 0 {
		t.Fatalf("stub render = %q, %v", out.String(), err)
	}
}

// The batch view builder must produce what the old per-item builder did:
// GetMeta for meta, and the category and tag rows per item.
func TestContentViewsMatchPerItemLookupsWithFewerQueries(t *testing.T) {
	b, db := dataFuncTheme(t)
	c := dataRequest("en")
	items, err := content.NewQuery(content.ScopedDB(c, db)).Published().Types([]string{"product", "post"}).OrderStable("published_at", "DESC").Get()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("fixture returned %d items", len(items))
	}
	queries := countQueries(t, db)
	views := b.contentViews(c, items)
	if *queries != 2 {
		t.Fatalf("contentViews ran %d queries for %d items, want 2", *queries, len(items))
	}
	for i, item := range items {
		meta, _ := b.App.ContentRepo().GetMeta(item.ID)
		if !reflect.DeepEqual(views[i]["Meta"], meta) {
			t.Errorf("%d: Meta = %v, want %v", item.ID, views[i]["Meta"], meta)
		}
		for _, tax := range []string{"category", "tag"} {
			rows, _ := b.App.TaxonomyRepo().GetContentTaxonomiesContext(taxonomy.RequestContext(c), item.ID, tax)
			key := map[string]string{"category": "Categories", "tag": "Tags"}[tax]
			if want := taxonomyViews(c, b.App, rows); !reflect.DeepEqual(views[i][key], want) {
				t.Errorf("%d: %s = %v, want %v", item.ID, key, views[i][key], want)
			}
		}
		if item.ID == idWidget {
			if views[i]["price"] != "10" || views[i]["Price"] != "10" || views[i]["URL"] != "/products/widget" {
				t.Errorf("widget view lost flattened meta or URL: %v", views[i])
			}
			terms, _ := views[i]["Terms"].(map[string][]map[string]interface{})
			if len(terms["product_cat"]) != 1 || terms["product_cat"][0]["Slug"] != "widgets" {
				t.Errorf("widget Terms = %v", views[i]["Terms"])
			}
		}
	}
	if got := b.contentViews(c, nil); got == nil || len(got) != 0 {
		t.Fatalf("no items: %v", got)
	}
}

// Scheduled content must not be reachable by URL before its publish time.
func TestRenderSingleHidesScheduledContent(t *testing.T) {
	b, _ := dataFuncTheme(t)
	for slug, want := range map[string]int{"scheduled": http.StatusNotFound, "draft": http.StatusNotFound, "widget": http.StatusOK} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/products/"+slug, nil)
		b.renderSingle(c, &rewrite.ResolvedRoute{ContentType: "product", Slug: slug})
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d", slug, rec.Code, want)
		}
	}
}
