package gopressmcp

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"

	"github.com/0xmattg/go-press/core/agent"
)

type scopeOption struct {
	Name    string
	Key     string
	Checked bool
	Write   bool
}

type writeToolOption struct {
	Name        string
	Key         string
	Scope       string
	Risk        agent.RiskLevel
	Enabled     bool
	Recommended bool
}

func (p *Plugin) SettingsData() map[string]interface{} {
	p.mu.RLock()
	host := p.host
	p.mu.RUnlock()
	endpoint := ""
	revision := uint64(0)
	toolCount := 0
	policy := agent.PolicySnapshot{Profile: agent.ProfileReadOnly}
	if host != nil {
		endpoint = endpointURL(host.PublicSiteURL())
		if registry := host.AgentToolRegistry(); registry != nil {
			snapshot := registry.Snapshot()
			revision = snapshot.Revision
			toolCount = len(snapshot.Tools)
		}
		if runtimePolicy := host.AgentToolPolicy(); runtimePolicy != nil {
			policy = runtimePolicy.Snapshot()
		}
	}
	enabled := make(map[string]struct{}, len(policy.EnabledWriteTools))
	for _, name := range policy.EnabledWriteTools {
		enabled[name] = struct{}{}
	}
	writeTools := make([]writeToolOption, 0, len(agent.CoreWriteTools()))
	for _, info := range agent.CoreWriteTools() {
		_, on := enabled[info.Name]
		writeTools = append(writeTools, writeToolOption{Name: info.Name, Key: writeToolLocaleKey(info.Name), Scope: info.Scope, Risk: info.Risk, Enabled: on, Recommended: info.Recommended})
	}
	scopes := []scopeOption{
		{Name: agent.ScopeSiteRead, Key: "site", Checked: true},
		{Name: agent.ScopeContentRead, Key: "content", Checked: true},
		{Name: agent.ScopeTaxonomyRead, Key: "taxonomy", Checked: true},
		{Name: agent.ScopeMediaRead, Key: "media", Checked: true},
	}
	for _, scope := range enabledWriteScopes(policy) {
		scopes = append(scopes, scopeOption{Name: scope, Key: scopeLocaleKey(scope), Write: true})
	}
	return map[string]interface{}{
		"EndpointURL": endpoint,
		"Protocols":   append([]string(nil), supportedProtocolVersions...),
		"SDKVersion":  MCPGoSDKVersion,
		"Revision":    revision,
		"ToolCount":   toolCount,
		"Scopes":      scopes, "PolicyProfile": policy.Profile,
		"PolicyRevision": policy.Revision, "WriteTools": writeTools,
	}
}

func loadPolicy(host appHost) {
	if host == nil || host.AgentToolPolicy() == nil || host.OptionsStore() == nil {
		return
	}
	profile := agent.ToolProfile(strings.TrimSpace(host.OptionsStore().Get(agent.OptionToolProfile)))
	if profile == "" {
		profile = agent.ProfileReadOnly
	}
	var enabled []string
	if raw := strings.TrimSpace(host.OptionsStore().Get(agent.OptionEnabledWriteTools)); raw != "" {
		_ = json.Unmarshal([]byte(raw), &enabled)
	}
	if validatePolicyInput(profile, enabled) != nil || host.AgentToolPolicy().Configure(profile, enabled) != nil {
		_ = host.AgentToolPolicy().Configure(agent.ProfileReadOnly, nil)
	}
}

func validatePolicyInput(profile agent.ToolProfile, enabled []string) error {
	if profile != agent.ProfileReadOnly && profile != agent.ProfileSafeWrite {
		return agent.ErrInvalidToolProfile
	}
	allowed := make(map[string]struct{}, len(agent.CoreWriteTools()))
	for _, info := range agent.CoreWriteTools() {
		allowed[info.Name] = struct{}{}
	}
	for _, name := range enabled {
		if _, ok := allowed[name]; !ok {
			return agent.ErrInvalidToolProfile
		}
	}
	return nil
}

func enabledWriteScopes(snapshot agent.PolicySnapshot) []string {
	if snapshot.Profile != agent.ProfileSafeWrite {
		return nil
	}
	byName := make(map[string]string, len(agent.CoreWriteTools()))
	for _, info := range agent.CoreWriteTools() {
		byName[info.Name] = info.Scope
	}
	seen := make(map[string]struct{})
	for _, name := range snapshot.EnabledWriteTools {
		if scope := byName[name]; scope != "" {
			seen[scope] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for scope := range seen {
		result = append(result, scope)
	}
	slices.Sort(result)
	return result
}

func allowedCredentialScopes(scopes []string, snapshot agent.PolicySnapshot) bool {
	if len(scopes) == 0 {
		return false
	}
	allowed := make(map[string]struct{})
	for _, scope := range agent.CoreReadScopes() {
		allowed[scope] = struct{}{}
	}
	for _, scope := range enabledWriteScopes(snapshot) {
		allowed[scope] = struct{}{}
	}
	for _, scope := range scopes {
		if _, ok := allowed[scope]; !ok {
			return false
		}
	}
	return true
}

func writeToolLocaleKey(name string) string { return strings.TrimPrefix(name, "gopress.") }
func scopeLocaleKey(scope string) string {
	switch scope {
	case agent.ScopeContentWrite:
		return "content_write"
	case agent.ScopeContentPublish:
		return "content_publish"
	case agent.ScopeMediaWrite:
		return "media_write"
	default:
		return strings.TrimPrefix(scope, "gopress:")
	}
}

func endpointURL(siteURL string) string {
	base := strings.TrimRight(strings.TrimSpace(siteURL), "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return ""
	}
	return base + mcpEndpointPath
}

func endpointUsesSafeTransport(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if strings.EqualFold(parsed.Scheme, "https") {
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	return strings.EqualFold(parsed.Scheme, "http") && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}
