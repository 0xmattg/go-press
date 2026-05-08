package theme

import (
	"fmt"
	"html/template"
	"net/url"
	"sort"
	"strings"

	coreMedia "go-press/core/media"
)

type imageAttrs struct {
	Class         string
	Sizes         string
	Loading       string
	FetchPriority string
}

func renderResponsiveImage(repo *coreMedia.Repository, src, alt string, attrs imageAttrs) template.HTML {
	if src == "" {
		return ""
	}
	loading := attrs.Loading
	if loading == "" {
		loading = "lazy"
	}
	attrs.Loading = loading
	if attrs.Sizes == "" {
		attrs.Sizes = "100vw"
	}

	mediaPath := normalizeMediaPath(src)
	if mediaPath == "" || repo == nil {
		return renderPlainImage(src, alt, attrs, 0, 0)
	}
	m, err := repo.FindByPath(mediaPath)
	if err != nil {
		return renderPlainImage(src, alt, attrs, 0, 0)
	}
	variants, err := repo.ListVariants(m.ID)
	if err != nil || len(variants) == 0 {
		return renderPlainImage(src, alt, attrs, m.Width, m.Height)
	}

	webp := filterVariants(variants, "webp")
	fallbacks := filterVariants(variants, "")
	fallbacks = filterOutFormat(fallbacks, "webp")
	webpCandidates := candidatesFromVariants(webp)
	fallbackCandidates := appendOriginalCandidate(candidatesFromVariants(fallbacks), m, src)
	fallbackSrc := src
	fallbackSrcSet := ""
	width, height := m.Width, m.Height
	if len(fallbackCandidates) > 0 {
		fallbackSrc, width, height = largestCandidate(fallbackCandidates, src, m.Width, m.Height)
		fallbackSrcSet = srcsetCandidates(fallbackCandidates)
	}

	img := imageTag(fallbackSrc, alt, attrs, width, height, fallbackSrcSet)
	// Avoid serving a narrower WebP set that would make browsers upscale while a larger original exists.
	if len(webpCandidates) == 0 || maxCandidateWidth(webpCandidates) < maxCandidateWidth(fallbackCandidates) {
		return template.HTML(img)
	}
	return template.HTML(fmt.Sprintf("<picture><source type=\"image/webp\" srcset=\"%s\" sizes=\"%s\">%s</picture>",
		escapeAttr(srcsetCandidates(webpCandidates)),
		escapeAttr(attrs.Sizes),
		img,
	))
}

func renderResponsivePreload(repo *coreMedia.Repository, src, sizes string) template.HTML {
	if src == "" {
		return ""
	}
	if sizes == "" {
		sizes = "100vw"
	}
	mediaPath := normalizeMediaPath(src)
	if mediaPath == "" || repo == nil {
		return template.HTML(fmt.Sprintf("<link rel=\"preload\" as=\"image\" href=\"%s\">", escapeAttr(src)))
	}
	m, err := repo.FindByPath(mediaPath)
	if err != nil {
		return template.HTML(fmt.Sprintf("<link rel=\"preload\" as=\"image\" href=\"%s\">", escapeAttr(src)))
	}
	variants, err := repo.ListVariants(m.ID)
	if err != nil || len(variants) == 0 {
		return template.HTML(fmt.Sprintf("<link rel=\"preload\" as=\"image\" href=\"%s\">", escapeAttr(src)))
	}
	webp := candidatesFromVariants(filterVariants(variants, "webp"))
	fallbacks := appendOriginalCandidate(candidatesFromVariants(filterOutFormat(filterVariants(variants, ""), "webp")), m, src)
	if len(webp) > 0 && maxCandidateWidth(webp) >= maxCandidateWidth(fallbacks) {
		href, _, _ := largestCandidate(webp, src, m.Width, m.Height)
		return template.HTML(fmt.Sprintf("<link rel=\"preload\" as=\"image\" href=\"%s\" imagesrcset=\"%s\" imagesizes=\"%s\" type=\"image/webp\">",
			escapeAttr(href),
			escapeAttr(srcsetCandidates(webp)),
			escapeAttr(sizes),
		))
	}
	if len(fallbacks) > 0 {
		href, _, _ := largestCandidate(fallbacks, src, m.Width, m.Height)
		return template.HTML(fmt.Sprintf("<link rel=\"preload\" as=\"image\" href=\"%s\" imagesrcset=\"%s\" imagesizes=\"%s\">",
			escapeAttr(href),
			escapeAttr(srcsetCandidates(fallbacks)),
			escapeAttr(sizes),
		))
	}
	return template.HTML(fmt.Sprintf("<link rel=\"preload\" as=\"image\" href=\"%s\">", escapeAttr(src)))
}

func renderPlainImage(src, alt string, attrs imageAttrs, width, height int) template.HTML {
	return template.HTML(imageTag(src, alt, attrs, width, height, ""))
}

func imageTag(src, alt string, attrs imageAttrs, width, height int, srcSet string) string {
	var b strings.Builder
	b.WriteString("<img src=\"")
	b.WriteString(escapeAttr(src))
	b.WriteString("\" alt=\"")
	b.WriteString(escapeAttr(alt))
	b.WriteString("\"")
	if srcSet != "" {
		b.WriteString(" srcset=\"")
		b.WriteString(escapeAttr(srcSet))
		b.WriteString("\" sizes=\"")
		b.WriteString(escapeAttr(attrs.Sizes))
		b.WriteString("\"")
	}
	if attrs.Class != "" {
		b.WriteString(" class=\"")
		b.WriteString(escapeAttr(attrs.Class))
		b.WriteString("\"")
	}
	if width > 0 && height > 0 {
		b.WriteString(fmt.Sprintf(" width=\"%d\" height=\"%d\"", width, height))
	}
	if attrs.FetchPriority != "" {
		b.WriteString(" fetchpriority=\"")
		b.WriteString(escapeAttr(attrs.FetchPriority))
		b.WriteString("\"")
	}
	b.WriteString(" loading=\"")
	b.WriteString(escapeAttr(attrs.Loading))
	b.WriteString("\" decoding=\"async\">")
	return b.String()
}

func normalizeMediaPath(src string) string {
	if strings.HasPrefix(src, "/static/uploads/") {
		return src
	}
	u, err := url.Parse(src)
	if err != nil {
		return ""
	}
	if u.Path != "" && strings.HasPrefix(u.Path, "/static/uploads/") {
		return u.Path
	}
	return ""
}

func filterVariants(items []coreMedia.MediaVariant, format string) []coreMedia.MediaVariant {
	out := make([]coreMedia.MediaVariant, 0, len(items))
	for _, item := range items {
		if item.Name == "thumb" || item.Width <= 0 {
			continue
		}
		if format == "" || item.Format == format {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Width < out[j].Width })
	return out
}

func filterOutFormat(items []coreMedia.MediaVariant, format string) []coreMedia.MediaVariant {
	out := items[:0]
	for _, item := range items {
		if item.Format != format {
			out = append(out, item)
		}
	}
	return out
}

type imageCandidate struct {
	Path   string
	Width  int
	Height int
}

func candidatesFromVariants(items []coreMedia.MediaVariant) []imageCandidate {
	out := make([]imageCandidate, 0, len(items))
	for _, item := range items {
		if item.Path == "" || item.Width <= 0 {
			continue
		}
		out = append(out, imageCandidate{
			Path:   item.Path,
			Width:  item.Width,
			Height: item.Height,
		})
	}
	sortCandidates(out)
	return out
}

func appendOriginalCandidate(items []imageCandidate, m *coreMedia.Media, src string) []imageCandidate {
	if m == nil || src == "" || m.Width <= 0 {
		return items
	}
	for _, item := range items {
		if item.Path == src || item.Width == m.Width {
			return items
		}
	}
	items = append(items, imageCandidate{
		Path:   src,
		Width:  m.Width,
		Height: m.Height,
	})
	sortCandidates(items)
	return items
}

func largestCandidate(items []imageCandidate, fallbackPath string, fallbackWidth, fallbackHeight int) (string, int, int) {
	if len(items) == 0 {
		return fallbackPath, fallbackWidth, fallbackHeight
	}
	item := items[len(items)-1]
	return item.Path, item.Width, item.Height
}

func maxCandidateWidth(items []imageCandidate) int {
	if len(items) == 0 {
		return 0
	}
	maxWidth := 0
	for _, item := range items {
		if item.Width > maxWidth {
			maxWidth = item.Width
		}
	}
	return maxWidth
}

func sortCandidates(items []imageCandidate) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Width == items[j].Width {
			return items[i].Path < items[j].Path
		}
		return items[i].Width < items[j].Width
	})
}

func srcsetCandidates(items []imageCandidate) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%s %dw", item.Path, item.Width))
	}
	return strings.Join(parts, ", ")
}

func escapeAttr(s string) string {
	return template.HTMLEscapeString(s)
}
