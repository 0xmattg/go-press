package theme

import (
	"go-press/core/content"
	"go-press/core/hook"
	coreI18n "go-press/core/i18n"
	"go-press/core/rewrite"
)

// SEOPageService extends BasePageService with the per-page SEO-metadata assembly
// shared by content themes: it builds SEOMeta via the core SEOBuilder and then
// applies runtime option overrides so admin settings (site_name /
// site_description / site_icon) win over the static config baked into the
// builder. Themes that render SEO embed this instead of BasePageService:
//
//	type PageService struct {
//	    coreTheme.SEOPageService
//	}
//
//	func NewPageService(engine *core.Engine) *PageService {
//	    return &PageService{coreTheme.NewSEOPageService(
//	        coreTheme.NewBasePageService(engine.DB, engine.Content, engine.Taxonomy, engine.Options),
//	        engine.SEO, engine.Registry, engine.Hooks, engine.I18n)}
//	}
//
// The SEO fields may be nil (for NewBasePageServiceDB-backed CLI/test services),
// in which case the builders gracefully return zero-value SEOMeta.
type SEOPageService struct {
	BasePageService
	SEOBuilder *rewrite.SEOBuilder
	Registry   *content.Registry
	Hooks      *hook.Bus
	I18n       *coreI18n.Manager
}

// NewSEOPageService wires an SEO-capable page service over a base.
func NewSEOPageService(base BasePageService, seoBuilder *rewrite.SEOBuilder, registry *content.Registry, hooks *hook.Bus, i18n *coreI18n.Manager) SEOPageService {
	return SEOPageService{
		BasePageService: base,
		SEOBuilder:      seoBuilder,
		Registry:        registry,
		Hooks:           hooks,
		I18n:            i18n,
	}
}

// BuildHomeSEO returns SEO metadata for the site home page.
func (s *SEOPageService) BuildHomeSEO() rewrite.SEOMeta {
	if s.SEOBuilder == nil {
		return rewrite.SEOMeta{}
	}
	seo := s.SEOBuilder.ForHome(s.Options.Get("site_description"))
	s.applySEOOverrides(&seo)
	return seo
}

// BuildArchiveSEO returns SEO metadata for a content-type archive page.
func (s *SEOPageService) BuildArchiveSEO(typeName string) rewrite.SEOMeta {
	if s.SEOBuilder == nil || s.Registry == nil {
		return rewrite.SEOMeta{}
	}
	typeDef := s.Registry.GetType(typeName)
	if typeDef == nil {
		return rewrite.SEOMeta{}
	}
	seo := s.SEOBuilder.ForArchiveTitle(typeDef, LocalizedArchiveTitle(s.ReqCtx, s.I18n, typeDef))
	s.applySEOOverrides(&seo)
	return seo
}

// BuildContentSEO returns SEO metadata for a single content item, including any
// per-content meta overrides contributed through hooks.
func (s *SEOPageService) BuildContentSEO(item *content.Content, typeName string) rewrite.SEOMeta {
	if s.SEOBuilder == nil || s.Registry == nil || item == nil {
		return rewrite.SEOMeta{}
	}
	typeDef := s.Registry.GetType(typeName)
	if typeDef == nil {
		return rewrite.SEOMeta{}
	}
	seo := s.SEOBuilder.ForContent(item, typeDef)
	s.applySEOOverrides(&seo)
	ApplyContentMetaSEO(s.Hooks, s.Content, &seo, item)
	return seo
}

func (s *SEOPageService) applySEOOverrides(seo *rewrite.SEOMeta) {
	ApplySiteOptionOverridesFromOptionsForRequest(s.ReqCtx, s.Options, s.I18n, s.SEOBuilder, seo)
}
