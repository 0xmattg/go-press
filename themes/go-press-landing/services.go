package gopresslanding

import (
	"strconv"
	"strings"

	"go-press/core"
	"go-press/core/content"
	"go-press/core/option"
	"go-press/core/rewrite"
	"go-press/core/taxonomy"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ======== View Models ========

// PageData is the base data shared by all pages.
type PageData struct {
	Title      string
	ActivePage string
	Settings   map[string]string
	Ctx        *gin.Context
	// SEO carries per-page SEO metadata for the seoHeadFor template helper.
	// Populated by PageService when the engine is available; left as the zero
	// value when no SEO model applies, in which case the layout falls back to
	// a plain meta description.
	SEO rewrite.SEOMeta
}

// FeatureView represents a feature card on the landing page.
type FeatureView struct {
	ID          uint
	Title       string
	Slug        string
	Description string
	Excerpt     string
	Icon        string
	SortOrder   int
}

// TestimonialView represents a customer testimonial.
type TestimonialView struct {
	ID       uint
	Title    string // author name
	Slug     string
	Content  string // the quote
	Excerpt  string
	ImageURL string
	Role     string
	Company  string
}

// PricingTier represents a pricing plan.
type PricingTier struct {
	Name     string
	Price    string
	Period   string
	Features []string
	CTA      string
	Popular  bool
}

// ======== Page Data Structs ========

// HomeData holds all data for the single-page landing.
type HomeData struct {
	PageData
	Features     []FeatureView
	Testimonials []TestimonialView
	Pricing      []PricingTier
}

// ======== PageService ========

// PageService assembles page data from the GoPress engine.
type PageService struct {
	db          *gorm.DB
	contentRepo *content.Repository
	taxRepo     *taxonomy.Repository
	options     *option.Store
	// seoBuilder is populated when constructed from a full Engine. May be nil
	// under NewPageServiceDB (CLI / tests), in which case the SEO helper
	// gracefully returns zero-value SEOMeta.
	seoBuilder *rewrite.SEOBuilder
}

// NewPageService creates a PageService backed by the full Engine.
func NewPageService(engine *core.Engine) *PageService {
	return &PageService{
		db:          engine.DB,
		contentRepo: engine.Content,
		taxRepo:     engine.Taxonomy,
		options:     engine.Options,
		seoBuilder:  engine.SEO,
	}
}

// NewPageServiceDB creates a PageService backed only by a DB connection.
func NewPageServiceDB(db *gorm.DB) *PageService {
	return &PageService{
		db:          db,
		contentRepo: content.NewRepository(db),
		taxRepo:     taxonomy.NewRepository(db),
		options:     option.NewStore(db),
	}
}

// buildHomeSEO mirrors BaseTheme's SEO injection for the landing page.
// Builds SEOMeta via the core SEOBuilder, then applies runtime option
// overrides so admin's site_name / site_description always win over the
// static cfg.Site.Name baked into the builder.
func (s *PageService) buildHomeSEO() rewrite.SEOMeta {
	if s.seoBuilder == nil {
		return rewrite.SEOMeta{}
	}
	seo := s.seoBuilder.ForHome(s.options.Get("site_description"))
	if name := s.options.Get("site_name"); name != "" && seo.OGType == "website" {
		seo.Title = name
		seo.OGTitle = name
	}
	if seo.Description == "" {
		if d := s.options.Get("site_description"); d != "" {
			seo.Description = d
			if seo.OGDescription == "" {
				seo.OGDescription = d
			}
		}
	}
	return seo
}

func (s *PageService) getSettings() map[string]string {
	return s.options.All()
}

func (s *PageService) buildPageData(title, activePage string) PageData {
	return PageData{
		Title:      title,
		ActivePage: activePage,
		Settings:   s.getSettings(),
	}
}

func (d *HomeData) SetCtx(c *gin.Context) {
	d.Ctx = c
}

// GetHomeData assembles all data for the landing page.
func (s *PageService) GetHomeData() (*HomeData, error) {
	settings := s.getSettings()

	// Determine max features
	maxFeatures := 6
	if maxStr, ok := settings["home_features_max"]; ok && maxStr != "" {
		if m, err := strconv.Atoi(maxStr); err == nil && m > 0 {
			maxFeatures = m
		}
	}

	// Load features
	features, _ := content.NewQuery(s.db).
		Type("feature").Published().
		OrderBy("sort_order", "ASC").
		Limit(maxFeatures).Get()

	featureViews := make([]FeatureView, len(features))
	for i, f := range features {
		meta, _ := s.contentRepo.GetMeta(f.ID)
		featureViews[i] = FeatureView{
			ID: f.ID, Title: f.Title, Slug: f.Slug,
			Description: f.Content, Excerpt: f.Excerpt,
			Icon: meta["icon"], SortOrder: f.SortOrder,
		}
	}

	// Load testimonials
	testimonials, _ := content.NewQuery(s.db).
		Type("testimonial").Published().
		OrderBy("sort_order", "ASC").
		Limit(3).Get()

	testimonialViews := make([]TestimonialView, len(testimonials))
	for i, t := range testimonials {
		meta, _ := s.contentRepo.GetMeta(t.ID)
		testimonialViews[i] = TestimonialView{
			ID: t.ID, Title: t.Title, Slug: t.Slug,
			Content: t.Content, Excerpt: t.Excerpt,
			ImageURL: t.ImageURL,
			Role:     meta["role"], Company: meta["company"],
		}
	}

	// Pricing tiers from settings (fallback to static defaults)
	pricing := s.buildPricingTiers(settings)

	data := &HomeData{
		PageData:     s.buildPageData("NovaPulse AI", "home"),
		Features:     featureViews,
		Testimonials: testimonialViews,
		Pricing:      pricing,
	}
	data.SEO = s.buildHomeSEO()
	return data, nil
}

// buildPricingTiers constructs pricing tiers from settings or defaults.
func (s *PageService) buildPricingTiers(settings map[string]string) []PricingTier {
	defaults := []PricingTier{
		{
			Name: "Starter", Price: "$0", Period: "/month",
			CTA: "Get Started Free",
			Features: []string{
				"10,000 predictions/month",
				"1 model deployment",
				"Community support",
				"Basic dashboard",
				"99.9% uptime SLA",
			},
		},
		{
			Name: "Pro", Price: "$299", Period: "/month",
			Popular: true, CTA: "Start Free Trial",
			Features: []string{
				"1,000,000 predictions/month",
				"10 model deployments",
				"Priority support",
				"Advanced analytics",
				"Custom domains",
				"99.99% uptime SLA",
			},
		},
		{
			Name: "Enterprise", Price: "Custom", Period: "",
			CTA: "Contact Sales",
			Features: []string{
				"Unlimited predictions",
				"Unlimited deployments",
				"24/7 dedicated support",
				"On-premise option",
				"SSO & SAML",
				"Custom SLA",
			},
		},
	}

	// Override from settings if available
	for i := 0; i < 3; i++ {
		prefix := "home_pricing_" + strconv.Itoa(i+1) + "_"
		if name, ok := settings[prefix+"name"]; ok && name != "" {
			defaults[i].Name = name
		}
		if price, ok := settings[prefix+"price"]; ok && price != "" {
			defaults[i].Price = price
		}
		if period, ok := settings[prefix+"period"]; ok {
			defaults[i].Period = period
		}
		if cta, ok := settings[prefix+"cta"]; ok && cta != "" {
			defaults[i].CTA = cta
		}
		if feats, ok := settings[prefix+"features"]; ok && feats != "" {
			lines := strings.Split(feats, "\n")
			var cleaned []string
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if l != "" {
					cleaned = append(cleaned, l)
				}
			}
			if len(cleaned) > 0 {
				defaults[i].Features = cleaned
			}
		}
		defaults[i].Popular = (i == 1) // Tier 2 is always popular
	}

	return defaults
}
