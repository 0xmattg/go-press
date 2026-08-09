# Identity, Authorization, and Security

The endpoint does not treat possession of any token as sufficient. Every
machine request must represent an identifiable and revocable GoPress subject,
and every operation must pass scope, current RBAC, ownership, risk, and data
consistency controls.

## Principal and Credential

A Principal carries kind (`user` or `service_account`), subject ID, username,
current role, scopes, audience, and credential ID. The Executor refreshes it on
every call, so account disablement, role changes, service-account disablement,
expiry, or revocation apply immediately.

The stock MCP admin currently issues user credentials for the current
administrator. Core service accounts exist but do not yet have a complete MCP
admin delegation UI.

Credentials use a high-entropy `gp_agent_` secret. Only a SHA-256 digest and
display prefix are persisted. A token binds subject, scopes, exact audience,
expiry, and creator; default expiry is 30 days and maximum is 90. The raw token
is displayed once. Revocation uses credential ID plus subject to prevent
cross-user IDOR.

Changing the canonical site scheme, host, or path changes the `/mcp` audience
and requires replacement credentials.

## Scope AND Current RBAC AND Ownership

Authorization requires the credential scope and current Core
`resource.action`. An own action is considered only when the server-resolved
resource owner equals the Principal subject. Client-supplied owner fields are
not accepted.

Discovery is not final authorization. A Tool that may operate on owned
resources can be listed, but `ResolvePermission` loads the concrete resource
and owner again at execution.

## Site Risk Policy

`read_only` blocks every write Tool. `safe_write` still allows only explicitly
enabled write names. Policy and credential scope are independent: enabling a
Tool does not upgrade an old token, and issuing a scope does not enable a Tool.

## Type and ID Boundaries

Content operations require both `content_type` and ID. Cross-type IDs return
Not Found. Content ownership comes from stored `AuthorID`; media ownership from
`UploadedBy`; parents must belong to the same content type.

## Idempotency and Optimistic Locking

Every write uses an 8–200 character key unique by credential, Tool, and key.
Arguments are canonically hashed. Completed identical calls replay the stored
result, in-progress calls return `idempotency_pending`, failed calls replay the
stable error, and changed arguments with a reused key conflict. The default
record TTL is 24 hours.

Updates, transitions, and media metadata writes also require the latest RFC
3339 `expected_updated_at`. Stale writes conflict and must re-read rather than
overwrite. Publish and trash additionally require literal `confirm: true`;
annotations or natural-language claims of approval are not authorization.

## Content and Prompt Injection

Content and metadata remain untrusted even after an Agent reads them. They
cannot change service-side scopes, RBAC, policy, or confirmation. HTML remains
sanitized, and no Tool exposes shell, SQL, arbitrary Go/template execution,
user/role management, extension activation, arbitrary options, hard deletion,
payments, or refunds.

Clients should still treat returned content as untrusted prompt context and use
human review for high-risk workflows.

## Audit

Audit states are `started`, `succeeded`, `denied`, `failed`, and `replayed`.
Events capture request/trace, adapter/protocol/client, principal/credential,
Tool owner and risk, status/error/duration, argument size/hash/top-level keys,
result hash, and a keyed source digest.

Bearer tokens, argument values, content bodies, and database causes are not
stored. Unavailable audit storage causes `audit_unavailable` and stops
execution.

## HTTP Controls

- Dedicated Bearer credentials only; no admin cookie/JWT or REST API key.
- Supported protocol header enforcement.
- 256 KiB HTTP body and separate 64 KiB Tool-argument limits.
- Go cross-origin protection.
- Private no-store, pragma no-cache, and nosniff responses.
- Baseline 120 requests per source per minute with bounded bucket count.
- Stateless Streamable HTTP, cancellation propagation, Tool timeout, and
  concurrency gates.
- HTTPS for every non-loopback endpoint.

The reverse proxy must preserve Authorization and `Mcp-*` headers, avoid shared
caching and secret logging, and provide deployment-level TLS, trusted-proxy,
WAF, and DDoS controls.

OAuth 2.1 resource metadata, Authorization Code plus PKCE, refresh-token
rotation, step-up scopes, complete OpenTelemetry, and automated audit retention
are Phase 4 work. Current manually copied credentials keep the feature in Beta.
