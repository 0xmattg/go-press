package gopressmcp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0xmattg/go-press/core/agent"
)

const (
	MCPGoSDKVersion       = "v1.7.0-pre.3"
	latestProtocol        = "2026-07-28"
	legacyProtocol        = "2025-11-25"
	maxMCPRequestBytes    = int64(256 << 10)
	toolListTTLMillis     = 30_000
	protocolVersionHeader = "Mcp-Protocol-Version"
	maxRateLimitBuckets   = 4096
)

const principalTokenInfoKey = "gopress.principal"

// Adapter maps the official MCP SDK onto Core's protocol-neutral Agent
// Executor. It never calls registered Tool handlers directly.
type Adapter struct {
	registry    *agent.Registry
	executor    *agent.Executor
	credentials *agent.CredentialService
	audience    string
	sourceKey   []byte
	limiter     *requestLimiter
	handler     http.Handler
}

func NewAdapter(registry *agent.Registry, executor *agent.Executor, credentials *agent.CredentialService, audience string, sourceKey []byte) (*Adapter, error) {
	if registry == nil || executor == nil || credentials == nil || strings.TrimSpace(audience) == "" {
		return nil, errors.New("gopress-mcp: incomplete Agent adapter dependencies")
	}
	adapter := &Adapter{
		registry: registry, executor: executor, credentials: credentials,
		audience: strings.TrimSpace(audience), sourceKey: append([]byte(nil), sourceKey...),
		limiter: newRequestLimiter(120, time.Minute),
	}
	adapter.handler = adapter.buildHTTPHandler()
	return adapter, nil
}

func (a *Adapter) Handler() http.Handler { return a.handler }

func (a *Adapter) buildHTTPHandler() http.Handler {
	stream := mcp.NewStreamableHTTPHandler(a.serverForRequest, &mcp.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 true,
		MaxRequestBodyBytes:          maxMCPRequestBytes,
		PropagateRequestCancellation: true,
	})
	handler := http.Handler(stream)
	handler = protocolGate(handler)
	handler = auth.RequireBearerToken(a.verifyBearer, nil)(handler)
	handler = http.NewCrossOriginProtection().Handler(handler)
	handler = a.limiter.Handler(handler)
	return noStoreHandler(handler)
}

func (a *Adapter) verifyBearer(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	principal, credential, err := a.credentials.AuthenticateWithCredential(ctx, token, a.audience)
	if err != nil {
		return nil, auth.ErrInvalidToken
	}
	return &auth.TokenInfo{
		Scopes: principal.Scopes, Expiration: credential.ExpiresAt,
		UserID: fmt.Sprintf("%s:%d", principal.Kind, principal.SubjectID),
		Extra:  map[string]any{principalTokenInfoKey: principal},
	}, nil
}

func (a *Adapter) serverForRequest(request *http.Request) *mcp.Server {
	principal, ok := principalFromRequest(request)
	if !ok {
		return nil
	}
	visible, err := a.executor.VisibleTools(request.Context(), principal)
	if err != nil {
		return nil
	}
	visibleNames := make(map[string]struct{}, len(visible.Tools))
	for _, registered := range visible.Tools {
		visibleNames[registered.Tool.Name] = struct{}{}
	}
	allTools := a.registry.Snapshot()
	description := "GoPress Agent capabilities exposed through MCP."
	instructions := "GoPress MCP Beta. Tool authorization is enforced by the site Tool Profile, credential scopes, current RBAC, ownership, and audit policy."
	server := mcp.NewServer(&mcp.Implementation{
		Name: "gopress", Title: "GoPress MCP", Version: pluginMeta.Version,
		Description: description,
	}, &mcp.ServerOptions{
		Instructions: instructions,
		PageSize:     mcp.DefaultPageSize,
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{ListChanged: false},
		},
	})
	server.AddReceivingMiddleware(privateCacheMiddleware(visible.Revision, visibleNames))
	client := clientInfoFromHTTPRequest(request, a.sourceDigest(request.RemoteAddr))
	// Register every Tool with the SDK so an explicit call to a non-visible Tool
	// still reaches Core Executor and produces a scope/RBAC denial audit. The
	// list middleware below removes those Tool definitions before disclosure.
	for _, registered := range allTools.Tools {
		registered := registered
		readOnly := registered.Tool.Mutability == agent.MutabilityRead
		closedWorld := false
		destructive := registered.Tool.Risk == agent.RiskDestructive || registered.Tool.Risk == agent.RiskCritical
		server.AddTool(&mcp.Tool{
			Name: registered.Tool.Name, Title: registered.Tool.Title, Description: registered.Tool.Description,
			InputSchema:  append(json.RawMessage(nil), registered.Tool.InputSchema...),
			OutputSchema: append(json.RawMessage(nil), registered.Tool.OutputSchema...),
			Meta: mcp.Meta{
				"xyz.gopress/owner":         registered.Owner,
				"xyz.gopress/agentRevision": visible.Revision,
			},
			Annotations: &mcp.ToolAnnotations{
				ReadOnlyHint: readOnly, IdempotentHint: registered.Tool.Idempotent || readOnly,
				DestructiveHint: &destructive, OpenWorldHint: &closedWorld,
			},
		}, a.toolHandler(principal, client))
	}
	return server
}

func (a *Adapter) toolHandler(principal agent.Principal, baseClient agent.ClientInfo) mcp.ToolHandler {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		arguments := request.Params.Arguments
		if len(arguments) == 0 {
			arguments = json.RawMessage(`{}`)
		}
		client := baseClient
		client.Protocol = request.ProtocolVersion()
		if implementation := request.ClientInfo(); implementation != nil {
			client.Adapter = boundedAdapterName(implementation.Name)
			client.Version = truncateASCII(implementation.Version, 50)
		}
		result, err := a.executor.Execute(ctx, agent.Call{
			RequestID: nextRequestID(), ToolName: request.Params.Name, Arguments: arguments,
			Principal: principal, Client: client,
		})
		if err != nil {
			return agentErrorResult(err), nil
		}
		var structured any
		decoder := json.NewDecoder(bytes.NewReader(result.Output))
		decoder.UseNumber()
		if err := decoder.Decode(&structured); err != nil {
			return agentErrorResult(agent.NewError(agent.CodeInvalidResult, "tool returned invalid structured output")), nil
		}
		return &mcp.CallToolResult{
			Meta:              mcp.Meta{"xyz.gopress/requestId": result.RequestID},
			Content:           []mcp.Content{&mcp.TextContent{Text: string(result.Output)}},
			StructuredContent: structured,
		}, nil
	}
}

func privateCacheMiddleware(revision uint64, visibleNames map[string]struct{}) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, request)
			if err != nil {
				return result, err
			}
			switch typed := result.(type) {
			case *mcp.ListToolsResult:
				filtered := make([]*mcp.Tool, 0, len(typed.Tools))
				for _, tool := range typed.Tools {
					if tool != nil {
						if _, allowed := visibleNames[tool.Name]; allowed {
							filtered = append(filtered, tool)
						}
					}
				}
				typed.Tools = filtered
				typed.TTLMs = toolListTTLMillis
				typed.CacheScope = "private"
				if typed.Meta == nil {
					typed.Meta = mcp.Meta{}
				}
				typed.Meta["xyz.gopress/agentRevision"] = revision
			case *mcp.DiscoverResult:
				typed.SupportedVersions = append([]string(nil), supportedProtocolVersions...)
				typed.TTLMs = toolListTTLMillis
				typed.CacheScope = "private"
				if typed.Meta == nil {
					typed.Meta = mcp.Meta{}
				}
				typed.Meta["xyz.gopress/agentRevision"] = revision
			}
			return result, nil
		}
	}
}

func principalFromRequest(request *http.Request) (agent.Principal, bool) {
	if request == nil {
		return agent.Principal{}, false
	}
	info := auth.TokenInfoFromContext(request.Context())
	if info == nil || info.Extra == nil {
		return agent.Principal{}, false
	}
	principal, ok := info.Extra[principalTokenInfoKey].(agent.Principal)
	return principal, ok && principal.Valid()
}

func clientInfoFromHTTPRequest(request *http.Request, sourceDigest string) agent.ClientInfo {
	if request == nil {
		return agent.ClientInfo{Adapter: "mcp", SourceDigest: sourceDigest}
	}
	return agent.ClientInfo{
		Adapter: "mcp", UserAgent: request.UserAgent(), SourceDigest: sourceDigest,
		TraceID: truncateASCII(request.Header.Get("Traceparent"), 100),
	}
}

func boundedAdapterName(name string) string {
	name = truncateASCII(strings.TrimSpace(name), 46)
	if name == "" {
		return "mcp"
	}
	return "mcp:" + name
}

func agentErrorResult(err error) *mcp.CallToolResult {
	code := agent.ErrorCodeOf(err)
	message := err.Error()
	if code == agent.CodeInternal || code == agent.CodeAuditUnavailable || code == agent.CodeInvalidResult {
		message = "tool execution failed"
	}
	payload := map[string]any{"code": code, "message": message}
	var typed *agent.Error
	if errors.As(err, &typed) && len(typed.RequiredScopes) > 0 {
		payload["required_scopes"] = append([]string(nil), typed.RequiredScopes...)
	}
	encoded, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		encoded = []byte(`{"code":"internal_error","message":"tool execution failed"}`)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}, IsError: true}
}

func nextRequestID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("mcp-%d", time.Now().UnixNano())
	}
	return "mcp-" + hex.EncodeToString(raw)
}

func (a *Adapter) sourceDigest(remoteAddr string) string {
	host := strings.TrimSpace(remoteAddr)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	if host == "" {
		host = "unknown"
	}
	if len(a.sourceKey) > 0 {
		mac := hmac.New(sha256.New, a.sourceKey)
		_, _ = mac.Write([]byte(host))
		return hex.EncodeToString(mac.Sum(nil))
	}
	digest := sha256.Sum256([]byte(host))
	return hex.EncodeToString(digest[:])
}

func truncateASCII(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit]
	}
	var builder strings.Builder
	for _, char := range value {
		if char >= 0x20 && char <= 0x7e {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func noStoreHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		secured := &securityResponseWriter{ResponseWriter: writer}
		secured.applyHeaders()
		next.ServeHTTP(secured, request)
	})
}

// securityResponseWriter reapplies non-cacheable response headers at the
// moment headers are committed. The SDK legitimately sets its own transport
// cache policy, so setting these values only before ServeHTTP is insufficient.
// Unwrap keeps http.ResponseController features such as flushing available.
type securityResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (writer *securityResponseWriter) applyHeaders() {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
}

func (writer *securityResponseWriter) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}
	writer.applyHeaders()
	writer.wroteHeader = true
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *securityResponseWriter) Write(payload []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(payload)
}

func (writer *securityResponseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

// protocolGate deliberately exposes only the latest protocol and the previous
// widely deployed revision, even though the SDK can negotiate older versions.
func protocolGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			next.ServeHTTP(writer, request)
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, maxMCPRequestBytes)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(writer, "MCP request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(writer, "invalid MCP request body", http.StatusBadRequest)
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &envelope) != nil {
			next.ServeHTTP(writer, request)
			return
		}
		headerVersion := strings.TrimSpace(request.Header.Get(protocolVersionHeader))
		if envelope.Method == "initialize" {
			var params struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(envelope.Params, &params)
			if params.ProtocolVersion != legacyProtocol || (headerVersion != "" && headerVersion != legacyProtocol) {
				writeUnsupportedProtocol(writer, envelope.ID)
				return
			}
		} else if !slices.Contains(supportedProtocolVersions, headerVersion) {
			writeUnsupportedProtocol(writer, envelope.ID)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func writeUnsupportedProtocol(writer http.ResponseWriter, id json.RawMessage) {
	if len(id) == 0 {
		id = json.RawMessage(`null`)
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{
			"code": -32022, "message": "unsupported MCP protocol version",
			"data": map[string]any{"supportedVersions": append([]string(nil), supportedProtocolVersions...)},
		},
	})
}

type requestLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]requestBucket
	now     func() time.Time
}

type requestBucket struct {
	Count int
	Reset time.Time
}

func newRequestLimiter(limit int, window time.Duration) *requestLimiter {
	return &requestLimiter{limit: limit, window: window, buckets: make(map[string]requestBucket), now: time.Now}
}

func (limiter *requestLimiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !limiter.allow(rateLimitKey(request)) {
			writer.Header().Set("Retry-After", "60")
			http.Error(writer, "MCP request rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (limiter *requestLimiter) allow(key string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.now()
	if _, exists := limiter.buckets[key]; !exists && len(limiter.buckets) >= maxRateLimitBuckets {
		for candidate, existing := range limiter.buckets {
			if !existing.Reset.After(now) {
				delete(limiter.buckets, candidate)
			}
		}
		if len(limiter.buckets) >= maxRateLimitBuckets {
			return false
		}
	}
	bucket := limiter.buckets[key]
	if bucket.Reset.IsZero() || !bucket.Reset.After(now) {
		bucket = requestBucket{Reset: now.Add(limiter.window)}
	}
	if bucket.Count >= limiter.limit {
		limiter.buckets[key] = bucket
		return false
	}
	bucket.Count++
	limiter.buckets[key] = bucket
	return true
}

func rateLimitKey(request *http.Request) string {
	host := request.RemoteAddr
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	if host == "" {
		host = "unknown"
	}
	return host
}
