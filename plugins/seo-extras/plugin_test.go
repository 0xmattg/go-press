package seoextras

import (
	"testing"

	"github.com/0xmattg/go-press/core/rewrite"
)

func TestApplyMetaOverridesNormalizesSocialImageURL(t *testing.T) {
	tests := []struct {
		name    string
		image   string
		siteURL string
		want    string
	}{
		{
			name:    "root-relative upload",
			image:   "/static/uploads/2026/05/featured.jpg",
			siteURL: "https://example.com",
			want:    "https://example.com/static/uploads/2026/05/featured.jpg",
		},
		{
			name:    "path-relative upload",
			image:   "static/uploads/featured.jpg",
			siteURL: "https://example.com/",
			want:    "https://example.com/static/uploads/featured.jpg",
		},
		{
			name:    "absolute CDN URL",
			image:   "https://cdn.example.net/featured.jpg",
			siteURL: "https://example.com",
			want:    "https://cdn.example.net/featured.jpg",
		},
		{
			name:    "missing public site URL preserves override",
			image:   "/static/uploads/featured.jpg",
			siteURL: "",
			want:    "/static/uploads/featured.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyMetaOverrides(rewrite.SEOMeta{}, map[string]string{
				metaKeyImage: tt.image,
			}, tt.siteURL)
			if got.OGImage != tt.want {
				t.Fatalf("OGImage = %q, want %q", got.OGImage, tt.want)
			}
		})
	}
}
