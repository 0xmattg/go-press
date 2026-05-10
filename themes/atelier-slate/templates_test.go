package atelierslate

import (
	"testing"

	"github.com/BurntSushi/toml"

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

func TestDemoSeedTermSlugsAreUnique(t *testing.T) {
	var data core.SeedData
	if _, err := toml.DecodeFile("demo/data/seed.toml", &data); err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, cat := range data.Categories {
		if owner, ok := seen[cat.Slug]; ok {
			t.Fatalf("duplicate term slug %q in category %q and %s", cat.Slug, cat.Name, owner)
		}
		seen[cat.Slug] = "category " + cat.Name
	}
	for _, tag := range data.Tags {
		if owner, ok := seen[tag.Slug]; ok {
			t.Fatalf("duplicate term slug %q in tag %q and %s", tag.Slug, tag.Name, owner)
		}
		seen[tag.Slug] = "tag " + tag.Name
	}
}

func TestAtelierSocialLinksUseConfiguredItems(t *testing.T) {
	links := atelierSocialLinks(map[string]string{
		"social_x":       "https://x.com/gopress",
		"social_wechat":  "gopress-dev",
		"social_discord": "https://discord.gg/gopress",
		"social_github":  "https://github.com/0xmattg/go-press",
	})
	if len(links) != 4 {
		t.Fatalf("expected 4 configured social links, got %d", len(links))
	}

	wantLabels := []string{"X", "WeChat", "Discord", "GitHub"}
	for i, want := range wantLabels {
		if links[i].Label != want {
			t.Fatalf("link %d label = %q, want %q", i, links[i].Label, want)
		}
	}
	if links[1].URL != "#" || links[1].Title != "gopress-dev" || links[1].External {
		t.Fatalf("wechat handle not normalized correctly: %+v", links[1])
	}
}
