// Sitemap integration for the multi-language plugin.
//
// The transformer hook is invoked by the core sitemap generator for every URL
// it produces. For URLs that correspond to a translatable Content item, we:
//
//  1. Look up the translation group (trid) and all sibling translations
//  2. Add <xhtml:link rel="alternate" hreflang="..."/> entries to the original
//     URL pointing at every translated copy (including itself + x-default)
//  3. Append additional <url> entries (with the same alternate set) for each
//     non-default-language translation, so search engines can discover them.
//
// Non-content URLs (homepage, archives, taxonomy archives) get one alternate
// per active language pointing at the language-prefixed variant of the same
// path, plus extra <url> entries for non-default languages.

package multilang

import (
	"strings"

	contentPkg "go-press/core/content"
	"go-press/core/rewrite"
)

// sitemapTransformer is registered with the core sitemap generator and runs
// once for every URL the generator emits.
func (p *Plugin) sitemapTransformer(entry *rewrite.SitemapEntry) {
	if p == nil || p.repo == nil || p.engine == nil || p.engine.Sitemap == nil {
		return
	}

	langs, err := p.repo.ActiveLanguages()
	if err != nil || len(langs) <= 1 {
		// Nothing to do when only one language is active
		return
	}

	defLang := p.getDefaultLang()
	siteURL := strings.TrimRight(p.engine.Sitemap.SiteURL(), "/")

	// Strip the site URL prefix to recover the path component we need to
	// re-prefix with /<lang>/ for the translated variants.
	originalPath := strings.TrimPrefix(entry.URL.Loc, siteURL)
	if originalPath == "" {
		originalPath = "/"
	}

	if entry.ContentType == "" || entry.ContentID == 0 {
		// Generic (non-content) URL — replicate per active non-default language.
		p.applyGenericAlternates(entry, langs, defLang, siteURL, originalPath)
		return
	}

	// Content URL — look up translation group.
	origTrans, err := p.repo.GetTranslation(entry.ContentID)
	if err != nil || origTrans == nil {
		// Not in a translation group — still annotate with x-default for SEO clarity.
		entry.URL.Alternates = append(entry.URL.Alternates, rewrite.SitemapAlternate{
			Rel: "alternate", HrefLang: "x-default", Href: entry.URL.Loc,
		}, rewrite.SitemapAlternate{
			Rel: "alternate", HrefLang: defLang, Href: entry.URL.Loc,
		})
		return
	}

	siblings, err := p.repo.GetTranslationsByTrid(origTrans.Trid)
	if err != nil || len(siblings) == 0 {
		return
	}

	// Build language→URL map by loading each translated content's slug.
	langToURL := make(map[string]string, len(siblings))
	for _, t := range siblings {
		c, err := p.engine.Content.FindByID(t.ContentID)
		if err != nil || c == nil {
			continue
		}
		// Only include published siblings; drafts shouldn't be advertised.
		if c.Status != contentPkg.StatusPublished {
			continue
		}
		basePath := p.engine.Rewrite.BuildURL(c.Type, c.Slug)
		var fullURL string
		if t.LanguageCode == defLang {
			fullURL = siteURL + basePath
		} else {
			fullURL = siteURL + "/" + t.LanguageCode + basePath
		}
		langToURL[t.LanguageCode] = fullURL
	}

	if len(langToURL) <= 1 {
		// Only one published variant — no alternates needed.
		return
	}

	alternates := buildAlternates(langToURL, defLang)
	entry.URL.Alternates = append(entry.URL.Alternates, alternates...)

	// Append translated URL entries (skip the default-language one — it's the
	// original entry we just annotated).
	for code, fullURL := range langToURL {
		if code == defLang {
			continue
		}
		extra := entry.URL // copy
		extra.Loc = fullURL
		extra.Alternates = alternates
		entry.Extra = append(entry.Extra, extra)
	}
}

// applyGenericAlternates handles non-content URLs (homepage, archives,
// taxonomy term pages) by replicating them for each active language.
func (p *Plugin) applyGenericAlternates(entry *rewrite.SitemapEntry, langs []Language, defLang, siteURL, originalPath string) {
	// Compute language-prefixed URL for each active language.
	langToURL := make(map[string]string, len(langs))
	for _, l := range langs {
		if l.Code == defLang {
			langToURL[l.Code] = siteURL + originalPath
		} else {
			langToURL[l.Code] = siteURL + "/" + l.Code + originalPath
		}
	}

	alternates := buildAlternates(langToURL, defLang)
	entry.URL.Alternates = append(entry.URL.Alternates, alternates...)

	for _, l := range langs {
		if l.Code == defLang {
			continue
		}
		extra := entry.URL
		extra.Loc = langToURL[l.Code]
		extra.Alternates = alternates
		entry.Extra = append(entry.Extra, extra)
	}
}

// buildAlternates returns hreflang entries (one per language plus x-default).
func buildAlternates(langToURL map[string]string, defLang string) []rewrite.SitemapAlternate {
	out := make([]rewrite.SitemapAlternate, 0, len(langToURL)+1)
	for code, href := range langToURL {
		out = append(out, rewrite.SitemapAlternate{
			Rel:      "alternate",
			HrefLang: code,
			Href:     href,
		})
	}
	if defURL, ok := langToURL[defLang]; ok {
		out = append(out, rewrite.SitemapAlternate{
			Rel:      "alternate",
			HrefLang: "x-default",
			Href:     defURL,
		})
	}
	return out
}
