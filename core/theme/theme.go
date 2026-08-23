package theme

import (
	"html/template"
	"path/filepath"
	"time"

	"github.com/0xmattg/go-press/core/comment"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	coreI18n "github.com/0xmattg/go-press/core/i18n"
	"github.com/0xmattg/go-press/core/mail"
	coreMedia "github.com/0xmattg/go-press/core/media"
	"github.com/0xmattg/go-press/core/menu"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/rewrite"
	"github.com/0xmattg/go-press/core/taxonomy"
	"github.com/0xmattg/go-press/core/user"
	"github.com/BurntSushi/toml"

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

// PublicAuthApp is optional so lightweight theme test apps do not need to
// implement authentication. BaseTheme consumes it only for provider discovery.
type PublicAuthApp interface {
	PublicAuthProviders() []user.ProviderDescriptor
}

// CommentApp is optional so themes that render comments can consume the
// framework service without expanding the mandatory App contract.
type CommentApp interface {
	CommentService() *comment.Service
}

// PublicSubmissionApp is optional. Themes that declare front-end authorable
// content types use this generic service instead of reaching into core user or
// RBAC internals.
type PublicSubmissionApp interface {
	PublicSubmissionService() *content.PublicSubmissionService
}

// PublicAuthorizationApp exposes provider-neutral RBAC checks for front-end
// workflows. Themes must use this instead of inspecting concrete identity
// providers or core RBAC internals.
type PublicAuthorizationApp interface {
	CanPublicUser(c *gin.Context, resource, action string) bool
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

// LogoProvider is an optional interface that themes can implement to supply an
// inline SVG logo shown on the admin theme card. BaseTheme provides a default
// implementation that reads static/logo.svg from the theme directory, so most
// themes only need to drop that file in — no Go code required.
type LogoProvider interface {
	// LogoSVG returns the raw SVG markup for the theme logo, or "" if none.
	// Core sanitizes the markup before rendering it into admin pages.
	LogoSVG() string
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
	PageTemplates []PageTemplateConfig `toml:"page_templates"`
	Requires      RequiresConfig       `toml:"requires"`
}

// PageTemplateConfig maps a [[page_templates]] entry in theme.toml. It lets a
// theme advertise selectable per-page templates (WordPress-style "page
// templates") to the admin page editor. Template is the page-bundle name — a
// file under templates/pages/ without the ".tmpl" suffix — and Name is the
// human label shown in the editor dropdown. The selection is stored per page as
// the page_template content meta and consumed by BaseTheme.renderSingle.
type PageTemplateConfig struct {
	Name     string `toml:"name"`
	Template string `toml:"template"`
}

// RequiresConfig is the theme's declarative dependency block (theme.toml
// [requires]). It is a module/assembly-level contract resolved and enforced by
// core: the theme names the plugins it needs by slug (and optional version
// constraint), but never imports or calls them — interaction stays core-mediated.
// Dependency direction is theme→plugin only.
type RequiresConfig struct {
	// Core is an optional semver constraint on the GoPress core version, e.g. ">=0.7.0".
	Core string `toml:"core"`
	// Plugins the theme needs active to work correctly.
	Plugins []PluginRequirement `toml:"plugins"`
}

// PluginRequirement is one entry in [requires].plugins.
type PluginRequirement struct {
	// Slug is the required plugin's stable identifier (its Name()), e.g. "multi-language".
	Slug string `toml:"slug"`
	// Version is an optional semver constraint, e.g. ">=2.0.0" or "^1.0". Empty = any version.
	Version string `toml:"version"`
	// Optional, when true, downgrades an unmet dependency to a soft warning
	// rather than blocking theme activation.
	Optional bool `toml:"optional"`
}

// RequirementsProvider is the optional interface a theme implements to expose
// its declared dependencies. BaseTheme implements it by reading theme.toml, so
// themes get it for free.
type RequirementsProvider interface {
	Requires() RequiresConfig
}

// ContentTypeConfig maps a [[content_types]] entry in theme.toml.
//
// It is converted into content.ContentTypeDef during theme activation. Fields
// intentionally mirror the content registry shape while keeping TOML-specific
// naming such as rewrite_slug.
type ContentTypeConfig struct {
	Name             string                 `toml:"name"`
	Label            string                 `toml:"label"`
	LabelPlural      string                 `toml:"label_plural"`
	ArchiveTitleKey  string                 `toml:"archive_title_key"`
	Supports         []string               `toml:"supports"`
	MetaFields       []content.MetaFieldDef `toml:"meta_fields"`
	Taxonomies       []string               `toml:"taxonomies"`
	HasArchive       bool                   `toml:"has_archive"`
	Hierarchical     bool                   `toml:"hierarchical"`
	ReadOnly         bool                   `toml:"read_only"`
	RewriteSlug      string                 `toml:"rewrite_slug"`
	Rootless         bool                   `toml:"rootless"`
	Templates        TemplateConfig         `toml:"templates"`
	MenuIcon         string                 `toml:"menu_icon"`
	MenuOrder        int                    `toml:"menu_order"`
	PublicSubmission PublicSubmissionConfig `toml:"public_submission"`
}

// PublicSubmissionConfig maps [content_types.public_submission] from
// theme.toml into core's runtime policy.
type PublicSubmissionConfig struct {
	Enabled        bool     `toml:"enabled"`
	Roles          []string `toml:"roles"`
	DefaultStatus  string   `toml:"default_status"`
	AllowUpdateOwn bool     `toml:"allow_update_own"`
	AllowDeleteOwn bool     `toml:"allow_delete_own"`
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

// FileConfigProvider is implemented by themes (via BaseTheme) that carry their
// theme.toml parsed from an embedded copy, so core can read content types and
// dependencies from the binary rather than from disk at runtime.
type FileConfigProvider interface {
	FileConfig() *FileConfig
}

// ParseFileConfig parses theme.toml content (typically go:embed'd) into a
// FileConfig. Mirrors LoadFileConfig but from an in-memory string, so theme
// metadata/dependencies are baked into the binary — matching how plugins embed
// plugin.toml — instead of being read from disk at runtime.
func ParseFileConfig(content string) (*FileConfig, error) {
	var cfg FileConfig
	if _, err := toml.Decode(content, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
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
			Hierarchical:    ct.Hierarchical,
			ReadOnly:        ct.ReadOnly,
			Rewrite:         content.RewriteRule{Slug: ct.RewriteSlug, Rootless: ct.Rootless},
			Templates: content.TemplateDef{
				Archive: ct.Templates.Archive,
				Single:  ct.Templates.Single,
			},
			MenuIcon:  ct.MenuIcon,
			MenuOrder: menuOrder,
			PublicSubmission: content.PublicSubmissionPolicy{
				Enabled:        ct.PublicSubmission.Enabled,
				Roles:          append([]string(nil), ct.PublicSubmission.Roles...),
				DefaultStatus:  ct.PublicSubmission.DefaultStatus,
				AllowUpdateOwn: ct.PublicSubmission.AllowUpdateOwn,
				AllowDeleteOwn: ct.PublicSubmission.AllowDeleteOwn,
			},
		})

		for _, taxName := range ct.Taxonomies {
			registry.AddContentTypeToTaxonomy(taxName, ct.Name)
		}
	}
}
