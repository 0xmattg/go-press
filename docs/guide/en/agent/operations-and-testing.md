# Operations and Testing

MCP is a public HTTP surface backed by short-lived credentials and persistent
idempotency/audit state. Readiness requires application, proxy, client, and
database checks, not merely a 200 response from `/mcp`.

## Admin Surface

`/admin/plugins/gopress-mcp/settings` provides connection/diagnostics, write
policy, credential issue/revocation, and filtered audit tabs. Dedicated
permissions are `mcp.read/update`, `agent_credential.read/create/delete`, and
`agent_audit.read`; each custom handler uses Core authentication/RBAC, and
state-changing admin requests receive same-origin protection.

Diagnostics reports endpoint, stateless transport, Bearer authentication,
protocols, SDK/plugin versions, registry/tool counts, policy/revisions, enabled
write tools, and secure transport. `ready: true` does not mean the caller has
all Tool permissions or that OAuth exists.

## Reverse Proxy

- Make external URL and `site.url` identical and use HTTPS.
- Preserve Authorization, Content-Type, Accept, and all `Mcp-*` headers.
- Do not cache `/mcp`; preserve private no-store responses.
- Support POST and Streamable HTTP response behavior.
- Set body and timeout limits compatible with GoPress's tighter Tool bounds.
- Propagate client disconnects and never log Authorization or full bodies.
- Configure trusted proxy/source handling deliberately; application baseline
  rate limiting currently keys the connection source.

## Multiple Instances

Both protocol revisions use stateless HTTP. Shared PostgreSQL holds
credentials, idempotency, and audit, so instances for one site must use the
same database and prefix. Per-Tool concurrency gates and source-rate buckets
are process-local, and full cross-instance metrics/tracing remain Phase 4.

## Common Failures

| Error | Check |
|---|---|
| 404 | Plugin activation and router rebuild |
| 401 / `unauthenticated` | Missing, expired, revoked, inactive-subject, or audience-mismatched token |
| `insufficient_scope` | Issue only the required missing scope |
| `permission_denied` | Current role and stored owner |
| `risk_denied` | Safe Write profile and individual Tool switch |
| `confirmation_required` | Fresh read plus literal `confirm: true` |
| `idempotency_required` | 8–200 character request key |
| `idempotency_pending` | Wait and retry identical arguments with the same key |
| `conflict` | Re-read stale resource or use a new key for changed intent |
| `invalid_arguments` | Use the schema from `tools/list` |
| `audit_unavailable` | Restore database/audit health; do not bypass |

Audit filters help distinguish denied authorization, domain failure, and
idempotent replay without exposing argument values.

## Test Layers

Core tests cover Registry concurrency/revision/handles, schema limits, scope
plus RBAC plus ownership, principal refresh and credential lifecycle,
idempotency convergence/replay, read-only policy, panic/timeout/error mapping,
and audit fail-closed behavior.

Domain tests cover Command Service behavior without Gin, type/ID and metadata
boundaries, sanitization, optimistic locking, explicit transitions, media
ownership, hooks, and cache invalidation.

MCP tests cover the official latest client, legacy initialization, unsupported
versions, missing/wrong credentials, permission-filtered discovery, audited
explicit hidden calls, Safe Write and replay, cross-origin/body/rate controls,
and admin RBAC/ownership/policy persistence.

Recommended commands:

```bash
go test ./core/agent ./core/content ./core/media ./core/audit ./plugins/gopress-mcp
go test -race ./core/agent ./core/content ./core/media ./plugins/gopress-mcp
go vet ./core/agent ./core/content ./core/media ./plugins/gopress-mcp
go test ./...
```

## Release Checklist

- [ ] Plugin active only on intended sites.
- [ ] HTTPS endpoint exactly matches credential audience.
- [ ] Read-only default verified.
- [ ] Separate short-lived least-scope token per Agent.
- [ ] No token in Git, logs, screenshots, or metric labels.
- [ ] Unauthorized roles, foreign owners, and cross-type IDs denied.
- [ ] Only task-required Safe Write Tools enabled.
- [ ] Confirmation, optimistic conflict, and idempotent retry tested.
- [ ] Success, denial, failure, and replay audit visible.
- [ ] Revocation, account disablement, and role lowering take effect next call.
- [ ] Proxy preserves headers, avoids caching, and does not log secrets.
- [ ] Agent tables included in backup/restore validation.
- [ ] OAuth and advanced MCP primitives clearly marked unavailable.

Until Phase 4, avoid claims of production OAuth, universal MCP-client
compatibility, or access to every admin operation. Suitable Beta deployments
use controlled sites, explicit operators, short-lived credentials, minimal Tool
sets, reproducible tests, and reversible content workflows.
