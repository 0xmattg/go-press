package rewrite

import (
	"encoding/xml"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"go-press/core/content"
	"go-press/core/taxonomy"
	"go-press/pkg/logger"
)

// SitemapAlternate represents an hreflang alternate for one sitemap URL.
//
// The XML tags intentionally match the sitemap extension namespace used by
// search engines for multilingual alternates.
type SitemapAlternate struct {
	Rel      string `xml:"rel,attr"`
	HrefLang string `xml:"hreflang,attr"`
	Href     string `xml:"href,attr"`
}

// SitemapURL represents one <url> entry in an XML sitemap.
//
// Alternates are optional and cause the root urlset to include the XHTML
// namespace. The rest of the fields map directly to sitemap.org elements.
type SitemapURL struct {
	Loc        string             `xml:"loc"`
	LastMod    string             `xml:"lastmod,omitempty"`
	ChangeFreq string             `xml:"changefreq,omitempty"`
	Priority   string             `xml:"priority,omitempty"`
	Alternates []SitemapAlternate `xml:"xhtml:link,omitempty"`
}

// SitemapURLSet is the root element of an XML sitemap.
type SitemapURLSet struct {
	XMLName    xml.Name     `xml:"urlset"`
	XMLNS      string       `xml:"xmlns,attr"`
	XMLNSXhtml string       `xml:"xmlns:xhtml,attr,omitempty"`
	URLs       []SitemapURL `xml:"url"`
}

// SitemapEntry is the high-level descriptor passed to transformers.
//
// ContentType is empty for archive, taxonomy, and homepage entries. Plugins can
// use ContentID and Slug to look up related rows, then mutate URL or append
// Extra entries without replacing the core generator.
type SitemapEntry struct {
	ContentType string       // e.g. "post", "product", "" for non-content URLs
	ContentID   uint         // 0 for non-content URLs
	Slug        string       // empty for non-content URLs
	URL         SitemapURL   // current URL (mutable)
	Extra       []SitemapURL // additional URLs (e.g. translated variants)
}

// SitemapTransformer lets plugins augment generated sitemap entries.
// Implementations can mutate entry.URL (e.g. add Alternates) and append
// extra URLs via entry.Extra (e.g. translated copies). The original URL
// is always emitted; Extra entries are emitted after it.
type SitemapTransformer func(entry *SitemapEntry)

// TransformerHandle is returned by AddTransformer. Pass it to RemoveTransformer
// to unregister — required so plugins can cleanly deactivate at runtime.
// The zero value is safe to pass to RemoveTransformer (no-op).
type TransformerHandle struct{ id uint64 }

// IsZero reports whether h is unset.
func (h TransformerHandle) IsZero() bool { return h.id == 0 }

type transformerEntry struct {
	id uint64
	fn SitemapTransformer
}

// SitemapGenerator produces XML sitemap entries from the active registry.
//
// It emits the homepage, content type archives, published single content rows,
// and taxonomy term archives. Plugins can register transformers to add hreflang
// alternates or additional derived URLs while keeping core generation stable.
type SitemapGenerator struct {
	siteURL      string
	registry     *content.Registry
	repo         *content.Repository
	taxRepo      *taxonomy.Repository
	rewrite      *Engine
	transformers []transformerEntry
	nextID       uint64
}

// NewSitemapGenerator creates a sitemap generator.
//
// siteURL should be an absolute public base URL without a trailing slash when
// possible. The generator does not own these dependencies; Engine wires the
// shared registry, repositories, and rewrite engine during startup.
func NewSitemapGenerator(siteURL string, registry *content.Registry, repo *content.Repository, rewrite *Engine) *SitemapGenerator {
	return &SitemapGenerator{
		siteURL:  siteURL,
		registry: registry,
		repo:     repo,
		rewrite:  rewrite,
	}
}

// SetTaxonomyRepo sets the taxonomy repository for generating taxonomy term URLs.
func (sg *SitemapGenerator) SetTaxonomyRepo(taxRepo *taxonomy.Repository) {
	sg.taxRepo = taxRepo
}

// AddTransformer registers a transformer that runs for every URL during Generate().
// Transformers are applied in registration order. Returns a handle that can be
// passed to RemoveTransformer; callers that never unregister may ignore it.
func (sg *SitemapGenerator) AddTransformer(fn SitemapTransformer) TransformerHandle {
	if fn == nil {
		return TransformerHandle{}
	}
	sg.nextID++
	id := sg.nextID
	sg.transformers = append(sg.transformers, transformerEntry{id: id, fn: fn})
	return TransformerHandle{id: id}
}

// RemoveTransformer unregisters a previously added transformer. A zero handle
// is a no-op. Safe to call multiple times.
func (sg *SitemapGenerator) RemoveTransformer(h TransformerHandle) {
	if h.IsZero() {
		return
	}
	filtered := sg.transformers[:0]
	for _, e := range sg.transformers {
		if e.id != h.id {
			filtered = append(filtered, e)
		}
	}
	sg.transformers = filtered
}

// SiteURL exposes the configured base URL (for transformers needing absolute URLs).
func (sg *SitemapGenerator) SiteURL() string { return sg.siteURL }

// Handler returns a Gin handler that serves /sitemap.xml.
func (sg *SitemapGenerator) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		urls := sg.Generate()

		set := SitemapURLSet{
			XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
			URLs:  urls,
		}
		if sg.hasAlternates(urls) {
			set.XMLNSXhtml = "http://www.w3.org/1999/xhtml"
		}

		c.Header("Content-Type", "application/xml; charset=utf-8")
		c.Header("Cache-Control", "public, max-age=3600")

		data, err := xml.MarshalIndent(set, "", "  ")
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Writer.Write([]byte(xml.Header))
		c.Writer.Write(data)
	}
}

func (sg *SitemapGenerator) hasAlternates(urls []SitemapURL) bool {
	for i := range urls {
		if len(urls[i].Alternates) > 0 {
			return true
		}
	}
	return false
}

// applyTransformers runs registered transformers and appends results to the URL list.
func (sg *SitemapGenerator) applyTransformers(urls *[]SitemapURL, entry SitemapEntry) {
	for _, e := range sg.transformers {
		e.fn(&entry)
	}
	*urls = append(*urls, entry.URL)
	if len(entry.Extra) > 0 {
		*urls = append(*urls, entry.Extra...)
	}
}

// Generate builds the sitemap URL list in memory.
//
// The generator limits single content items per content type to the latest 1000
// published rows. Large deployments should add sitemap index support before
// raising that limit.
func (sg *SitemapGenerator) Generate() []SitemapURL {
	var urls []SitemapURL

	// Homepage
	sg.applyTransformers(&urls, SitemapEntry{
		ContentType: "",
		URL: SitemapURL{
			Loc:        sg.siteURL + "/",
			ChangeFreq: "daily",
			Priority:   "1.0",
		},
	})

	// For each content type with archives
	for _, typeDef := range sg.registry.AllTypes() {
		if typeDef.HasArchive {
			archiveURL := sg.rewrite.BuildArchiveURL(typeDef.Name)
			sg.applyTransformers(&urls, SitemapEntry{
				ContentType: typeDef.Name,
				URL: SitemapURL{
					Loc:        sg.siteURL + archiveURL,
					ChangeFreq: "weekly",
					Priority:   "0.8",
				},
			})
		}

		// Individual items
		items, err := sg.repo.Query().
			Type(typeDef.Name).
			Published().
			OrderBy("published_at", "DESC").
			Limit(1000).
			Get()
		if err != nil {
			continue
		}
		for _, item := range items {
			itemURL := sg.rewrite.BuildURL(typeDef.Name, item.Slug)
			lastmod := item.UpdatedAt.Format(time.RFC3339)
			sg.applyTransformers(&urls, SitemapEntry{
				ContentType: typeDef.Name,
				ContentID:   item.ID,
				Slug:        item.Slug,
				URL: SitemapURL{
					Loc:        sg.siteURL + itemURL,
					LastMod:    lastmod,
					ChangeFreq: "monthly",
					Priority:   "0.6",
				},
			})
		}
	}

	// Taxonomy term archives (/category/xxx, /tag/xxx)
	if sg.taxRepo != nil {
		for _, taxDef := range sg.registry.AllTaxonomies() {
			items, err := sg.taxRepo.ListByTaxonomy(taxDef.Name)
			if err != nil {
				continue
			}
			for _, tax := range items {
				termURL := "/" + taxDef.Name + "/" + tax.Term.Slug
				sg.applyTransformers(&urls, SitemapEntry{
					ContentType: "",
					URL: SitemapURL{
						Loc:        sg.siteURL + termURL,
						ChangeFreq: "weekly",
						Priority:   "0.5",
					},
				})
			}
		}
	}

	return urls
}

// GenerateToFile writes the sitemap XML to the specified file path.
func (sg *SitemapGenerator) GenerateToFile(path string) (int, error) {
	urls := sg.Generate()

	set := SitemapURLSet{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}
	if sg.hasAlternates(urls) {
		set.XMLNSXhtml = "http://www.w3.org/1999/xhtml"
	}

	data, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		return 0, err
	}

	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	if _, err := f.WriteString(xml.Header); err != nil {
		return 0, err
	}
	if _, err := f.Write(data); err != nil {
		return 0, err
	}

	logger.Info("Sitemap generated", "path", path, "urls", len(urls))
	return len(urls), nil
}
