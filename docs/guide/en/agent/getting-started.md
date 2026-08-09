# Agent and MCP Getting Started

Use this flow on a local or test site to exercise the current Phase 3
capability. MCP remains Beta until OAuth 2.1, proxy security review, and broader
real-client compatibility work are complete.

## 1. Prerequisites

- GoPress installation and database migrations are complete.
- `site.url` is the canonical URL clients actually use.
- Production uses HTTPS; plain HTTP is limited to loopback development.
- You have the super-administrator permissions required for plugin and MCP
  controls.
- Your client supports remote Streamable HTTP, or you will begin with curl.

Core migrations create the prefixed Agent tables for service accounts,
credentials, idempotency records, and audit events. No plugin SQL step is
required.

## 2. Activate GoPress MCP

Activate **GoPress MCP** from the admin plugin page, then open:

```text
/admin/plugins/gopress-mcp/settings
```

Connection overview should report `{site.url}/mcp`, stateless Streamable HTTP,
protocols `2026-07-28` and `2025-11-25`, and secure transport for non-loopback
sites. Deactivation removes the route on router rebuild.

## 3. Issue a Read Credential

Create a 30-day test credential and select only the required read scopes:

```text
gopress:site:read
gopress:content:read
gopress:taxonomy:read
gopress:media:read
```

The `gp_agent_` token is displayed once. Move it immediately into the client
secret store or a temporary environment variable; never commit it.

## 4. Configure a Client

```json
{
  "mcpServers": {
    "gopress": {
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": {
        "Authorization": "Bearer gp_agent_REPLACE_WITH_TOKEN"
      }
    }
  }
}
```

Client-specific outer syntax can differ. Current clients negotiate
`2026-07-28`; clients that still use initialization can use the
`2025-11-25` compatibility path.

## 5. Verify Reads

Call these tools in order:

1. `gopress.site.get` for canonical site data, Core version, and Agent revision.
2. `gopress.content_types.list` for current type and field declarations.
3. `gopress.content.list` with `content_type: "post"`.
4. `gopress.content.get` with the returned ID and the same content type.

A partial `tools/list` is expected when the credential lacks taxonomy or media
scopes. Discovery is filtered by scope and current RBAC, then privately cached
for 30 seconds.

## 6. Enable the Smallest Safe Write

In Write policy:

1. Change the profile to `safe_write`.
2. Enable only `gopress.content.create_draft`.
3. Save, then issue a new test credential containing
   `gopress:content:write`.

Existing read tokens do not gain write scope automatically.

Draft arguments:

```json
{
  "content_type": "post",
  "title": "Agent smoke-test draft",
  "content": "<p>Test only.</p>",
  "idempotency_key": "draft-smoke-20260809-0001"
}
```

The result must remain a draft and bind its author to the credential subject.
An identical retry replays the saved result. Unknown fields are rejected, and
the audit should show started/succeeded followed by replayed for the retry.

## 7. Verify Locking and Confirmation

For update, first read the current `updated_at`, pass it as
`expected_updated_at`, and use a fresh idempotency key. Reusing the old
timestamp after a successful edit must return `conflict`.

Publish and trash additionally require the individual Tool switch, the matching
scope, current RBAC, a fresh optimistic timestamp and idempotency key, and
`confirm: true`.

## 8. Verify Revocation

Revoke the test token from Access credentials and call a tool again. It must
fail immediately. Disabling the account or lowering its role has the same
next-call effect because the Executor reloads the principal.

## Next Steps

- [Layered Architecture](architecture.md)
- [Tools and Execution](tools-and-execution.md)
- [Identity, Authorization, and Security](security.md)
- [Operations and Testing](operations-and-testing.md)
- [Complete plugin and curl guide](../plugins/gopress-mcp.md)
