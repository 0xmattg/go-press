// Package gopressanalytics provides GoPress's first-party, self-hosted web
// analytics plugin. It collects anonymous front-end page views and exposes a
// compact operations dashboard without coupling core or themes to analytics.
package gopressanalytics

import (
	"context"
	"html/template"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"go-press/core"
	"go-press/core/admin"
	"go-press/core/hook"
	"go-press/core/plugin"
	"go-press/core/user"
	"go-press/pkg/logger"
)

const (
	PluginName = "gopress-analytics"

	optEnabled       = "plugin_gopress-analytics_enabled"
	optRetentionDays = "plugin_gopress-analytics_retention_days"
	optDashboardDays = "plugin_gopress-analytics_dashboard_days"
)

type Plugin struct {
	engine    *core.Engine
	repo      *Repository
	summary   SummaryStore
	dataQuery DataQueryStore
	geoIP     *geoIPDatabase
	collector *collector
	widget    *template.Template
	location  *time.Location
	hashKey   []byte
	// collectionOverride is used by isolated middleware tests.
	collectionOverride func() bool
	hookHandles        []hook.Handle
	rbacGrants         []user.CapabilityGrant
	active             atomic.Bool
	retention          atomic.Int64
	started            atomic.Bool
	stopOnce           sync.Once
	maintStop          chan struct{}
	maintDone          chan struct{}
}

func New() *Plugin {
	p := &Plugin{
		location:  time.UTC,
		maintStop: make(chan struct{}),
		maintDone: make(chan struct{}),
	}
	p.retention.Store(90)
	return p
}

func (p *Plugin) Name() string    { return PluginName }
func (p *Plugin) Version() string { return "1.0.0" }
func (p *Plugin) Description() string {
	return "GoPress 官方自托管访问统计：匿名采集 PV、UV、新访客、趋势和热门页面。"
}

func (p *Plugin) SettingsTemplatePath() string {
	return filepath.Join("plugins", PluginName, "templates", "admin", "settings.tmpl")
}

func (p *Plugin) SettingsData() map[string]interface{} {
	return map[string]interface{}{
		"AnalyticsEnabled": p.collectionEnabled(),
		"RetentionDays":    p.retentionDays(),
		"DashboardDays":    p.dashboardDays(),
		"GeoIP":            p.geoIPStatus(),
	}
}

func (p *Plugin) OnSettingsSave(settings map[string]string) {
	if p.engine == nil {
		return
	}
	if value, ok := settings[optRetentionDays]; ok {
		days, _ := strconv.Atoi(value)
		if days != 30 && days != 60 && days != 90 && days != 180 {
			days = 90
		}
		p.retention.Store(int64(days))
	}
}

func (p *Plugin) Activate(app plugin.App) {
	e, ok := app.(*core.Engine)
	if !ok {
		logger.Error("gopress-analytics: failed to cast app to *core.Engine")
		return
	}
	p.engine = e
	p.repo = NewRepository(e.DB)
	p.summary = p.repo
	p.dataQuery = p.repo
	p.location = siteLocation(e)
	p.hashKey = analyticsHashKey(e)
	p.geoIP = newGeoIPDatabase(geoIPFileRelPath)
	if err := p.geoIP.Load(); err != nil {
		logger.Info("gopress-analytics: GeoIP database not loaded", "path", geoIPFileRelPath, "error", err)
	}
	retentionDays, _ := strconv.Atoi(e.Options.GetDefault(optRetentionDays, "90"))
	if retentionDays != 30 && retentionDays != 60 && retentionDays != 90 && retentionDays != 180 {
		retentionDays = 90
	}
	p.retention.Store(int64(retentionDays))
	p.stopOnce = sync.Once{}
	p.maintStop = make(chan struct{})
	p.maintDone = make(chan struct{})

	if err := p.repo.AutoMigrate(); err != nil {
		logger.Error("gopress-analytics: table migration failed", "error", err)
		return
	}
	for _, table := range []string{
		"events", "visitors", "sessions", "visitor_days", "page_visitor_days",
		"daily", "daily_pages", "daily_dimensions", "daily_dimension_visitors",
	} {
		core.RegisterPluginTable(storageSlug, table)
	}

	widget, err := template.New("analytics-widget").ParseFiles(
		filepath.Join("plugins", PluginName, "templates", "admin", "dashboard_widget.tmpl"),
	)
	if err != nil {
		logger.Error("gopress-analytics: dashboard template parse failed", "error", err)
		return
	}
	p.widget = widget
	p.collector = newCollector(p.repo, 2000)
	p.hookHandles = p.hookHandles[:0]
	p.rbacGrants = p.rbacGrants[:0]
	p.rbacGrants = append(p.rbacGrants, e.RBAC.GrantCapability(user.RoleEditor, "analytics", "read"))
	p.active.Store(true)
	p.started.Store(true)

	p.hookHandles = append(p.hookHandles, e.Hooks.AddAction("middleware.early", func(_ context.Context, args ...interface{}) {
		if len(args) == 0 {
			return
		}
		router, ok := args[0].(*gin.Engine)
		if ok {
			router.Use(p.analyticsMiddleware())
		}
	}, 20))

	p.hookHandles = append(p.hookHandles, e.Hooks.AddAction("routes.register", func(_ context.Context, args ...interface{}) {
		if len(args) == 0 {
			return
		}
		router, ok := args[0].(*gin.Engine)
		if !ok {
			return
		}
		router.GET(
			"/admin/plugins/gopress-analytics/summary",
			admin.RequirePermission(e.Auth, e.RBAC, "analytics", "read"),
			p.handleSummary,
		)
		router.GET(
			"/admin/plugins/gopress-analytics/data-query",
			admin.RequirePermission(e.Auth, e.RBAC, "analytics", "read"),
			p.handleDataQuery,
		)
		router.POST(
			"/admin/plugins/gopress-analytics/geoip/update",
			admin.RequirePermission(e.Auth, e.RBAC, "plugin", "update"),
			p.handleGeoIPUpdate,
		)
	}, 20))

	p.hookHandles = append(p.hookHandles, e.Hooks.AddFilter(hook.AdminDashboardWidgets, p.renderDashboardWidget, 20))
	p.hookHandles = append(p.hookHandles, e.Hooks.AddAction("engine.shutdown", func(_ context.Context, _ ...interface{}) {
		p.shutdown()
	}, 20))

	go p.maintenanceLoop()
	logger.Info("gopress-analytics plugin activated",
		"collection_enabled", p.collectionEnabled(),
		"retention_days", p.retentionDays(),
	)
}

func (p *Plugin) Deactivate(app plugin.App) {
	p.active.Store(false)
	p.shutdown()

	e, ok := app.(*core.Engine)
	if !ok {
		e = p.engine
	}
	if e != nil {
		for _, handle := range p.hookHandles {
			e.Hooks.RemoveAction(handle)
			e.Hooks.RemoveFilter(handle)
		}
		for _, grant := range p.rbacGrants {
			e.RBAC.RevokeCapabilityGrant(grant)
		}
	}
	p.hookHandles = p.hookHandles[:0]
	p.rbacGrants = p.rbacGrants[:0]
	logger.Info("gopress-analytics plugin deactivated")
}

func (p *Plugin) shutdown() {
	if !p.started.Load() {
		return
	}
	p.stopOnce.Do(func() {
		p.active.Store(false)
		if p.collector != nil {
			p.collector.stopAndFlush()
		}
		close(p.maintStop)
		<-p.maintDone
		p.started.Store(false)
	})
}

func (p *Plugin) maintenanceLoop() {
	defer close(p.maintDone)
	p.purgeExpired()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.purgeExpired()
		case <-p.maintStop:
			return
		}
	}
}

func (p *Plugin) purgeExpired() {
	if p.repo == nil {
		return
	}
	cutoff := time.Now().In(p.location).AddDate(0, 0, -p.retentionDays())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.repo.PurgeBefore(ctx, cutoff); err != nil {
		logger.Error("gopress-analytics: retention cleanup failed", "error", err)
	}
}

func (p *Plugin) collectionEnabled() bool {
	if p.collectionOverride != nil {
		return p.collectionOverride()
	}
	if p.engine == nil || p.engine.Options == nil {
		return false
	}
	return p.engine.Options.GetDefault(optEnabled, "false") == "true"
}

func (p *Plugin) retentionDays() int {
	days := int(p.retention.Load())
	switch days {
	case 30, 60, 90, 180:
		return days
	default:
		return 90
	}
}

func (p *Plugin) dashboardDays() int {
	if p.engine == nil || p.engine.Options == nil {
		return 30
	}
	days, _ := strconv.Atoi(p.engine.Options.All()[optDashboardDays])
	switch days {
	case 7, 30, 90:
		return days
	default:
		return 30
	}
}

func (p *Plugin) geoIPStatus() GeoIPStatus {
	if p.geoIP != nil {
		return p.geoIP.Status()
	}
	return GeoIPStatus{Path: geoIPFileRelPath}
}

func normalizeBoolOption(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return "true"
	default:
		return "false"
	}
}

func siteLocation(e *core.Engine) *time.Location {
	if e != nil && e.Config != nil && strings.TrimSpace(e.Config.Site.Timezone) != "" {
		if loc, err := time.LoadLocation(e.Config.Site.Timezone); err == nil {
			return loc
		}
	}
	return time.UTC
}

func analyticsHashKey(e *core.Engine) []byte {
	if e != nil && e.Config != nil {
		value := strings.TrimSpace(e.Config.CMS.JWTSecret + "|" + e.Config.Site.URL)
		if value != "|" {
			return []byte(value)
		}
	}
	return []byte(randomToken(32))
}
