# GoPress MCP (Agent Access)

`gopress-mcp` is the optional first-party MCP protocol adapter for GoPress. It
exposes the protocol-neutral Core Agent Tool Registry and Executor as a remote,
site-scoped MCP server, allowing compatible clients to work inside the same
content, permission, and audit boundaries as the CMS.

This page focuses on plugin activation, client connection, and troubleshooting.
For Core Agent design, execution, security, and extension contracts, start with
the independent [Agent and MCP module](../agent/overview.md).

The current implementation is **Safe Write Beta (Phase 3)**. Phases 0–3 are
implemented; Phase 4 OAuth 2.1 and Phase 5 Resources, Prompts, Tasks, and MCP
Apps are not. The plugin is disabled by default and its Tool Profile remains
`read_only` after activation until an operator explicitly changes it.

## Current Capability

| Area | Implementation |
|---|---|
| Endpoint | `POST /mcp` below the configured public site URL |
| Transport | Stateless Streamable HTTP |
| Protocols | `2026-07-28`, with `2025-11-25` compatibility |
| SDK | Official `modelcontextprotocol/go-sdk` `v1.7.0-pre.3` |
| Authentication | Short-lived administrator-issued Bearer tokens |
| Tools | 6 read tools and 6 controlled write tools |
| Default | Plugin inactive; active profile is `read_only` |
| Discovery | Filtered by credential scopes, current Core RBAC, and Tool Policy; private 30-second cache |
| Audit | Principal, tool, status, duration, argument digest, and result digest; no token or argument values |

This is not automatic REST-to-Tool conversion. The plugin owns MCP transport,
protocol negotiation, and authentication mapping. Core owns authorization,
ownership checks, validation, idempotency, audit, domain commands, hooks, and
cache invalidation.

```mermaid
flowchart LR
    C["MCP client"] -->|"Bearer + POST /mcp"| P["gopress-mcp plugin"]
    P --> E["Core Agent Executor"]
    E --> A["Scope + RBAC + ownership + Tool Policy"]
    E --> R["Agent Tool Registry"]
    R --> D["Content / Taxonomy / Media services"]
    D --> H["Hooks + cache invalidation"]
    E --> U["Agent audit + idempotency"]
```

## Enable and Connect

1. Configure the canonical public site URL. Production endpoints should use
   HTTPS; plain HTTP is accepted only for `localhost`, `127.0.0.1`, and `::1`.
   Credentials are audience-bound to the resulting URL, such as
   `https://example.com/mcp`.
2. Activate **GoPress MCP** from the admin plugin page. The `/mcp` and plugin
   admin routes exist only while the plugin is active.
3. Open `/admin/plugins/gopress-mcp/settings`.
4. In **Access credentials**, issue a token with the smallest required scopes.
   The default expiry is 30 days and the valid range is 1–90 days. The secret
   is displayed once; only its one-way digest is persisted.
5. Copy the generic client configuration from **Connection overview**:

```json
{
  "mcpServers": {
    "gopress": {
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": {
        "Authorization": "Bearer gp_agent_REPLACE_WITH_YOUR_TOKEN"
      }
    }
  }
}
```

Client-specific outer configuration can vary. The HTTP URL and Authorization
header are the stable parts. Never commit a real token to source control.

The settings page has four tabs: connection and diagnostics, write policy,
credentials, and audit. Dedicated `mcp.*`, `agent_credential.*`, and
`agent_audit.*` permissions protect the page and its custom handlers. Default
roles leave these controls to the super administrator.

## Verify with curl

A real MCP client or Inspector is preferred. For a minimal compatibility-path
check, initialize with protocol `2025-11-25`:

```bash
export GOPRESS_MCP_URL='https://example.com/mcp'
export GOPRESS_MCP_TOKEN='gp_agent_REPLACE_WITH_YOUR_TOKEN'

curl --fail-with-body -sS \
  -X POST "$GOPRESS_MCP_URL" \
  -H "Authorization: Bearer $GOPRESS_MCP_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data-binary '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2025-11-25",
      "capabilities": {},
      "clientInfo": {"name": "curl-test", "version": "1.0.0"}
    }
  }'
```

List the tools visible to this credential:

```bash
curl --fail-with-body -sS \
  -X POST "$GOPRESS_MCP_URL" \
  -H "Authorization: Bearer $GOPRESS_MCP_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'Mcp-Protocol-Version: 2025-11-25' \
  --data-binary '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/list",
    "params": {}
  }'
```

A partial list is expected when scopes, current RBAC, or Tool Policy do not
permit every tool. List results use `cacheScope: private`, `ttlMs: 30000`, and
an Agent capability revision.

## Scopes and Tools

### Read tools

| Tool | Scope | Core RBAC | Main arguments |
|---|---|---|---|
| `gopress.site.get` | `gopress:site:read` | `dashboard.read` | none |
| `gopress.content_types.list` | `gopress:content:read` | `content.read` | none |
| `gopress.content.list` | `gopress:content:read` | `content.read` | `content_type`; optional `status`, `search`, taxonomy filters, and pagination |
| `gopress.content.get` | `gopress:content:read` | `content.read` | `content_type`, `id` |
| `gopress.taxonomy.list` | `gopress:taxonomy:read` | `taxonomy.read` | `content_type`; optional `taxonomy` |
| `gopress.media.list` | `gopress:media:read` | `media.read` | optional `mime_type`, `page`, `per_page` |

Content status is limited to `published`, `pending`, `draft`, `archived`, and
`trash`; the default is `published`. Lists default to 20 rows and allow at most
100. Content details include only registered metadata and sanitized HTML. Media
results do not expose server filesystem paths.

### Write tools

| Tool | Scope | Core RBAC | Additional controls |
|---|---|---|---|
| `gopress.content.create_draft` | `gopress:content:write` | `content.create` | Draft only; `idempotency_key` required |
| `gopress.content.update` | `gopress:content:write` | `content.update` or owned `content.update_own` | Optimistic timestamp and idempotency key |
| `gopress.content.publish` | `gopress:content:publish` | `content.publish` | Optimistic timestamp, idempotency key, and `confirm: true` |
| `gopress.content.move_to_trash` | `gopress:content:write` | `content.delete` or owned `content.delete_own` | Soft delete, optimistic timestamp, key, and confirmation |
| `gopress.content.restore` | `gopress:content:write` | `content.update` | Restores trash to draft; optimistic timestamp and key |
| `gopress.media.update_metadata` | `gopress:media:write` | `media.update` or owned `media.update_own` | Only `alt_text`, `title`, and `caption`; optimistic timestamp and key |

Write tools accept only registered editable content types. Draft creation
validates declared fields, parent type, metadata, unique slugs, and sanitized
content. A generic update cannot smuggle in a status transition; publish,
trash, and restore use separate tools.

## Safe Write Policy

A write call succeeds only when all four controls agree:

```text
site profile == safe_write
AND the individual write tool is enabled
AND the credential contains its gopress:* scope
AND the current principal has the Core resource.action permission
```

Start with draft creation and content update. Publish and trash are never
enabled merely by choosing `safe_write`; each requires its own switch. The
default `content.publish` capability is assigned only to editors and the super
administrator, so an author cannot gain publication rights from a token scope.

Every write argument object needs an 8–200 character `idempotency_key`.
Retrying the same tool with the same credential, key, and arguments replays the
stored result. Reusing a key with different arguments conflicts. Updates and
transitions also require the most recently read RFC 3339 `updated_at` as
`expected_updated_at`; stale writes are rejected.

Draft call arguments:

```json
{
  "name": "gopress.content.create_draft",
  "arguments": {
    "content_type": "post",
    "title": "Agent-created draft",
    "content": "<p>Body</p>",
    "idempotency_key": "draft-20260809-0001"
  }
}
```

Publish call arguments:

```json
{
  "name": "gopress.content.publish",
  "arguments": {
    "content_type": "post",
    "id": 42,
    "expected_updated_at": "2026-08-09T08:30:00.123456Z",
    "idempotency_key": "publish-post-42-0001",
    "confirm": true
  }
}
```

## Security and Operations

- `/mcp` accepts only dedicated Agent Bearer tokens, not admin cookies, admin
  JWTs, or REST API keys.
- Tokens bind a subject, scopes, audience, expiry, and revocation state. Current
  account activation and role are reloaded for every execution.
- Arguments use restricted JSON Schemas, a 64 KiB Tool-argument limit, and a
  depth limit of 16. HTTP bodies are capped at 256 KiB and outputs at 1 MiB.
- Cross-origin protection, `private, no-store`, request cancellation, and a
  baseline limit of 120 requests per source per minute are enabled.
- Tool-specific timeouts and concurrency limits protect domain services.
- Type and ownership checks prevent cross-type or cross-owner ID access.
- Audit states include `started`, `succeeded`, `denied`, `failed`, and
  `replayed`. Argument values and Bearer tokens are never persisted. Execution
  fails closed if audit storage is unavailable.

At a reverse proxy, preserve `Authorization`, `Content-Type`, `Accept`, and all
`Mcp-*` headers; do not cache `/mcp`; use HTTPS; and keep the externally visible
URL identical to the configured public site URL. Store tokens only in a client
secret store or environment variable and rotate them regularly.

Protocol transport is stateless, while credentials, idempotency records, and
audits use the site database. There is no browser OAuth flow, refresh token, or
automatic scope elevation before Phase 4.

## Troubleshooting

| Symptom | Check |
|---|---|
| `/mcp` returns 404 | Plugin activation and router rebuild |
| 401 | Missing, expired, revoked, or audience-mismatched token |
| `insufficient_scope` | Required credential scope |
| `permission_denied` | Current Core role and resource ownership |
| `risk_denied` | `safe_write` profile and individual Tool switch |
| `confirmation_required` | `confirm: true` for publish or trash |
| `conflict` | Stale `expected_updated_at` or reused key with changed arguments |
| Fewer than 12 tools | Expected permission-filtered discovery; inspect policy, scopes, and RBAC |

Connection diagnostics report endpoint, transport, authentication, supported
protocols, SDK/plugin versions, registry and policy revisions, registered tool
count, and secure transport state. The audit tab identifies whether a request
was rejected by authorization, policy, or the domain command.

## Not Implemented Yet

- OAuth 2.1 resource metadata, browser authorization, PKCE, refresh-token
  rotation, and step-up scopes.
- MCP Resources, Prompts, Tasks, MCP Apps, subscriptions, or registry
  publication.
- User and role management, plugin or theme changes, arbitrary site settings,
  file deletion, database maintenance, refunds, or payment operations.
- A claim of compatibility with every MCP client or access to every admin
  operation.

The implementation-oriented design is documented in the
[Agent and MCP module](../agent/overview.md). Full phase decisions, threat model,
and later phases are currently documented in the
[Chinese Agent and MCP capability plan](../../zh-CN/architecture/mcp-agent-capability-plan.md).
