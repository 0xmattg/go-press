package monojournal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core"
	"github.com/0xmattg/go-press/core/comment"
	coreTheme "github.com/0xmattg/go-press/core/theme"
	"github.com/0xmattg/go-press/core/user"
)

func TestThemeConfigAndInterfaces(t *testing.T) {
	cfg, err := coreTheme.ParseFileConfig(themeTOML)
	if err != nil {
		t.Fatalf("parse theme.toml: %v", err)
	}
	if cfg.Theme.Name != "Mono Journal" || cfg.Theme.Version != "1.1.1" {
		t.Fatalf("unexpected metadata: %+v", cfg.Theme)
	}
	if cfg.Requires.Core == "" || len(cfg.Requires.Plugins) != 1 ||
		cfg.Requires.Plugins[0].Slug != "google-identity" || cfg.Requires.Plugins[0].Version != ">=1.0.1" {
		t.Fatalf("unexpected dependencies: %+v", cfg.Requires)
	}
	var value interface{} = &Theme{}
	if _, ok := value.(coreTheme.DemoDataProvider); !ok {
		t.Fatal("theme must implement DemoDataProvider")
	}
	if _, ok := value.(coreTheme.SettingsProvider); !ok {
		t.Fatal("theme must implement SettingsProvider")
	}
}

func TestAllPageTemplatesCompile(t *testing.T) {
	themeDir := filepath.Clean(".")
	theme := NewWithDB(nil, themeDir)
	if _, err := coreTheme.LoadAllPageBundles(theme); err != nil {
		t.Fatalf("compile templates: %v", err)
	}
}

func TestAdminSettingsTemplateCompiles(t *testing.T) {
	path := filepath.Join("templates", "admin", "theme_settings.tmpl")
	funcs := template.FuncMap{
		"X": func(_ interface{}, _ string, fallback string, args ...interface{}) string {
			if len(args) == 0 {
				return fallback
			}
			return fmt.Sprintf(fallback, args...)
		},
	}
	if _, err := template.New("settings").Funcs(funcs).ParseFiles(path); err != nil {
		t.Fatalf("compile admin settings template: %v", err)
	}
}

func TestHeaderExposesStandardNavigationHook(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("templates", "partials", "header.tmpl"))
	if err != nil {
		t.Fatalf("read header template: %v", err)
	}
	if !strings.Contains(string(data), `renderHook "header.nav.after"`) {
		t.Fatal("header must expose the standard header.nav.after extension point")
	}
}

func TestLoginModalContract(t *testing.T) {
	modal, err := os.ReadFile(filepath.Join("templates", "partials", "login-modal.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"isLoggedIn .Ctx", "loginProviders", "loginProviderURL .BeginURL $.Ctx", `role="dialog"`, `aria-modal="true"`, "data-login-close"} {
		if !strings.Contains(string(modal), want) {
			t.Errorf("login modal missing %q", want)
		}
	}
	header, err := os.ReadFile(filepath.Join("templates", "partials", "header.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(header), "data-login-open") {
		t.Error("header must open the login modal via data-login-open")
	}
	base, err := os.ReadFile(filepath.Join("templates", "layouts", "base.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(base), `{{template "loginModal" .}}`) {
		t.Error("base layout must include the login modal partial")
	}
}

func TestDemoSeedIsValidAndContainsNoAdmin(t *testing.T) {
	path := filepath.Join("demo", "data", "seed.toml")
	var seed core.SeedData
	if _, err := toml.DecodeFile(path, &seed); err != nil {
		t.Fatalf("parse demo seed: %v", err)
	}
	if seed.Admin.Username != "" || seed.Admin.Password != "" || seed.Admin.Email != "" {
		t.Fatal("demo seed must not contain administrator credentials")
	}
	if len(seed.Contents) < 7 || len(seed.Categories) < 3 || len(seed.Tags) < 4 {
		t.Fatalf("demo seed is incomplete: contents=%d categories=%d tags=%d", len(seed.Contents), len(seed.Categories), len(seed.Tags))
	}
	seen := map[string]bool{}
	for _, term := range append(seed.Categories, seed.Tags...) {
		if seen[term.Slug] {
			t.Fatalf("duplicate taxonomy slug %q", term.Slug)
		}
		seen[term.Slug] = true
	}
}

func TestLocalesAreValidJSON(t *testing.T) {
	paths := []string{"locales/en.json", "locales/zh.json", "locales/admin/en.json", "locales/admin/zh-CN.json"}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var values map[string]string
		if err := json.Unmarshal(data, &values); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if len(values) == 0 {
			t.Fatalf("locale %s is empty", path)
		}
		if path == "locales/en.json" && values["page_title_blog"] != "Writing" {
			t.Fatalf("English archive title = %q, want Writing", values["page_title_blog"])
		}
	}
}

func TestFrontendLocalesHaveMatchingKeys(t *testing.T) {
	read := func(path string) map[string]string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		values := map[string]string{}
		if err := json.Unmarshal(data, &values); err != nil {
			t.Fatal(err)
		}
		return values
	}
	en := read("locales/en.json")
	zh := read("locales/zh.json")
	for key := range en {
		if _, ok := zh[key]; !ok {
			t.Errorf("zh locale missing %q", key)
		}
	}
	for key := range zh {
		if _, ok := en[key]; !ok {
			t.Errorf("en locale missing %q", key)
		}
	}
}

func TestCommentAndProfileTemplatesRenderEscapedUserData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	theme := NewWithDB(nil, ".")
	bundle, err := coreTheme.LoadPageBundle(theme, []string{"single-post", "profile"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/blog/post", nil)
	commentData := map[string]interface{}{
		"Ctx": ctx, "Title": "Post", "ActivePage": "post", "Settings": map[string]string{},
		"Item":       PostView{ID: 7, Type: "post", Title: "Post", Slug: "post", Content: "<p>Body</p>", PublishedAt: &now},
		"ArchiveURL": "/blog", "Permalink": "/blog/post", "CommentsOpen": true, "CanComment": false, "CommentCount": int64(1),
		"Comments": []comment.View{{ID: 2, ContentID: 7, Body: "<script>alert(1)</script>", Status: comment.StatusApproved, CreatedAt: now, Author: comment.AuthorView{DisplayName: "Reader"}}},
	}
	var singleOut bytes.Buffer
	if err := bundle["single-post"].ExecuteTemplate(&singleOut, "base", commentData); err != nil {
		t.Fatalf("render single-post: %v", err)
	}
	if strings.Contains(singleOut.String(), "<script>alert(1)</script>") || !strings.Contains(singleOut.String(), "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("comment body must be escaped in the rendered template")
	}

	profileData := &ProfileData{
		PageData: PageData{Ctx: ctx, Title: "Profile", ActivePage: "profile", Settings: map[string]string{}},
		Account:  &user.PublicUserView{Username: "reader", DisplayName: "Reader", Email: `reader+<tag>@example.com`, Role: user.RoleSubscriber, CreatedAt: now},
	}
	var profileOut bytes.Buffer
	if err := bundle["profile"].ExecuteTemplate(&profileOut, "base", profileData); err != nil {
		t.Fatalf("render profile: %v", err)
	}
	if strings.Contains(profileOut.String(), "reader+<tag>@example.com") || !strings.Contains(profileOut.String(), "&lt;tag&gt;") {
		t.Fatalf("profile email must be escaped: %s", profileOut.String())
	}
}

func TestThemeHelpers(t *testing.T) {
	if got := journalPalette("<script>"); got != "ink" {
		t.Fatalf("palette allowlist failed: %q", got)
	}
	if got := readingTime(strings.Repeat("word ", 221)); got != 2 {
		t.Fatalf("readingTime = %d, want 2", got)
	}
}
