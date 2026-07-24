package theme

import (
	"testing"

	"go-press/core/content"
)

func TestNewSEOPageServiceWiresFields(t *testing.T) {
	base := NewBasePageServiceDB(nil)
	s := NewSEOPageService(base, nil, nil, nil, nil)
	if s.Content == nil {
		t.Fatal("embedded base repositories should be wired")
	}
	if s.SEOBuilder != nil || s.Registry != nil || s.Hooks != nil || s.I18n != nil {
		t.Fatal("SEO fields should be nil when passed nil")
	}
}

func TestBuildSEOReturnsZeroWhenUnwired(t *testing.T) {
	// No SEOBuilder / Registry — every builder must degrade to zero-value SEOMeta
	// rather than panic (the DB-only / test path).
	s := NewSEOPageService(NewBasePageServiceDB(nil), nil, nil, nil, nil)
	if got := s.BuildHomeSEO(); got.Title != "" || got.Description != "" {
		t.Fatalf("BuildHomeSEO() = %+v, want zero value", got)
	}
	if got := s.BuildArchiveSEO("post"); got.Title != "" {
		t.Fatalf("BuildArchiveSEO() = %+v, want zero value", got)
	}
	if got := s.BuildContentSEO(&content.Content{}, "post"); got.Title != "" {
		t.Fatalf("BuildContentSEO() = %+v, want zero value", got)
	}
	if got := s.BuildContentSEO(nil, "post"); got.Title != "" {
		t.Fatalf("BuildContentSEO(nil) = %+v, want zero value", got)
	}
}
