package theme

import (
	"strings"
	"testing"

	coreMedia "go-press/core/media"
)

func TestAppendOriginalCandidateExtendsSrcsetToOriginalWidth(t *testing.T) {
	variants := []coreMedia.MediaVariant{
		{Path: "/static/uploads/sample-480w.jpg", Width: 480, Height: 320},
		{Path: "/static/uploads/sample-768w.jpg", Width: 768, Height: 512},
	}
	media := &coreMedia.Media{Path: "/static/uploads/sample.jpg", Width: 960, Height: 640}

	candidates := appendOriginalCandidate(candidatesFromVariants(variants), media, media.Path)
	got := srcsetCandidates(candidates)

	if !strings.Contains(got, "/static/uploads/sample.jpg 960w") {
		t.Fatalf("srcset %q does not include original image as the largest candidate", got)
	}
	if maxCandidateWidth(candidates) != 960 {
		t.Fatalf("max candidate width = %d, want 960", maxCandidateWidth(candidates))
	}
}

func TestWebPCandidatesMustCoverFallbackWidth(t *testing.T) {
	media := &coreMedia.Media{Path: "/static/uploads/sample.jpg", Width: 960, Height: 640}
	fallbacks := appendOriginalCandidate(candidatesFromVariants([]coreMedia.MediaVariant{
		{Path: "/static/uploads/sample-768w.jpg", Width: 768, Height: 512},
	}), media, media.Path)
	webp := candidatesFromVariants([]coreMedia.MediaVariant{
		{Path: "/static/uploads/sample-768w.webp", Width: 768, Height: 512},
	})

	if maxCandidateWidth(webp) >= maxCandidateWidth(fallbacks) {
		t.Fatalf("webp max width should be smaller than fallback max width")
	}

	webp = candidatesFromVariants([]coreMedia.MediaVariant{
		{Path: "/static/uploads/sample-768w.webp", Width: 768, Height: 512},
		{Path: "/static/uploads/sample-full.webp", Width: 960, Height: 640},
	})
	if maxCandidateWidth(webp) < maxCandidateWidth(fallbacks) {
		t.Fatalf("full webp candidate should cover fallback max width")
	}
}
