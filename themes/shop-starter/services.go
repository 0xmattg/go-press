package shopstarter

import (
	"errors"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"go-press/core/content"
	"go-press/core/option"
	"go-press/core/rewrite"
	"go-press/core/taxonomy"
	coreTheme "go-press/core/theme"
)

const (
	defaultHeroImage = "/static/images/starter-hero.svg"
)

// HomeData is the small request-scoped model required by the one-page shop.
type HomeData struct {
	Ctx         *gin.Context `json:"-"`
	Title       string
	ActivePage  string
	SearchQuery string
	Settings    map[string]string
	Products    []content.Content
	Categories  []taxonomy.Taxonomy
	HeroImage   string
	SEO         rewrite.SEOMeta
}

// PageService composes storefront data only from Core-owned abstractions.
type PageService struct {
	coreTheme.SEOPageService
}

func NewPageService(app coreTheme.App) *PageService {
	if app == nil {
		return &PageService{}
	}
	base := coreTheme.NewBasePageService(
		app.Database(), app.ContentRepo(), app.TaxonomyRepo(), app.OptionsStore(),
	)
	return &PageService{SEOPageService: coreTheme.NewSEOPageService(
		base, app.SEOBuilder(), app.ContentRegistry(), app.HookBus(), app.I18nManager(),
	)}
}

func (s *PageService) ForRequest(c *gin.Context) *PageService {
	if s == nil {
		return nil
	}
	clone := *s
	clone.BasePageService = s.BasePageService.ForRequest(c)
	return &clone
}

// GetHomeData loads published products and the first few product categories.
func (s *PageService) GetHomeData() (*HomeData, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("shop-starter: page database is unavailable")
	}
	products, err := content.NewQuery(s.DB).
		Type("product").Published().
		OrderBy("sort_order", "ASC").
		Limit(8).Get()
	if err != nil {
		return nil, err
	}

	var categories []taxonomy.Taxonomy
	if s.Tax != nil {
		categories, err = s.Tax.ListByTaxonomy("product_cat")
		if err != nil {
			return nil, err
		}
		if len(categories) > 6 {
			categories = categories[:6]
		}
	}

	settings := s.Settings()
	if s.I18n != nil && s.ReqCtx != nil {
		settings = s.I18n.TranslateSettings(
			s.ReqCtx, settings, option.IsTranslatable, option.AllTranslatableKeys(),
		)
	}

	return &HomeData{
		Ctx:        s.ReqCtx,
		ActivePage: "home",
		Settings:   settings,
		Products:   products,
		Categories: categories,
		HeroImage:  safeThemeURL(settings["home_hero_image"], defaultHeroImage),
		SEO:        s.BuildHomeSEO(),
	}, nil
}

// safeThemeURL accepts same-site paths and absolute HTTP(S) URLs while
// rejecting executable, protocol-relative, and malformed values from settings.
func safeThemeURL(raw, fallback string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fallback
	}
	if parsed.Scheme == "" {
		if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
			return fallback
		}
		return value
	}
	if (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
		return value
	}
	return fallback
}
