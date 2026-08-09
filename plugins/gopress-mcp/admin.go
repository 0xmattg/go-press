package gopressmcp

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/admin"
	"github.com/0xmattg/go-press/core/agent"
)

func (p *Plugin) registerRoutes(router *gin.Engine) {
	p.mu.RLock()
	host := p.host
	p.mu.RUnlock()
	if router == nil || host == nil {
		return
	}
	router.Any(mcpEndpointPath, p.serveMCP)

	credentialRead := admin.RequirePermission(host.AdminAuth(), host.RBACManager(), "agent_credential", "read")
	credentialCreate := admin.RequirePermission(host.AdminAuth(), host.RBACManager(), "agent_credential", "create")
	credentialDelete := admin.RequirePermission(host.AdminAuth(), host.RBACManager(), "agent_credential", "delete")
	auditRead := admin.RequirePermission(host.AdminAuth(), host.RBACManager(), "agent_audit", "read")
	mcpRead := admin.RequirePermission(host.AdminAuth(), host.RBACManager(), "mcp", "read")
	mcpUpdate := admin.RequirePermission(host.AdminAuth(), host.RBACManager(), "mcp", "update")

	router.GET("/admin/plugins/gopress-mcp/credentials", privateNoStore, credentialRead, p.handleCredentialList)
	router.POST("/admin/plugins/gopress-mcp/credentials", privateNoStore, credentialCreate, p.handleCredentialIssue)
	router.POST("/admin/plugins/gopress-mcp/credentials/:id/revoke", privateNoStore, credentialDelete, p.handleCredentialRevoke)
	router.GET("/admin/plugins/gopress-mcp/audit", privateNoStore, auditRead, p.handleAuditQuery)
	router.GET("/admin/plugins/gopress-mcp/diagnostics", privateNoStore, mcpRead, p.handleDiagnostics)
	router.GET("/admin/plugins/gopress-mcp/policy", privateNoStore, mcpRead, p.handlePolicyGet)
	router.POST("/admin/plugins/gopress-mcp/policy", privateNoStore, mcpUpdate, p.handlePolicyUpdate)
}

type credentialView struct {
	ID          uint                `json:"id"`
	Name        string              `json:"name"`
	TokenPrefix string              `json:"token_prefix"`
	Scopes      []string            `json:"scopes"`
	Audience    string              `json:"audience"`
	ExpiresAt   time.Time           `json:"expires_at"`
	LastUsedAt  *time.Time          `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time          `json:"revoked_at,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
	Kind        agent.PrincipalKind `json:"subject_kind"`
}

func (p *Plugin) handleCredentialList(c *gin.Context) {
	host, userID, ok := p.adminSubject(c)
	if !ok {
		return
	}
	credentials, err := host.AgentCredentialService().ListForSubject(c.Request.Context(), agent.PrincipalUser, userID, 100)
	if err != nil {
		respondAgentError(c, err)
		return
	}
	rows := make([]credentialView, 0, len(credentials))
	for _, credential := range credentials {
		rows = append(rows, credentialView{
			ID: credential.ID, Name: credential.Name, TokenPrefix: credential.TokenPrefix,
			Scopes: credential.Scopes(), Audience: credential.Audience, ExpiresAt: credential.ExpiresAt,
			LastUsedAt: credential.LastUsedAt, RevokedAt: credential.RevokedAt,
			CreatedAt: credential.CreatedAt, Kind: credential.SubjectKind,
		})
	}
	noStoreJSON(c, http.StatusOK, gin.H{"credentials": rows})
}

type issueCredentialRequest struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`
	ExpiresInDays int      `json:"expires_in_days"`
}

func (p *Plugin) handleCredentialIssue(c *gin.Context) {
	host, userID, ok := p.adminSubject(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var input issueCredentialRequest
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid credential request"})
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Scopes = agent.NormalizeScopes(input.Scopes)
	if input.ExpiresInDays == 0 {
		input.ExpiresInDays = 30
	}
	policy := host.AgentToolPolicy().Snapshot()
	if input.Name == "" || len(input.Name) > 100 || input.ExpiresInDays < 1 || input.ExpiresInDays > 90 || !allowedCredentialScopes(input.Scopes, policy) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name, currently enabled scopes, and an expiry from 1 to 90 days are required"})
		return
	}
	issued, err := host.AgentCredentialService().Issue(c.Request.Context(), agent.CreateCredentialInput{
		SubjectKind: agent.PrincipalUser, SubjectID: userID, Name: input.Name,
		Scopes: input.Scopes, Audience: endpointURL(host.PublicSiteURL()),
		ExpiresAt: time.Now().UTC().Add(time.Duration(input.ExpiresInDays) * 24 * time.Hour), CreatedBy: userID,
	})
	if err != nil {
		respondAgentError(c, err)
		return
	}
	noStoreJSON(c, http.StatusCreated, gin.H{
		"id": issued.Credential.ID, "token": issued.Token,
		"token_prefix": issued.Credential.TokenPrefix, "scopes": issued.Credential.Scopes(),
		"audience": issued.Credential.Audience, "expires_at": issued.Credential.ExpiresAt,
	})
}

func (p *Plugin) handleCredentialRevoke(c *gin.Context) {
	host, userID, ok := p.adminSubject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid credential id required"})
		return
	}
	if err := host.AgentCredentialService().RevokeForSubject(c.Request.Context(), uint(id), agent.PrincipalUser, userID); err != nil {
		respondAgentError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Status(http.StatusNoContent)
}

func (p *Plugin) handleAuditQuery(c *gin.Context) {
	host := p.currentHost()
	if host == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, perPageErr := strconv.Atoi(c.DefaultQuery("limit", "50"))
	credentialID := uint64(0)
	var credentialErr error
	if raw := strings.TrimSpace(c.Query("credential_id")); raw != "" {
		credentialID, credentialErr = strconv.ParseUint(raw, 10, 64)
	}
	if pageErr != nil || perPageErr != nil || credentialErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid audit pagination and credential filters are required"})
		return
	}
	result, err := host.AgentAuditStore().Query(c.Request.Context(), agent.AuditQuery{
		Page: page, PerPage: perPage, ToolName: c.Query("tool"), Status: c.Query("status"), CredentialID: uint(credentialID),
	})
	if err != nil {
		respondAgentError(c, err)
		return
	}
	noStoreJSON(c, http.StatusOK, result)
}

func (p *Plugin) handleDiagnostics(c *gin.Context) {
	host := p.currentHost()
	if host == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	snapshot := host.AgentToolRegistry().Snapshot()
	policy := host.AgentToolPolicy().Snapshot()
	endpoint := endpointURL(host.PublicSiteURL())
	noStoreJSON(c, http.StatusOK, gin.H{
		"ready": true, "endpoint": endpoint, "transport": "streamable_http_stateless",
		"authentication": "bearer", "protocols": append([]string(nil), supportedProtocolVersions...),
		"sdk_version": MCPGoSDKVersion, "plugin_version": p.Version(),
		"registry_revision": snapshot.Revision, "registered_tools": len(snapshot.Tools),
		"policy_profile": policy.Profile, "policy_revision": policy.Revision,
		"enabled_write_tools": policy.EnabledWriteTools,
		"secure_transport":    endpointUsesSafeTransport(endpoint),
	})
}

type policyUpdateRequest struct {
	Profile           agent.ToolProfile `json:"profile"`
	EnabledWriteTools []string          `json:"enabled_write_tools"`
}

func (p *Plugin) handlePolicyGet(c *gin.Context) {
	host := p.currentHost()
	if host == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	noStoreJSON(c, http.StatusOK, policyResponse(host.AgentToolPolicy().Snapshot()))
}

func (p *Plugin) handlePolicyUpdate(c *gin.Context) {
	host := p.currentHost()
	if host == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var input policyUpdateRequest
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid policy request"})
		return
	}
	input.EnabledWriteTools = uniqueStrings(input.EnabledWriteTools)
	if input.Profile == agent.ProfileReadOnly {
		input.EnabledWriteTools = nil
	}
	if err := validatePolicyInput(input.Profile, input.EnabledWriteTools); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tool profile or write tool"})
		return
	}
	encoded, err := json.Marshal(input.EnabledWriteTools)
	if err != nil {
		respondAgentError(c, err)
		return
	}
	if err := host.OptionsStore().SetMany(map[string]string{agent.OptionToolProfile: string(input.Profile), agent.OptionEnabledWriteTools: string(encoded)}); err != nil {
		respondAgentError(c, err)
		return
	}
	if err := host.AgentToolPolicy().Configure(input.Profile, input.EnabledWriteTools); err != nil {
		respondAgentError(c, err)
		return
	}
	noStoreJSON(c, http.StatusOK, policyResponse(host.AgentToolPolicy().Snapshot()))
}

func policyResponse(snapshot agent.PolicySnapshot) gin.H {
	return gin.H{"profile": snapshot.Profile, "enabled_write_tools": snapshot.EnabledWriteTools, "revision": snapshot.Revision, "available_write_tools": agent.CoreWriteTools(), "available_write_scopes": enabledWriteScopes(snapshot)}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (p *Plugin) currentHost() appHost {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.host
}

func (p *Plugin) adminSubject(c *gin.Context) (appHost, uint, bool) {
	host := p.currentHost()
	if host == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return nil, 0, false
	}
	value, exists := c.Get("admin_user_id")
	if !exists {
		c.AbortWithStatus(http.StatusUnauthorized)
		return nil, 0, false
	}
	userID, ok := value.(uint)
	if !ok || userID == 0 {
		c.AbortWithStatus(http.StatusUnauthorized)
		return nil, 0, false
	}
	return host, userID, true
}

func respondAgentError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	code := agent.ErrorCodeOf(err)
	message := err.Error()
	switch code {
	case agent.CodeInvalidRequest, agent.CodeInvalidArguments:
		status = http.StatusBadRequest
	case agent.CodeUnauthenticated:
		status = http.StatusUnauthorized
	case agent.CodePermissionDenied, agent.CodeInsufficientScope:
		status = http.StatusForbidden
	case agent.CodeNotFound:
		status = http.StatusNotFound
	case agent.CodeConflict:
		status = http.StatusConflict
	case agent.CodeInternal, agent.CodeAuditUnavailable:
		message = "agent service unavailable"
	}
	noStoreJSON(c, status, gin.H{"error": message, "code": code})
}

func noStoreJSON(c *gin.Context, status int, value any) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(status, value)
}

func privateNoStore(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Pragma", "no-cache")
	c.Next()
}
