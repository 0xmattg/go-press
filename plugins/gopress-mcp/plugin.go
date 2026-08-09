// Package gopressmcp exposes GoPress's protocol-neutral Agent foundation over
// the official Model Context Protocol Go SDK. The plugin owns all MCP wire and
// HTTP concerns; Core never imports this package or the SDK.
package gopressmcp

import (
	"context"
	"crypto/rand"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/agent"
	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/plugin"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/logger"
)

const (
	PluginName      = "gopress-mcp"
	mcpEndpointPath = "/mcp"
)

var supportedProtocolVersions = []string{"2026-07-28", "2025-11-25"}

// appHost is the narrow Core surface required by the protocol adapter and its
// admin controls. In particular, it does not expose themes or business plugins.
type appHost interface {
	AgentToolRegistry() *agent.Registry
	AgentExecutor() *agent.Executor
	AgentCredentialService() *agent.CredentialService
	AgentAuditStore() *agent.AuditStore
	AgentToolPolicy() *agent.Policy
	OptionsStore() *option.Store
	HookBus() *hook.Bus
	PublicSiteURL() string
	AdminAuth() *user.Auth
	RBACManager() *user.RBAC
}

type Plugin struct {
	mu          sync.RWMutex
	host        appHost
	httpHandler http.Handler
	hookHandles []hook.Handle
	sourceKey   []byte
}

func New() *Plugin {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		key = nil
	}
	return &Plugin{sourceKey: key}
}

func (p *Plugin) Name() string          { return pluginMeta.Slug }
func (p *Plugin) Version() string       { return pluginMeta.Version }
func (p *Plugin) Description() string   { return pluginMeta.Description }
func (p *Plugin) DefaultInactive() bool { return pluginMeta.DefaultInactive }

func (p *Plugin) SettingsTemplatePath() string {
	return filepath.Join("plugins", PluginName, "templates", "admin", "settings.tmpl")
}

// MCP settings and all custom admin endpoints remain super-admin-only unless
// an operator explicitly grants these dedicated resources to another role.
func (p *Plugin) SettingsPermissionResource() string { return "mcp" }

func (p *Plugin) Activate(app plugin.App) {
	host, ok := app.(appHost)
	if !ok || !validHost(host) {
		logger.Error("gopress-mcp: required Agent host capabilities unavailable")
		return
	}
	host.AgentExecutor().SetRiskPolicy(host.AgentToolPolicy())
	loadPolicy(host)
	adapter, err := NewAdapter(host.AgentToolRegistry(), host.AgentExecutor(), host.AgentCredentialService(), endpointURL(host.PublicSiteURL()), p.sourceKey)
	if err != nil {
		logger.Error("gopress-mcp: adapter initialization failed", "error", err)
		return
	}

	p.mu.Lock()
	p.host = host
	p.httpHandler = adapter.Handler()
	p.mu.Unlock()

	p.hookHandles = append(p.hookHandles, host.HookBus().AddAction("routes.register", func(_ context.Context, args ...interface{}) {
		if len(args) == 0 {
			return
		}
		router, ok := args[0].(*gin.Engine)
		if !ok {
			return
		}
		p.registerRoutes(router)
	}, 20))
	logger.Info("gopress-mcp activated", "endpoint", endpointURL(host.PublicSiteURL()), "sdk", MCPGoSDKVersion)
}

func (p *Plugin) Deactivate(app plugin.App) {
	p.mu.Lock()
	host := p.host
	p.httpHandler = nil
	p.host = nil
	p.mu.Unlock()
	if host != nil && host.HookBus() != nil {
		for _, handle := range p.hookHandles {
			host.HookBus().RemoveAction(handle)
		}
	}
	if host != nil && host.AgentToolPolicy() != nil {
		_ = host.AgentToolPolicy().Configure(agent.ProfileReadOnly, nil)
	}
	p.hookHandles = nil
	logger.Info("gopress-mcp deactivated")
}

func (p *Plugin) serveMCP(c *gin.Context) {
	p.mu.RLock()
	handler := p.httpHandler
	p.mu.RUnlock()
	if handler == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	handler.ServeHTTP(c.Writer, c.Request)
	c.Abort()
}

func validHost(host appHost) bool {
	return host != nil && host.AgentToolRegistry() != nil && host.AgentExecutor() != nil &&
		host.AgentCredentialService() != nil && host.AgentAuditStore() != nil &&
		host.AgentToolPolicy() != nil && host.OptionsStore() != nil && host.HookBus() != nil &&
		host.AdminAuth() != nil && host.RBACManager() != nil
}

var (
	_ plugin.Plugin                        = (*Plugin)(nil)
	_ plugin.DefaultInactiveProvider       = (*Plugin)(nil)
	_ plugin.SettingsProvider              = (*Plugin)(nil)
	_ plugin.SettingsDataProvider          = (*Plugin)(nil)
	_ plugin.SettingsAuthorizationProvider = (*Plugin)(nil)
)
