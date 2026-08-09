package gopressmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xmattg/go-press/core/agent"
	"github.com/0xmattg/go-press/core/user"
)

func TestAdapterSafeWriteToolIsPolicyGatedAndIdempotentlyReplayed(t *testing.T) {
	fixture := newTestFixture(t)
	var calls atomic.Int32
	writeName := "test.content.update"
	if _, err := fixture.host.registry.Register("test", agent.Tool{
		Name: writeName, Title: "Test write", Description: "Deterministic MCP write contract test.",
		InputSchema:  json.RawMessage(`{"type":"object","required":["value","idempotency_key"],"properties":{"value":{"type":"string"},"idempotency_key":{"type":"string","minLength":8,"maxLength":200}},"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["value"],"properties":{"value":{"type":"string"}},"additionalProperties":false}`),
		Mutability:   agent.MutabilityWrite, Risk: agent.RiskWrite, Idempotent: true,
		Permission: agent.PermissionRequirement{Scope: agent.ScopeContentWrite, Resource: "content", Action: "update"},
		Handler: func(_ context.Context, invocation agent.Invocation) (any, error) {
			calls.Add(1)
			var input struct {
				Value string `json:"value"`
			}
			_ = json.Unmarshal(invocation.Arguments, &input)
			return map[string]any{"value": input.Value}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	audience := endpointURL(fixture.host.siteURL)
	issued := fixture.issueToken(t, fixture.editor, audience, agent.ScopeContentWrite)
	adapter, err := NewAdapter(fixture.host.registry, fixture.host.executor, fixture.host.credentials, audience, []byte("stable-source-key"))
	if err != nil {
		t.Fatal(err)
	}
	hidden := performMCPRequest(t, adapter.Handler(), issued.Token, latestProtocol, "tools/list", "", latestRequestBody("tools/list", map[string]any{}))
	if hidden.Code != http.StatusOK || strings.Contains(hidden.Body.String(), writeName) {
		t.Fatalf("default list=%s", hidden.Body.String())
	}
	deniedBody := latestRequestBody("tools/call", map[string]any{"name": writeName, "arguments": map[string]any{"value": "one", "idempotency_key": "mcp-write-0001"}})
	denied := performMCPRequest(t, adapter.Handler(), issued.Token, latestProtocol, "tools/call", writeName, deniedBody)
	if denied.Code != http.StatusOK || !strings.Contains(denied.Body.String(), string(agent.CodeRiskDenied)) || calls.Load() != 0 {
		t.Fatalf("denied=%s calls=%d", denied.Body.String(), calls.Load())
	}
	if err := fixture.host.policy.Configure(agent.ProfileSafeWrite, []string{writeName}); err != nil {
		t.Fatal(err)
	}
	listed := performMCPRequest(t, adapter.Handler(), issued.Token, latestProtocol, "tools/list", "", latestRequestBody("tools/list", map[string]any{}))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), writeName) {
		t.Fatalf("enabled list=%s", listed.Body.String())
	}
	first := performMCPRequest(t, adapter.Handler(), issued.Token, latestProtocol, "tools/call", writeName, deniedBody)
	second := performMCPRequest(t, adapter.Handler(), issued.Token, latestProtocol, "tools/call", writeName, deniedBody)
	if first.Code != http.StatusOK || second.Code != http.StatusOK || strings.Contains(first.Body.String(), `"isError":true`) || strings.Contains(second.Body.String(), `"isError":true`) || calls.Load() != 1 {
		t.Fatalf("first=%s second=%s calls=%d", first.Body.String(), second.Body.String(), calls.Load())
	}
	audit, err := fixture.host.audit.Query(context.Background(), agent.AuditQuery{ToolName: writeName, CredentialID: issued.Credential.ID, PerPage: 10})
	if err != nil || audit.Total != 5 {
		t.Fatalf("audit=%+v err=%v", audit, err)
	}
	foundReplay := false
	for _, event := range audit.Events {
		if event.Status == agent.AuditReplayed {
			foundReplay = true
		}
	}
	if !foundReplay {
		t.Fatalf("replay audit missing: %+v", audit.Events)
	}
}

func TestAdapterLatestProtocolWithOfficialClient(t *testing.T) {
	fixture := newTestFixture(t)
	audience := endpointURL(fixture.host.siteURL)
	issued := fixture.issueToken(t, fixture.editor, audience, agent.ScopeSiteRead, agent.ScopeContentRead)
	adapter, err := NewAdapter(fixture.host.registry, fixture.host.executor, fixture.host.credentials, audience, []byte("stable-source-key"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(adapter.Handler())
	defer server.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "contract-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: server.URL, DisableStandaloneSSE: true,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{token: issued.Token}},
	}, nil)
	if err != nil {
		t.Fatalf("connect latest MCP protocol: %v", err)
	}
	defer session.Close()
	if initialized := session.InitializeResult(); initialized == nil || initialized.ProtocolVersion != latestProtocol {
		t.Fatalf("initialize result=%+v", initialized)
	}

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if listed.CacheScope != "private" || listed.TTLMs != toolListTTLMillis || len(listed.Tools) != 2 {
		t.Fatalf("tool list=%+v", listed)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "test.site.get", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("tool result=%+v err=%v", result, err)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok || structured["site"] != "example" {
		t.Fatalf("structured content=%#v", result.StructuredContent)
	}

	audit, err := fixture.host.audit.Query(context.Background(), agent.AuditQuery{ToolName: "test.site.get", PerPage: 10})
	if err != nil || audit.Total != 2 {
		t.Fatalf("audit=%+v err=%v", audit, err)
	}
	for _, event := range audit.Events {
		if event.Adapter != "mcp:contract-client" || event.Protocol != latestProtocol || event.SourceDigest == "" {
			t.Fatalf("incomplete MCP audit event=%+v", event)
		}
	}
}

func TestAdapterProtocolNegotiationAndCredentialRejection(t *testing.T) {
	fixture := newTestFixture(t)
	audience := endpointURL(fixture.host.siteURL)
	issued := fixture.issueToken(t, fixture.editor, audience, agent.ScopeSiteRead, agent.ScopeContentRead)
	adapter, err := NewAdapter(fixture.host.registry, fixture.host.executor, fixture.host.credentials, audience, []byte("stable-source-key"))
	if err != nil {
		t.Fatal(err)
	}

	discover := latestRequestBody("server/discover", map[string]any{})
	response := performMCPRequest(t, adapter.Handler(), issued.Token, latestProtocol, "server/discover", "", discover)
	if response.Code != http.StatusOK {
		t.Fatalf("discover status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Result struct {
			ResultType        string   `json:"resultType"`
			SupportedVersions []string `json:"supportedVersions"`
			TTLMs             int      `json:"ttlMs"`
			CacheScope        string   `json:"cacheScope"`
		} `json:"result"`
	}
	decodeRecorderJSON(t, response, &envelope)
	if envelope.Result.ResultType != "complete" || envelope.Result.TTLMs != toolListTTLMillis || envelope.Result.CacheScope != "private" ||
		!reflect.DeepEqual(envelope.Result.SupportedVersions, supportedProtocolVersions) {
		t.Fatalf("discover result=%+v", envelope.Result)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers=%v", response.Header())
	}
	headerMismatch := performMCPRequest(t, adapter.Handler(), issued.Token, latestProtocol, "tools/list", "", discover)
	if headerMismatch.Code != http.StatusBadRequest || !strings.Contains(headerMismatch.Body.String(), "header mismatch") {
		t.Fatalf("header mismatch status=%d body=%s", headerMismatch.Code, headerMismatch.Body.String())
	}
	crossOriginRequest := httptest.NewRequest(http.MethodPost, "https://example.test/mcp", bytes.NewReader(discover))
	crossOriginRequest.Header.Set("Authorization", "Bearer "+issued.Token)
	crossOriginRequest.Header.Set("Content-Type", "application/json")
	crossOriginRequest.Header.Set("Accept", "application/json, text/event-stream")
	crossOriginRequest.Header.Set(protocolVersionHeader, latestProtocol)
	crossOriginRequest.Header.Set("Mcp-Method", "server/discover")
	crossOriginRequest.Header.Set("Origin", "https://evil.test")
	crossOriginResponse := httptest.NewRecorder()
	adapter.Handler().ServeHTTP(crossOriginResponse, crossOriginRequest)
	if crossOriginResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-origin MCP status=%d body=%s", crossOriginResponse.Code, crossOriginResponse.Body.String())
	}
	oversized := bytes.Repeat([]byte("x"), int(maxMCPRequestBytes)+1)
	oversizedResponse := performMCPRequest(t, adapter.Handler(), issued.Token, latestProtocol, "server/discover", "", oversized)
	if oversizedResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized MCP status=%d body=%s", oversizedResponse.Code, oversizedResponse.Body.String())
	}

	legacyInitialize := []byte(`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy-test","version":"1"}}}`)
	response = performMCPRequest(t, adapter.Handler(), issued.Token, "", "", "", legacyInitialize)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), legacyProtocol) {
		t.Fatalf("legacy initialize status=%d body=%s", response.Code, response.Body.String())
	}
	legacyList := []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}`)
	response = performMCPRequest(t, adapter.Handler(), issued.Token, legacyProtocol, "", "", legacyList)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "test.site.get") {
		t.Fatalf("legacy list status=%d body=%s", response.Code, response.Body.String())
	}

	unsupported := []byte(`{"jsonrpc":"2.0","id":4,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"old","version":"1"}}}`)
	response = performMCPRequest(t, adapter.Handler(), issued.Token, "", "", "", unsupported)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":-32022`) {
		t.Fatalf("unsupported status=%d body=%s", response.Code, response.Body.String())
	}

	response = performMCPRequest(t, adapter.Handler(), "", latestProtocol, "server/discover", "", discover)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status=%d body=%s", response.Code, response.Body.String())
	}
	wrongAudience := fixture.issueToken(t, fixture.editor, "https://other.test/mcp", agent.ScopeSiteRead)
	response = performMCPRequest(t, adapter.Handler(), wrongAudience.Token, latestProtocol, "server/discover", "", discover)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong audience status=%d body=%s", response.Code, response.Body.String())
	}
	if err := fixture.host.credentials.Revoke(context.Background(), issued.Credential.ID); err != nil {
		t.Fatal(err)
	}
	response = performMCPRequest(t, adapter.Handler(), issued.Token, latestProtocol, "server/discover", "", discover)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAdapterFiltersToolsButAuditsExplicitScopeAndRBACDenials(t *testing.T) {
	fixture := newTestFixture(t)
	audience := endpointURL(fixture.host.siteURL)
	adapter, err := NewAdapter(fixture.host.registry, fixture.host.executor, fixture.host.credentials, audience, []byte("stable-source-key"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		account  user.User
		scopes   []string
		wantCode string
	}{
		{name: "scope insufficient", account: fixture.editor, scopes: []string{agent.ScopeSiteRead}, wantCode: string(agent.CodeInsufficientScope)},
		{name: "role denied", account: fixture.subscriber, scopes: []string{agent.ScopeContentRead}, wantCode: string(agent.CodePermissionDenied)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			issued := fixture.issueToken(t, test.account, audience, test.scopes...)
			listed := performMCPRequest(t, adapter.Handler(), issued.Token, latestProtocol, "tools/list", "", latestRequestBody("tools/list", map[string]any{}))
			if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), "test.content.list") {
				t.Fatalf("filtered tool list status=%d body=%s", listed.Code, listed.Body.String())
			}
			callBody := latestRequestBody("tools/call", map[string]any{"name": "test.content.list", "arguments": map[string]any{}})
			called := performMCPRequest(t, adapter.Handler(), issued.Token, latestProtocol, "tools/call", "test.content.list", callBody)
			if called.Code != http.StatusOK || !strings.Contains(called.Body.String(), `"isError":true`) || !strings.Contains(called.Body.String(), test.wantCode) {
				t.Fatalf("denied tool call status=%d body=%s", called.Code, called.Body.String())
			}
			if test.wantCode == string(agent.CodeInsufficientScope) && !strings.Contains(called.Body.String(), agent.ScopeContentRead) {
				t.Fatalf("scope denial omitted required scope: %s", called.Body.String())
			}
			audit, err := fixture.host.audit.Query(context.Background(), agent.AuditQuery{ToolName: "test.content.list", CredentialID: issued.Credential.ID, PerPage: 10})
			if err != nil || audit.Total != 1 || audit.Events[0].Status != agent.AuditDenied || string(audit.Events[0].ErrorCode) != test.wantCode {
				t.Fatalf("denial audit=%+v err=%v", audit, err)
			}
		})
	}
}

func TestRequestLimiterCannotBeBypassedWithTokenChurnOrGrowUnbounded(t *testing.T) {
	limiter := newRequestLimiter(2, time.Minute)
	request := httptest.NewRequest(http.MethodPost, "https://example.test/mcp", nil)
	request.RemoteAddr = "203.0.113.20:1000"
	for index, token := range []string{"Bearer one", "Bearer two", "Bearer three"} {
		request.Header.Set("Authorization", token)
		allowed := limiter.allow(rateLimitKey(request))
		if allowed != (index < 2) {
			t.Fatalf("request %d allowed=%v", index+1, allowed)
		}
	}

	bounded := newRequestLimiter(1, time.Hour)
	for index := 0; index < maxRateLimitBuckets; index++ {
		if !bounded.allow(fmt.Sprintf("source-%d", index)) {
			t.Fatalf("bucket %d rejected before capacity", index)
		}
	}
	if bounded.allow("source-over-capacity") || len(bounded.buckets) != maxRateLimitBuckets {
		t.Fatalf("rate-limit buckets grew to %d", len(bounded.buckets))
	}
}

func latestRequestBody(method string, params map[string]any) []byte {
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    latestProtocol,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "raw-client", "version": "1"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	encoded, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	return encoded
}

func performMCPRequest(t *testing.T, handler http.Handler, token, protocol, method, name string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "https://example.test/mcp", bytes.NewReader(body))
	request.RemoteAddr = "203.0.113.10:4321"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if protocol != "" {
		request.Header.Set(protocolVersionHeader, protocol)
	}
	if method != "" {
		request.Header.Set("Mcp-Method", method)
	}
	if name != "" {
		request.Header.Set("Mcp-Name", name)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeRecorderJSON(t *testing.T, recorder *httptest.ResponseRecorder, value any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), value); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
}
