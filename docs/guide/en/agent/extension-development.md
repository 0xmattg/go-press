# Agent Tool Extension Development

Business plugins can register protocol-neutral Tools through
`plugin.AgentHost`. The MCP adapter reads the Registry only; it does not
enumerate plugin slugs or call plugin-specific transport APIs.

The low-level contract exists, but productized governance is incomplete. A
read Tool can integrate when it accurately reuses an existing scope/RBAC
meaning. Custom scopes and third-party write Tool approval/credential UI remain
Phase 5.

## Public Host and Lifecycle

```go
type AgentHost interface {
    AgentToolRegistry() *agent.Registry
    AgentExecutor() *agent.Executor
}
```

Type-assert the smallest required host in `Activate`, keep every returned
handle, and revoke it in `Deactivate`. Do not assert the whole concrete Engine
to access undeclared internals.

```go
type Plugin struct {
    agentHandles []*agent.Handle
}

func (p *Plugin) Activate(app plugin.App) {
    host, ok := app.(plugin.AgentHost)
    if !ok || host.AgentToolRegistry() == nil {
        return
    }
    handle, err := host.AgentToolRegistry().Register("example-reports", agent.Tool{
        Name:         "example.reports.summary",
        Title:        "Get report summary",
        Description:  "Return a bounded summary without private row data.",
        InputSchema:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
        OutputSchema: json.RawMessage(`{"type":"object","required":["count"],"properties":{"count":{"type":"integer","minimum":0}},"additionalProperties":false}`),
        Mutability:   agent.MutabilityRead,
        Risk:         agent.RiskRead,
        Permission: agent.PermissionRequirement{
            Scope: "gopress:site:read", Resource: "dashboard", Action: "read",
        },
        Timeout: 5 * time.Second, MaxConcurrency: 4,
        Handler: func(ctx context.Context, call agent.Invocation) (any, error) {
            return map[string]any{"count": 0}, nil
        },
    })
    if err == nil {
        p.agentHandles = append(p.agentHandles, handle)
    }
}

func (p *Plugin) Deactivate(app plugin.App) {
    for _, handle := range p.agentHandles {
        handle.Revoke()
    }
    p.agentHandles = nil
}
```

Reusing `dashboard.read` is correct only when the summary truly has the same
safe semantics. Plugin-owned data normally needs plugin-owned scopes and RBAC.
The stock credential UI cannot issue arbitrary third-party scopes yet; do not
borrow unrelated Core permissions or create a weaker unaudited route.

## Handler and Permission Rules

- Accept standard `context.Context` and `agent.Invocation`, never Gin or MCP
  request types.
- Use plugin-owned services with the supplied context and bounded queries.
- Derive subject/owner from the refreshed Principal and stored resource, not
  request fields.
- Paginate lists and return only reviewed fields.
- Return stable `agent.Error` values and keep internal causes server-side.
- Respect cancellation and avoid untracked background work.

Resource-dependent authorization belongs in `ResolvePermission`: parse the ID,
load the stored owner/type, return `PermissionRequirement`, and do no mutation.
Missing and cross-type resources should use a consistent Not Found response.

## Write Tool Requirements and Current Gate

Every write must be idempotent, have a dedicated scope/action/risk, use a
required idempotency key, apply optimistic versioning to updates, separate
publish/destructive transitions, request confirmation where appropriate,
validate ownership in a transaction, return `ResultForResource`, and trigger
domain hooks/cache invalidation independently of MCP.

However, the current `gopress-mcp` Safe Write UI allow-lists only the six Core
write tools and derives issuable write scopes from them. A third-party write
Tool therefore remains blocked by stock policy even when registered. This is
an intentional safety gate, not something an extension should bypass.

## Schema and Lifecycle Guidance

- Use `additionalProperties: false` and bounded strings/arrays/numbers.
- Prefer type plus ID over a guessable ID alone.
- Keep outputs smaller than database DTOs and free of secrets/paths.
- Use the plugin namespace, not the Core-reserved `gopress.*` prefix.
- Roll back earlier handles if multi-tool activation partially fails.
- Test duplicate names, stale handles, deactivation, and revision changes.

Tests must cover missing scope/RBAC, owned vs foreign resources, cross-type ID,
schema limits and invalid output, concurrent idempotency, optimistic conflict,
panic/timeout/cancel audit, and clean deactivation.

Phase 5 needs a scope registry, admin approval, dynamic credential scope
discovery, write-policy providers, risk review, and compatibility tests before
third-party Safe Write is productized.
