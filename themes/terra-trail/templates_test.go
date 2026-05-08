package terratrail

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/gin-gonic/gin"

	"go-press/core"
)

func TestTemplatesCompile(t *testing.T) {
	theme := NewWithDB(nil, ".")
	if err := theme.handler.LoadPageTemplates(theme); err != nil {
		t.Fatal(err)
	}
}

func TestDemoSeedDoesNotDefineAdmin(t *testing.T) {
	var data core.SeedData
	if _, err := toml.DecodeFile("demo/data/seed.toml", &data); err != nil {
		t.Fatal(err)
	}
	if data.Admin.Username != "" || data.Admin.Password != "" {
		t.Fatal("theme demo seed must not define admin credentials")
	}
}

func TestDemoSeedParses(t *testing.T) {
	var data core.SeedData
	if _, err := toml.DecodeFile("demo/data/seed.toml", &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Contents) < 12 {
		t.Fatalf("expected rich demo content, got %d records", len(data.Contents))
	}
}

func TestDemoSeedTermSlugsAreUnique(t *testing.T) {
	var data core.SeedData
	if _, err := toml.DecodeFile("demo/data/seed.toml", &data); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]string)
	for _, cat := range data.Categories {
		if prev := seen[cat.Slug]; prev != "" {
			t.Fatalf("duplicate term slug %q in category %q, already used by %s", cat.Slug, cat.Name, prev)
		}
		seen[cat.Slug] = "category " + cat.Name
	}
	for _, tag := range data.Tags {
		if prev := seen[tag.Slug]; prev != "" {
			t.Fatalf("duplicate term slug %q in tag %q, already used by %s", tag.Slug, tag.Name, prev)
		}
		seen[tag.Slug] = "tag " + tag.Name
	}
}

func TestHomeTemplateRenders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	theme := NewWithDB(nil, ".")
	if err := theme.handler.LoadPageTemplates(theme); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	data := &HomeData{
		PageData: PageData{
			Title:      "Home",
			ActivePage: "home",
			Settings: map[string]string{
				"company_name":       "TerraTrail",
				"home_logo_text":     "TerraTrail",
				"home_hero_title":    "Your Next Adventure Awaits",
				"home_hero_subtitle": "Explore stunning destinations and unique experiences.",
			},
		},
		Products: []ProductView{{
			Title:   "Jaya Wijaya Mountain",
			Slug:    "jaya-wijaya-mountain",
			Excerpt: "A highland route with alpine views.",
		}},
		Showcases: []ShowcaseView{{
			Title:    "Rinjani Valley Road",
			Slug:     "rinjani-valley-road",
			Location: "Indonesia",
		}},
	}
	theme.handler.render(c, "home", data)
	if w.Code != 200 {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Your Next Adventure Awaits") {
		t.Fatal("expected rendered home hero title")
	}
	if !strings.Contains(w.Body.String(), "Every Step of the Way") {
		t.Fatal("expected rendered transport section")
	}
}
