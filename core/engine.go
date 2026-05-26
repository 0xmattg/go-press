package core

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go-press/config"
	"go-press/core/admin"
	"go-press/core/api"
	"go-press/core/cache"
	"go-press/core/content"
	"go-press/core/hook"
	coreI18n "go-press/core/i18n"
	"go-press/core/media"
	"go-press/core/menu"
	"go-press/core/option"
	"go-press/core/plugin"
	"go-press/core/rewrite"
	"go-press/core/taxonomy"
	coreTheme "go-press/core/theme"
	"go-press/core/user"
	"go-press/core/worker"
	"go-press/pkg/logger"
	"go-press/pkg/middleware"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// ThemeInfo is the normalized theme summary shown by admin screens.
//
// Engine builds this from registered theme instances rather than reading
// theme.toml directly so the admin view reflects the runtime theme contract:
// metadata methods, active slug, and optional capabilities such as settings or
// demo data.
type ThemeInfo struct {
	Name        string
	Slug        string
	Version     string
	Description string
	Author      string
	Active      bool
}

// Engine is the main GoPress application container.
//
// A single Engine instance owns the process-wide runtime state for one site:
// database access, content model registry, hooks, caches, workers, repositories,
// URL/SEO services, active theme, plugin manager, admin handler, and Gin router.
// Packages outside core should prefer the narrower interfaces exposed by their
// domain packages (for example theme.App or plugin.App) when possible.
type Engine struct {
	Config *config.Config
	DB     *gorm.DB

	// SiteDir is the directory containing the active site's config.toml.
	// Site-scoped generated files should be written under this directory, not
	// into the shared application root.
	SiteDir string

	// Core subsystems are long-lived services shared by admin, themes, plugins,
	// REST APIs, and front-end rendering.
	Hooks    *hook.Bus
	Registry *content.Registry
	Options  *option.Store
	Menus    *menu.Store
	RBAC     *user.RBAC
	Cache    *cache.Manager
	Workers  *worker.Pool
	Sched    *worker.Scheduler

	// URL / SEO services derive public routes, metadata, redirects, and sitemap
	// entries from the content registry and repositories.
	Rewrite   *rewrite.Engine
	Redirects *rewrite.RedirectManager
	SEO       *rewrite.SEOBuilder
	Sitemap   *rewrite.SitemapGenerator

	// Repositories are thin data access layers. Higher-level workflows should
	// still go through services where those exist, especially in admin code.
	Content  *content.Repository
	Taxonomy *taxonomy.Repository
	Users    *user.Repository
	Media    *media.Repository

	// Auth handles admin/session JWT concerns.
	Auth *user.Auth

	// I18n is always available. The multilang plugin can layer request language
	// detection and database overrides on top of this base manager.
	I18n *coreI18n.Manager

	// Theme and plugin runtime state. Themes are initialized once, while the
	// active theme slug can change at runtime through the admin panel.
	mu              sync.RWMutex
	themes          map[string]coreTheme.Theme // all initialized theme instances
	activeThemeName string                     // slug of the currently active theme
	PluginManager   *plugin.Manager

	// Admin is created after repositories and the registry are ready.
	Admin *admin.Handler

	// HTTP runtime. Router can be rebuilt when theme/plugin state changes.
	Router *gin.Engine
	server *http.Server

	// OnRouterRebuild is called after the router is rebuilt, for example after a
	// theme switch. cmd/server uses it to swap the active HTTP handler without
	// restarting the process.
	OnRouterRebuild func(http.Handler)
}

// SitePublicPath returns a path below the active site's public directory.
//
// Use this for site-scoped generated files such as sitemap.xml, robots.txt,
// llms.txt, or similar public artifacts. The directory is derived from the
// active config path so multiple sites can share one application root safely.
func (e *Engine) SitePublicPath(parts ...string) string {
	elems := append([]string{e.SiteDir, "public"}, parts...)
	return filepath.Join(elems...)
}

// New creates a new Engine with the given config and database.
//
// New constructs in-memory services and repositories but does not load themes,
// plugins, options, menus, redirects, or routes. Callers should follow the
// boot sequence in BuildAndBootstrap or explicitly call LoadAllThemes,
// SetupAdmin, Bootstrap, LoadAllPlugins, and SetupRouter in the same order.
func New(cfg *config.Config, db *gorm.DB) *Engine {
	// --- Cache: detect available backends, degrade gracefully ---
	var l1 cache.Cache
	var l2 cache.Cache

	// L1: always use in-process memory cache (10k entries)
	l1 = cache.NewMemoryCache(10000)
	logger.Info("Cache L1 (memory) enabled", "max_entries", 10000)

	// L2: try connecting to Redis if configured
	if cfg.Redis.Host != "" {
		addr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
		rc := cache.NewRedisCache(addr, cfg.Redis.Password, cfg.Redis.DB)
		if rc != nil {
			l2 = rc
			logger.Info("Cache L2 (Redis) enabled", "addr", addr)
		} else {
			logger.Info("Cache L2 (Redis) unavailable — running without distributed cache", "addr", addr)
		}
	} else {
		logger.Info("Cache L2 (Redis) not configured — running without distributed cache")
	}

	cacheMgr := cache.NewManager(l1, l2)

	// --- Worker pool ---
	wp := worker.NewPool(4)
	sched := worker.NewScheduler(wp)

	hookBus := hook.New()

	e := &Engine{
		Config: cfg,
		DB:     db,

		Hooks:    hookBus,
		Registry: content.NewRegistry(),
		Options:  option.NewStore(db),
		Menus:    menu.NewStore(db, hookBus),
		RBAC:     user.NewRBAC(),
		Cache:    cacheMgr,
		Workers:  wp,
		Sched:    sched,

		Content:  content.NewRepository(db),
		Taxonomy: taxonomy.NewRepository(db),
		Users:    user.NewRepository(db),
		Media:    media.NewRepository(db),

		themes:        make(map[string]coreTheme.Theme),
		PluginManager: plugin.NewManager(),
	}

	// URL / SEO (depends on Registry, created after it)
	e.Rewrite = rewrite.NewEngine(e.Registry)
	e.SEO = rewrite.NewSEOBuilder(cfg.Site.URL, cfg.Site.Name, e.Rewrite)
	e.Sitemap = rewrite.NewSitemapGenerator(cfg.Site.URL, e.Registry, e.Content, e.Rewrite)
	e.Sitemap.SetTaxonomyRepo(e.Taxonomy)

	// Initialize auth
	e.Auth = user.NewAuth(cfg.CMS.JWTSecret, cfg.CMS.JWTExpireHours, e.Users)

	// Initialize core i18n
	e.I18n = coreI18n.NewManager(cfg.Site.Language)

	return e
}

// LoadAllThemes initializes every registered theme factory and activates the
// theme specified by the options table, falling back to config.Site.Theme.
//
// Theme instances are kept in memory so switching themes later does not require
// re-running factory registration. The active theme's theme.toml content models
// are reloaded into the registry during activation.
func (e *Engine) LoadAllThemes() error {
	for name, factory := range AllThemeFactories() {
		themeDir := "themes/" + name
		t := factory(e, themeDir)
		e.themes[name] = t
		logger.Info("Theme registered", "theme", t.Name(), "slug", name)
	}

	// Determine active theme: options DB overrides config file
	active := e.Options.Get("active_theme")
	if active == "" {
		active = e.Config.Site.Theme
	}

	return e.activateTheme(active)
}

// activateTheme activates a theme by slug without persisting the choice.
//
// Activation rebuilds the content registry from core definitions plus the
// selected theme's theme.toml declarations, then calls Theme.Setup. This keeps
// stale content type and taxonomy declarations from previous themes out of the
// active runtime.
func (e *Engine) activateTheme(name string) error {
	theme, ok := e.themes[name]
	if !ok {
		return fmt.Errorf("theme %q not found in registered themes", name)
	}

	// Clear and re-register: core types first, then theme types
	e.Registry.Clear()
	e.registerCoreTypes()
	themeConfig, err := coreTheme.LoadFileConfig("themes/" + name)
	if err != nil {
		return fmt.Errorf("failed to load theme config for %q: %w", name, err)
	}
	coreTheme.RegisterContentTypesFromConfig(e.Registry, themeConfig)
	theme.Setup(e)

	e.mu.Lock()
	e.activeThemeName = name
	e.mu.Unlock()

	// Load theme's locale files for core i18n
	if e.I18n != nil {
		e.I18n.LoadThemeLocales("themes/" + name)
	}

	logger.Info("Theme activated", "theme", theme.Name(), "slug", name)
	return nil
}

// registerCoreTypes registers content types and taxonomies that are part of
// GoPress core and must survive theme switches.
//
// Theme activation clears the registry before re-registering these definitions,
// so anything listed here is treated as framework-level surface area rather
// than a theme-specific model.
func (e *Engine) registerCoreTypes() {
	e.Registry.RegisterType(content.ContentTypeDef{
		Name:        "post",
		Label:       "文章",
		LabelPlural: "文章列表",
		HasArchive:  true,
		Supports:    []string{"title", "content", "excerpt", "thumbnail", "publish_date"},
		Taxonomies:  []string{"category", "tag"},
		Rewrite:     content.RewriteRule{Slug: "blog"},
		MenuOrder:   100,
	})

	e.Registry.RegisterType(content.ContentTypeDef{
		Name:        "contact_message",
		Label:       "联系留言",
		LabelPlural: "联系留言",
		HasArchive:  false,
		MetaFields: []content.MetaFieldDef{
			{Key: "email", Label: "邮箱", Type: "string"},
			{Key: "phone", Label: "电话", Type: "string"},
		},
		MenuOrder: 110,
	})

	e.Registry.RegisterTaxonomy(content.TaxonomyDef{
		Name:         "category",
		Label:        "分类",
		LabelPlural:  "分类列表",
		ContentTypes: []string{"post"},
		Hierarchical: true,
	})

	e.Registry.RegisterTaxonomy(content.TaxonomyDef{
		Name:         "tag",
		Label:        "标签",
		LabelPlural:  "标签列表",
		ContentTypes: []string{"post"},
		Hierarchical: false,
	})
}

// SwitchTheme switches the active theme and persists the choice to the options table.
//
// The change takes effect immediately: the registry is rebuilt, page and
// fragment caches are flushed, the Gin router is rebuilt so dynamic admin routes
// match the new registry, and future front-end requests are dispatched to the
// new active theme.
func (e *Engine) SwitchTheme(name string) error {
	if err := e.activateTheme(name); err != nil {
		return err
	}
	// Flush page + fragment caches so the new theme renders fresh
	e.Cache.L1.DeleteByPrefix("page:")
	e.Cache.L2.DeleteByPrefix("page:")
	e.Cache.L1.DeleteByPrefix("frag:")
	e.Cache.L2.DeleteByPrefix("frag:")
	logger.Info("Caches flushed after theme switch", "theme", name)

	// Rebuild router so dynamic admin routes reflect the new registry
	e.SetupRouter()
	if e.OnRouterRebuild != nil {
		e.OnRouterRebuild(e.Router)
	}
	if e.server != nil {
		e.server.Handler = e.Router
	}

	return e.Options.Set("active_theme", name)
}

// ActiveTheme returns the currently active theme instance.
func (e *Engine) ActiveTheme() coreTheme.Theme {
	e.mu.RLock()
	name := e.activeThemeName
	e.mu.RUnlock()
	return e.themes[name]
}

// ActiveThemeName returns the slug of the currently active theme.
func (e *Engine) ActiveThemeName() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.activeThemeName
}

// AvailableThemes returns info about all registered themes.
func (e *Engine) AvailableThemes() []ThemeInfo {
	active := e.ActiveThemeName()
	infos := make([]ThemeInfo, 0, len(e.themes))
	for slug, t := range e.themes {
		infos = append(infos, ThemeInfo{
			Name:        t.Name(),
			Slug:        slug,
			Version:     t.Version(),
			Description: t.Description(),
			Author:      t.Author(),
			Active:      slug == active,
		})
	}
	return infos
}

// ThemeDemoSeedPath returns the seed.toml path for the given theme slug,
// or "" if the theme does not provide demo data.
func (e *Engine) ThemeDemoSeedPath(slug string) string {
	t, ok := e.themes[slug]
	if !ok {
		return ""
	}
	if dp, ok := t.(coreTheme.DemoDataProvider); ok {
		return dp.DemoSeedPath()
	}
	return ""
}

// IsThemeDemoImported checks the option store for a per-theme flag.
func (e *Engine) IsThemeDemoImported(slug string) bool {
	return e.Options.Get("demo_imported_"+slug) == "1"
}

// ThemeSettingsTemplatePath returns the admin settings template path for
// the given theme slug, or "" if the theme does not provide a settings page.
func (e *Engine) ThemeSettingsTemplatePath(slug string) string {
	t, ok := e.themes[slug]
	if !ok {
		return ""
	}
	if sp, ok := t.(coreTheme.SettingsProvider); ok {
		return sp.SettingsTemplatePath()
	}
	return ""
}

// ImportThemeDemoData seeds demo data for the specified theme.
//
// The seed operation intentionally replaces existing demo-managed content. It
// then marks the theme as imported, reloads options so seeded settings become
// visible immediately, and flushes caches so the next request renders from the
// fresh database state.
func (e *Engine) ImportThemeDemoData(slug string) error {
	seedPath := e.ThemeDemoSeedPath(slug)
	if seedPath == "" {
		return fmt.Errorf("theme %q does not provide demo data", slug)
	}
	if err := e.ForceSeedFromFile(seedPath); err != nil {
		return err
	}
	// Mark as imported so the UI disables the button
	_ = e.Options.Set("demo_imported_"+slug, "1")
	// Reload options into memory so settings from seed take effect
	e.Options.LoadAll()
	// Flush all caches so pages render fresh content
	e.Cache.Flush()
	logger.Info("Caches flushed after demo data import", "theme", slug)
	return nil
}

// LoadPlugin registers a plugin and activates it unless a persisted
// "plugin_active_<name> = false" option marks it as intentionally disabled.
// Missing option is treated as "active" to preserve first-run behavior.
func (e *Engine) LoadPlugin(p plugin.Plugin) {
	e.PluginManager.Register(p)
	if e.Options != nil && e.Options.Get("plugin_active_"+p.Name()) == "false" {
		logger.Info("Plugin registered but inactive (persisted state)",
			"plugin", p.Name(), "version", p.Version())
		return
	}
	e.PluginManager.Activate(p.Name(), e)
	logger.Info("Plugin activated", "plugin", p.Name(), "version", p.Version())
}

// LoadAllPlugins loads all auto-registered plugins (those registered via init() + RegisterPlugin).
func (e *Engine) LoadAllPlugins() {
	for name, factory := range AllPluginFactories() {
		logger.Info("Loading plugin", "name", name)
		factory(e)
	}
}

// Bootstrap loads runtime state after core, themes, and admin are configured.
//
// It refreshes memory-backed stores, creates the redirect manager, starts
// scheduled jobs, and fires the engine init hook. It does not build the router;
// SetupRouter is separate so callers can load plugins before final route
// registration.
func (e *Engine) Bootstrap() error {
	// 1. Load global options into memory
	e.Options.LoadAll()

	// 2. Load menus into memory
	e.Menus.LoadAll()

	// 3. Initialize redirect manager (auto-migrates table)
	e.Redirects = rewrite.NewRedirectManager(e.DB)
	if err := e.Redirects.Migrate(); err != nil {
		logger.Error("Failed to migrate redirects table", "error", err)
	}

	// 4. Register scheduled jobs
	e.Sched.AddJob("cache-cleanup", 10*time.Minute, func(ctx context.Context) error {
		// Periodic expired-entry eviction is handled by MemoryCache internally;
		// this job is a hook point for future cache warming or stats collection.
		logger.Info("Scheduled cache maintenance completed")
		return nil
	})
	e.Sched.Start()

	// 5. Fire init hook
	e.Hooks.DoAction(context.Background(), "engine.init")

	logger.Info("GoPress engine bootstrapped")
	return nil
}

// SetupRouter creates the Gin router with a front-controller dispatcher.
//
// Theme routes are not registered directly on Gin. Instead, a catch-all NoRoute
// handler delegates front-end requests to the active theme at runtime. This is
// what makes theme switching possible without restarting the process.
func (e *Engine) SetupRouter() *gin.Engine {
	if e.Config.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()

	// Global middleware
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.CacheControl())

	// Redirect middleware (301/302 from DB, in-memory lookup)
	if e.Redirects != nil {
		r.Use(e.Redirects.Middleware())
	}

	// Core i18n middleware (sets default localizer; multilang plugin may override via middleware.early)
	if e.I18n != nil {
		r.Use(e.I18n.Middleware())
	}

	// Page cache middleware (only effective when cache is available)
	// Fire early middleware hook (runs BEFORE page cache — for plugins like multilang)
	e.Hooks.DoAction(context.Background(), "middleware.early", r)

	r.Use(e.poweredByMiddleware())

	if !e.Cache.IsNoop() {
		r.Use(cache.PageCacheMiddleware(e.Cache, 10*time.Minute))
		logger.Info("Page cache middleware enabled")
	}

	// Health check (engine-level)
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"theme":  e.ActiveThemeName(),
			"cache":  !e.Cache.IsNoop(),
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	// XML Sitemap (shared generator on engine — plugins may register transformers)
	r.GET("/sitemap.xml", e.Sitemap.Handler())

	// robots.txt
	r.GET("/robots.txt", func(c *gin.Context) {
		c.Header("Content-Type", "text/plain")
		c.String(http.StatusOK, "User-agent: *\nAllow: /\nSitemap: %s/sitemap.xml\n", e.Config.Site.URL)
	})

	// All static files through a unified handler
	r.GET("/static/*filepath", e.serveStatic)

	// REST API (/api/v1/...)
	apiGroup := r.Group("/api/v1")
	apiGroup.Use(api.CORSMiddleware("*"))
	rl := api.NewRateLimiter(100, 1*time.Minute)
	apiGroup.Use(rl.Middleware())
	// API Key auth — pass-through when no keys configured
	apiKeyMap := make(map[string]bool, len(e.Config.CMS.APIKeys))
	for _, k := range e.Config.CMS.APIKeys {
		apiKeyMap[k] = true
	}
	apiGroup.Use(api.APIKeyAuth(apiKeyMap))
	apiGroup.Use(api.JWTAuth(e.Auth))
	{
		apiHandler := api.NewHandler(e.Registry, e.Content)
		apiHandler.RegisterRoutes(apiGroup)
		apiGroup.GET("/types", apiHandler.Types)
	}

	// Apply admin routes
	if e.Admin != nil {
		admin.SetupRoutes(r, e.Admin, e.Auth, e.Registry)
	}

	// Fire hook for additional route registration
	e.Hooks.DoAction(context.Background(), "routes.register", r)

	// Swagger API documentation UI
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler,
		ginSwagger.URL("/swagger/doc.json"),
		ginSwagger.DefaultModelsExpandDepth(-1),
	))

	// Front-end catch-all: delegate to active theme
	r.NoRoute(e.themeDispatcher)

	e.Router = r
	return r
}

// serveStatic handles all /static/* requests.
// Routes to admin assets, uploads, or active theme static files.
func (e *Engine) serveStatic(c *gin.Context) {
	fp := c.Param("filepath") // e.g. "/admin/css/admin.css" or "/css/style.css"

	// Admin static assets
	if strings.HasPrefix(fp, "/admin/") {
		c.File(filepath.Join("core/admin/static", strings.TrimPrefix(fp, "/admin/")))
		return
	}

	// User uploads — serve from config upload directory
	if strings.HasPrefix(fp, "/uploads/") {
		relPath := strings.TrimPrefix(fp, "/uploads/")
		c.File(filepath.Join(e.Config.CMS.UploadDir, relPath))
		return
	}

	// Theme static assets — serve from active theme's static directory
	theme := e.ActiveTheme()
	if theme != nil {
		c.File(filepath.Join(theme.StaticDir(), fp))
		return
	}

	c.Status(http.StatusNotFound)
}

// themeDispatcher is the NoRoute handler that delegates front-end requests
// to the currently active theme. This enables runtime theme switching.
func (e *Engine) themeDispatcher(c *gin.Context) {
	// Skip API and admin paths — they are handled by registered routes
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/admin") || strings.HasPrefix(path, "/api/") {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	theme := e.ActiveTheme()
	if theme == nil {
		c.String(http.StatusServiceUnavailable, "No active theme")
		return
	}

	theme.ServeHTTP(c)
}

// SetupAdmin creates the admin module and handler.
// Admin templates and static assets live under core/admin/templates and core/admin/static.
func (e *Engine) SetupAdmin() {
	svc := admin.NewService(
		e.DB,
		e.Content,
		e.Taxonomy,
		e.Users,
		e.Media,
		e.Options,
		e.Auth,
		e.RBAC,
		e.Config.Site.Name,
		e.Config.Site.Timezone,
		e.Config.CMS,
		e.Registry,
	)
	h := admin.NewHandler(svc, e.Registry, "core/admin/templates")

	// Inject engine hook bus so handlers can invoke plugin-provided filters
	h.SetHookBus(e.Hooks)

	// Inject theme management callbacks
	h.SetThemeManager(&admin.ThemeManager{
		SwitchFn: e.SwitchTheme,
		ActiveFn: e.ActiveThemeName,
		AvailableFn: func() []admin.ThemeDisplayInfo {
			themes := e.AvailableThemes()
			result := make([]admin.ThemeDisplayInfo, len(themes))
			for i, t := range themes {
				result[i] = admin.ThemeDisplayInfo{
					Name:         t.Name,
					Slug:         t.Slug,
					Version:      t.Version,
					Description:  t.Description,
					Author:       t.Author,
					Active:       t.Active,
					HasDemoData:  e.ThemeDemoSeedPath(t.Slug) != "",
					DemoImported: e.IsThemeDemoImported(t.Slug),
					HasSettings:  e.ThemeSettingsTemplatePath(t.Slug) != "",
				}
			}
			return result
		},
		ImportDemoFn:       e.ImportThemeDemoData,
		SettingsTemplateFn: e.ThemeSettingsTemplatePath,
		LocaleCatalogFn: func(slug string) *coreI18n.Catalog {
			return extensionAdminCatalog(filepath.Join("themes", slug))
		},
	})

	// Inject cache management callbacks
	h.SetCacheCallbacks(&admin.CacheCallbacks{
		StatusFn: func() admin.CacheManagerInfo {
			info := admin.CacheManagerInfo{IsNoop: e.Cache.IsNoop()}
			switch e.Cache.L1.(type) {
			case *cache.MemoryCache:
				info.L1Type = "memory"
			default:
				info.L1Type = "noop"
			}
			switch e.Cache.L2.(type) {
			case *cache.RedisCache:
				info.L2Type = "redis"
			default:
				info.L2Type = "noop"
			}
			return info
		},
		FlushAllFn:  func() { e.Cache.Flush() },
		FlushPageFn: func() { e.Cache.L1.DeleteByPrefix("page:"); e.Cache.L2.DeleteByPrefix("page:") },
		FlushFragFn: func() { e.Cache.L1.DeleteByPrefix("frag:"); e.Cache.L2.DeleteByPrefix("frag:") },
	})

	// Inject redirect management callbacks
	h.SetRedirectCallbacks(&admin.RedirectCallbacks{
		AllFn: func() []admin.RedirectInfo {
			all := e.Redirects.All()
			result := make([]admin.RedirectInfo, len(all))
			for i, r := range all {
				result[i] = admin.RedirectInfo{
					ID:         r.ID,
					SourcePath: r.SourcePath,
					TargetPath: r.TargetPath,
					StatusCode: r.StatusCode,
					HitCount:   r.HitCount,
				}
			}
			return result
		},
		AddFn:    func(source, target string, code int) error { return e.Redirects.Add(source, target, code) },
		RemoveFn: func(source string) error { return e.Redirects.Remove(source) },
	})

	// Inject plugin management callbacks
	h.SetPluginCallbacks(&admin.PluginCallbacks{
		AllFn: func() []admin.PluginInfo {
			registered := e.PluginManager.RegisteredPlugins()
			result := make([]admin.PluginInfo, len(registered))
			for i, p := range registered {
				slug := plugin.Slug(p)
				hasSettings := false
				if sp, ok := p.(plugin.SettingsProvider); ok && sp.SettingsTemplatePath() != "" {
					hasSettings = true
				}
				result[i] = admin.PluginInfo{
					Name:        p.Name(),
					DisplayName: p.Name(),
					Slug:        slug,
					Version:     p.Version(),
					Description: p.Description(),
					Active:      e.PluginManager.IsActive(p.Name()),
					HasSettings: hasSettings,
				}
			}
			return result
		},
		ActivateFn: func(name string) error {
			if e.PluginManager.IsActive(name) {
				return fmt.Errorf("插件「%s」已处于激活状态", name)
			}
			if !e.PluginManager.Activate(name, e) {
				return fmt.Errorf("未找到插件「%s」", name)
			}
			e.Options.Set("plugin_active_"+name, "true")
			if e.Cache != nil {
				e.Cache.Flush()
			}
			return nil
		},
		DeactivateFn: func(name string) error {
			if !e.PluginManager.IsActive(name) {
				return fmt.Errorf("插件「%s」未激活", name)
			}
			if !e.PluginManager.Deactivate(name, e) {
				return fmt.Errorf("停用插件「%s」失败", name)
			}
			// Persist as "false" rather than deleting, so LoadPlugin can
			// distinguish "user deactivated" from "first run".
			e.Options.Set("plugin_active_"+name, "false")
			if e.Cache != nil {
				e.Cache.Flush()
			}
			return nil
		},
		SettingsTemplateFn: func(slug string) string {
			for _, p := range e.PluginManager.ActivePlugins() {
				if plugin.Slug(p) == slug {
					if sp, ok := p.(plugin.SettingsProvider); ok {
						return sp.SettingsTemplatePath()
					}
				}
			}
			return ""
		},
		SettingsDataFn: func(slug string) map[string]interface{} {
			for _, p := range e.PluginManager.ActivePlugins() {
				if plugin.Slug(p) == slug {
					if dp, ok := p.(plugin.SettingsDataProvider); ok {
						return dp.SettingsData()
					}
				}
			}
			return nil
		},
		SettingsSaveFn: func(slug string, settings map[string]string) {
			for _, p := range e.PluginManager.ActivePlugins() {
				if plugin.Slug(p) == slug {
					if sp, ok := p.(plugin.SettingsSaveProvider); ok {
						sp.OnSettingsSave(settings)
					}
				}
			}
		},
		LocaleCatalogFn: func(slug string) *coreI18n.Catalog {
			for _, p := range e.PluginManager.RegisteredPlugins() {
				if plugin.Slug(p) != slug {
					continue
				}
				if sp, ok := p.(plugin.SettingsProvider); ok {
					if baseDir := extensionBaseFromSettingsTemplate(sp.SettingsTemplatePath()); baseDir != "" {
						if catalog := extensionAdminCatalog(baseDir); catalog != nil {
							return catalog
						}
					}
				}
			}
			return extensionAdminCatalog(filepath.Join("plugins", slug))
		},
	})

	// Inject sitemap generation callback (uses shared generator with transformers)
	h.SetSitemapCallbacks(&admin.SitemapCallbacks{
		GenerateFn: func() (int, error) {
			return e.Sitemap.GenerateToFile(e.SitePublicPath("sitemap.xml"))
		},
	})

	// Inject menu management callbacks
	h.SetMenuCallbacks(&admin.MenuCallbacks{
		AllFn: func() ([]admin.MenuInfo, error) {
			menus, err := e.Menus.GetAll()
			if err != nil {
				return nil, err
			}
			result := make([]admin.MenuInfo, len(menus))
			for i, m := range menus {
				result[i] = admin.MenuInfo{
					ID:       m.ID,
					Name:     m.Name,
					Location: m.Location,
					Items:    convertMenuItems(m.Items),
				}
			}
			return result, nil
		},
		GetByIDFn: func(id uint) (*admin.MenuInfo, error) {
			m, err := e.Menus.GetByID(id)
			if err != nil {
				return nil, err
			}
			info := &admin.MenuInfo{
				ID:       m.ID,
				Name:     m.Name,
				Location: m.Location,
				Items:    convertMenuItems(m.Items),
			}
			return info, nil
		},
		CreateFn: func(name, location string) error {
			m := &menu.Menu{Name: name, Location: location}
			return e.Menus.Create(m)
		},
		UpdateFn: func(id uint, name, location string) error {
			m, err := e.Menus.GetByID(id)
			if err != nil {
				return err
			}
			m.Name = name
			m.Location = location
			return e.Menus.Update(m)
		},
		DeleteFn: func(id uint) error {
			return e.Menus.Delete(id)
		},
		SaveItemsFn: func(menuID uint, items []admin.MenuItemInfo) error {
			menuItems := convertMenuItemInfoToItems(items)
			if err := e.Menus.SaveItems(menuID, menuItems); err != nil {
				return err
			}
			return nil
		},
		LocationsFn: func() []admin.MenuLocationInfo {
			locs := e.Menus.GetLocations()
			result := make([]admin.MenuLocationInfo, len(locs))
			for i, l := range locs {
				result[i] = admin.MenuLocationInfo{Name: l.Name, Label: l.Label}
			}
			return result
		},
		ReloadFn: func() { e.Menus.Reload() },
	})

	e.Admin = h
	logger.Info("Admin module initialized")
}

// convertMenuItems converts menu.Item slice to admin.MenuItemInfo slice.
func convertMenuItems(items []menu.Item) []admin.MenuItemInfo {
	result := make([]admin.MenuItemInfo, len(items))
	for i, item := range items {
		result[i] = admin.MenuItemInfo{
			ID:        item.ID,
			ParentID:  item.ParentID,
			Title:     item.Title,
			URL:       item.URL,
			Target:    item.Target,
			ContentID: item.ContentID,
			SortOrder: item.SortOrder,
			Children:  convertMenuItems(item.Children),
		}
	}
	return result
}

// convertMenuItemInfoToItems converts admin.MenuItemInfo (with nested children) to menu.Item slice.
func convertMenuItemInfoToItems(infos []admin.MenuItemInfo) []menu.Item {
	result := make([]menu.Item, len(infos))
	for i, info := range infos {
		result[i] = menu.Item{
			Title:     info.Title,
			URL:       info.URL,
			Target:    info.Target,
			ContentID: info.ContentID,
			SortOrder: info.SortOrder,
			Children:  convertMenuItemInfoToItems(info.Children),
		}
	}
	return result
}

func extensionAdminCatalog(baseDir string) *coreI18n.Catalog {
	messages := coreI18n.LoadFlatMessagesDir(filepath.Join(baseDir, "locales", "admin"))
	if len(messages) == 0 {
		messages = coreI18n.LoadFlatMessagesDir(filepath.Join(baseDir, "locales"))
	}
	if len(messages) == 0 {
		return nil
	}
	return coreI18n.NewCatalog(coreI18n.DefaultUILanguage, messages)
}

func extensionBaseFromSettingsTemplate(tmplPath string) string {
	if tmplPath == "" {
		return ""
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(tmplPath)))
}

// Start starts the HTTP server.
func (e *Engine) Start() error {
	if e.Router == nil {
		e.SetupRouter()
	}

	addr := fmt.Sprintf("%s:%d", e.Config.Server.Host, e.Config.Server.Port)
	e.server = &http.Server{
		Addr:         addr,
		Handler:      e.Router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Info("GoPress starting", "addr", addr)
	e.Hooks.DoAction(context.Background(), "engine.start")

	return e.server.ListenAndServe()
}

// Shutdown gracefully stops the engine.
func (e *Engine) Shutdown(ctx context.Context) error {
	logger.Info("GoPress shutting down...")
	e.Hooks.DoAction(ctx, "engine.shutdown")

	// Stop scheduler and workers
	if e.Sched != nil {
		e.Sched.Stop()
	}
	if e.Workers != nil {
		e.Workers.Shutdown()
	}

	// Close Redis connection if L2 is Redis
	if rc, ok := e.Cache.L2.(*cache.RedisCache); ok {
		_ = rc.Close()
	}

	if e.server != nil {
		return e.server.Shutdown(ctx)
	}
	return nil
}

// --- App interface implementation ---
// These methods expose engine capabilities to themes via the theme.App interface.

func (e *Engine) Database() *gorm.DB                 { return e.DB }
func (e *Engine) ContentRepo() *content.Repository   { return e.Content }
func (e *Engine) TaxonomyRepo() *taxonomy.Repository { return e.Taxonomy }
func (e *Engine) ContentRegistry() *content.Registry { return e.Registry }
func (e *Engine) OptionsStore() *option.Store        { return e.Options }
func (e *Engine) RewriteEngine() *rewrite.Engine     { return e.Rewrite }
func (e *Engine) SEOBuilder() *rewrite.SEOBuilder    { return e.SEO }
func (e *Engine) MenuStore() *menu.Store             { return e.Menus }
func (e *Engine) MediaRepo() *media.Repository       { return e.Media }
func (e *Engine) I18nManager() *coreI18n.Manager     { return e.I18n }
func (e *Engine) HookBus() *hook.Bus                 { return e.Hooks }

func (e *Engine) SiteTimezone() string {
	if e != nil && e.Options != nil {
		if tz := strings.TrimSpace(e.Options.Get("site_timezone")); tz != "" && config.IsValidTimezone(tz) {
			return tz
		}
	}
	if e != nil && e.Config != nil {
		if tz := strings.TrimSpace(e.Config.Site.Timezone); tz != "" && config.IsValidTimezone(tz) {
			return tz
		}
	}
	return config.DefaultTimezoneName()
}

func (e *Engine) SiteLocation() *time.Location {
	loc, _ := config.LoadTimezone(e.SiteTimezone())
	return loc
}

// Compile-time check: Engine implements theme.App.
var _ coreTheme.App = (*Engine)(nil)
