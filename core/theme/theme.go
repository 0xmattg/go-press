package theme

import (
	"html/template"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"go-press/core/content"
	"go-press/core/hook"
	coreI18n "go-press/core/i18n"
	"go-press/core/mail"
	coreMedia "go-press/core/media"
	"go-press/core/menu"
	"go-press/core/option"
	"go-press/core/rewrite"
	"go-press/core/taxonomy"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// App is the narrow interface through which themes access engine capabilities.
//
// Themes should depend on App instead of *core.Engine. That keeps theme code
// isolated from private engine lifecycle details while still exposing the
// repositories, registry, rewrite/SEO services, menu store, media repository,
// i18n manager, and hook bus required for rendering.
type App interface {
	// Database returns the underlying gorm.DB for content queries.
	Database() *gorm.DB

	// ContentRepo returns the content repository for CRUD operations.
	ContentRepo() *content.Repository

	// TaxonomyRepo returns the taxonomy repository.
	TaxonomyRepo() *taxonomy.Repository

	// ContentRegistry returns the content type / taxonomy registry.
	ContentRegistry() *content.Registry

	// Options returns the global option store.
	OptionsStore() *option.Store

	// RewriteEngine returns the URL → ContentType resolver.
	RewriteEngine() *rewrite.Engine

	// SEOBuilder returns the SEO metadata generator.
	SEOBuilder() *rewrite.SEOBuilder

	// MenuStore returns the menu store for accessing registered menus.
	MenuStore() *menu.Store

	// MediaRepo returns the media repository for responsive image helpers.
	MediaRepo() *coreMedia.Repository

	// I18nManager returns the core i18n manager for translations.
	I18nManager() *coreI18n.Manager

	// HookBus returns the core hook bus for theme/plugin extension points.
	HookBus() *hook.Bus

	// MailSender returns the configured core mail sender for theme-owned
	// workflows that need to trigger notification emails.
	MailSender() mail.Sender

	// SiteLocation returns the configured site timezone location used for
	// public date formatting.
	SiteLocation() *time.Location
}

// Theme is the runtime contract every GoPress theme must implement.
//
// Core owns request dispatch and calls ServeHTTP for front-end paths after
// admin, API, static, and system routes have been handled. Themes may implement
// this directly, but most should embed BaseTheme and focus on data assembly,
// templates, static assets, and optional custom routes.
type Theme interface {
	// Metadata
	Name() string
	Version() string
	Description() string
	Author() string

	// Lifecycle
	Setup(app App)            // Register menu locations and theme runtime hooks
	ServeHTTP(c *gin.Context) // Handle a front-end request (internal routing)

	// Templates
	TemplateFuncs() template.FuncMap
	TemplateDir() string
	StaticDir() string
}

// DemoDataProvider is an optional interface that themes can implement
// to supply bundled demo/seed data for one-click import from the admin panel.
type DemoDataProvider interface {
	// DemoSeedPath returns the absolute path to the theme's seed.toml file.
	// Return "" if no demo data is available.
	DemoSeedPath() string
}

// SettingsProvider is an optional interface that themes can implement
// to supply a custom admin settings page for theme-specific configuration.
type SettingsProvider interface {
	// SettingsTemplatePath returns the absolute path to the theme's admin
	// settings template file. Return "" if no settings page is available.
	SettingsTemplatePath() string
}

// Config holds the [theme] metadata parsed from theme.toml.
type Config struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
	Author      string `toml:"author"`
	Screenshot  string `toml:"screenshot"`
}

// FileConfig is the complete theme.toml model used by core runtime registration.
//
// ContentTypes and MenuLocations declared here are framework-visible contract:
// core uses them for registry setup, admin screens, rewrites, REST exposure,
// menu management, and docs. Theme Go code should not duplicate those
// definitions unless it needs additional runtime-only behavior.
type FileConfig struct {
	Theme         Config               `toml:"theme"`
	ContentTypes  []ContentTypeConfig  `toml:"content_types"`
	MenuLocations []MenuLocationConfig `toml:"menu_locations"`
}

// ContentTypeConfig maps a [[content_types]] entry in theme.toml.
//
// It is converted into content.ContentTypeDef during theme activation. Fields
// intentionally mirror the content registry shape while keeping TOML-specific
// naming such as rewrite_slug.
type ContentTypeConfig struct {
	Name            string                 `toml:"name"`
	Label           string                 `toml:"label"`
	LabelPlural     string                 `toml:"label_plural"`
	ArchiveTitleKey string                 `toml:"archive_title_key"`
	Supports        []string               `toml:"supports"`
	MetaFields      []content.MetaFieldDef `toml:"meta_fields"`
	Taxonomies      []string               `toml:"taxonomies"`
	HasArchive      bool                   `toml:"has_archive"`
	RewriteSlug     string                 `toml:"rewrite_slug"`
	Templates       TemplateConfig         `toml:"templates"`
	MenuIcon        string                 `toml:"menu_icon"`
	MenuOrder       int                    `toml:"menu_order"`
}

// TemplateConfig optionally maps a content type to existing page templates.
type TemplateConfig struct {
	Archive string `toml:"archive"`
	Single  string `toml:"single"`
}

// MenuLocationConfig maps a [[menu_locations]] entry in theme.toml.
type MenuLocationConfig struct {
	Name  string `toml:"name"`
	Label string `toml:"label"`
}

// LoadFileConfig parses theme.toml from a theme directory.
//
// The caller is responsible for deciding whether missing or invalid config is
// fatal. Engine activation treats it as fatal because content registration must
// be complete before a theme can serve requests.
func LoadFileConfig(themeDir string) (*FileConfig, error) {
	var cfg FileConfig
	if _, err := toml.DecodeFile(filepath.Join(themeDir, "theme.toml"), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// RegisterContentTypesFromConfig registers theme-declared content models.
//
// Core content types such as "post" and "contact_message" are ignored here
// because Engine registers them before theme config is applied. For each theme
// type, configured taxonomies are also extended so shared taxonomies like
// category and tag know which theme-specific types they apply to.
func RegisterContentTypesFromConfig(registry *content.Registry, cfg *FileConfig) {
	if registry == nil || cfg == nil {
		return
	}

	for i, ct := range cfg.ContentTypes {
		if ct.Name == "" {
			continue
		}
		if ct.Name == "post" || ct.Name == "contact_message" {
			continue
		}
		label := ct.Label
		if label == "" {
			label = ct.Name
		}
		labelPlural := ct.LabelPlural
		if labelPlural == "" {
			labelPlural = label
		}
		menuOrder := ct.MenuOrder
		if menuOrder == 0 {
			menuOrder = i + 1
		}

		registry.RegisterType(content.ContentTypeDef{
			Name:            ct.Name,
			Label:           label,
			LabelPlural:     labelPlural,
			ArchiveTitleKey: ct.ArchiveTitleKey,
			Supports:        append([]string(nil), ct.Supports...),
			MetaFields:      append([]content.MetaFieldDef(nil), ct.MetaFields...),
			Taxonomies:      append([]string(nil), ct.Taxonomies...),
			HasArchive:      ct.HasArchive,
			Rewrite:         content.RewriteRule{Slug: ct.RewriteSlug},
			Templates: content.TemplateDef{
				Archive: ct.Templates.Archive,
				Single:  ct.Templates.Single,
			},
			MenuIcon:  ct.MenuIcon,
			MenuOrder: menuOrder,
		})

		for _, taxName := range ct.Taxonomies {
			registry.AddContentTypeToTaxonomy(taxName, ct.Name)
		}
	}
}
