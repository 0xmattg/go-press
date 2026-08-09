# Tools and Execution Pipeline

A Tool is the smallest executable Core Agent capability. It describes a stable
name, restricted schemas, risk, permission, execution bounds, and handler
without containing MCP or HTTP types.

## Tool Contract

| Field | Meaning |
|---|---|
| `Name` | Globally stable lower-case segmented name |
| `Title`, `Description` | Client/model guidance, never authorization |
| `InputSchema`, `OutputSchema` | Core-validated restricted JSON Schemas |
| `Mutability` | `read` or `write` |
| `Risk` | `read`, `write`, `publish`, `destructive`, or `critical` |
| `Idempotent` | Mandatory for every write Tool |
| `RequiresConfirmation` | Requires `confirm: true` before high-risk execution |
| `Permission` | Scope, `resource.action`, and optional own action |
| `ResolvePermission` | Resolves concrete type/resource/owner from arguments |
| `Timeout`, `MaxConcurrency` | Bounded execution |
| `Handler` | Context-aware domain entry after authorization |

The Registry rejects duplicate names, invalid schemas/permissions, non-read
risk on read tools, non-idempotent writes, and confirmation on non-write tools.

## Restricted Schemas

Supported keywords are limited to the execution needs: `type`, `properties`,
`required`, `additionalProperties`, `items`, `enum`, length/range/item bounds,
and documentation fields. Objects must explicitly declare
`additionalProperties`.

Defaults are 64 KiB arguments, 1 MiB output, JSON depth 16, and 64 KiB per
schema. Inputs and outputs are both validated. A handler result outside its
output schema becomes `invalid_result` rather than leaking an undeclared shape.

## Registry Semantics

- Global Tool-name uniqueness and deterministic sorted snapshots.
- Owner-aware registration and exact revocable handles.
- Generations prevent stale handles from removing a later same-name tool.
- `RevokeOwner` supports complete extension cleanup.
- Public snapshots strip executable handlers and permission resolvers.
- Every material registration change increments the revision.

## Mandatory Executor Order

1. Validate request ID, Tool name, and call envelope.
2. Reload the Principal, current role, and account state.
3. Resolve the Tool from the Registry.
4. Validate argument schema, size, and depth.
5. Resolve concrete permission and ownership facts.
6. Enforce token scope AND current Core RBAC AND ownership.
7. Enforce site Tool Profile and per-tool policy.
8. Require explicit confirmation where declared.
9. Record start; fail closed if audit is unavailable.
10. Reserve the write idempotency key.
11. Acquire the Tool concurrency slot and timeout context.
12. Invoke the handler with the refreshed principal in context.
13. Extract an optional `ResourceResult`, serialize, and validate output.
14. Complete idempotency state.
15. Audit success, failure, denial, or replay.

Handler panics are recovered into safe internal errors. Context deadline and
cancellation map to stable `timeout` and `canceled` codes.

## Read Tools

| Tool | Scope / RBAC | Boundary |
|---|---|---|
| `gopress.site.get` | `gopress:site:read` + `dashboard.read` | Safe site metadata and Agent revision |
| `gopress.content_types.list` | `gopress:content:read` + `content.read` | Registered types, supports, taxonomies, declared fields |
| `gopress.content.list` | `gopress:content:read` + `content.read` | Required type; bounded status/search/taxonomy/pagination |
| `gopress.content.get` | `gopress:content:read` + `content.read` | Required type plus ID; sanitized body and allow-listed meta |
| `gopress.taxonomy.list` | `gopress:taxonomy:read` + `taxonomy.read` | Only taxonomies attached to the requested type |
| `gopress.media.list` | `gopress:media:read` + `media.read` | Bounded metadata without filesystem paths |

Content lists accept only `published`, `pending`, `draft`, `archived`, and
`trash`; the default is published. Pagination defaults to 20 and caps at 100.

## Write Tools

| Tool | Scope / RBAC | Controls |
|---|---|---|
| `gopress.content.create_draft` | `gopress:content:write` + `content.create` | Draft only; type/field/meta/parent/slug/sanitization and idempotency |
| `gopress.content.update` | `gopress:content:write` + `content.update` or `update_own` | Type plus ID plus owner, optimistic lock, no status mutation |
| `gopress.content.publish` | `gopress:content:publish` + `content.publish` | Separate command, lock, key, `confirm: true` |
| `gopress.content.move_to_trash` | `gopress:content:write` + `content.delete` or `delete_own` | Soft delete, lock, key, confirmation |
| `gopress.content.restore` | `gopress:content:write` + `content.update` | Trash to draft only, lock and key |
| `gopress.media.update_metadata` | `gopress:media:write` + `media.update` or `update_own` | Alt/title/caption only, owner, lock and key |

Editors and super administrators can publish by default. Authors cannot gain
that right merely from a token scope.

## Dynamic Content Types

Clients discover current types first and pass `content_type` to generic tools.
Core then checks whether the type exists and is editable, which features and
metadata it declares, whether a parent belongs to the same type, and which
taxonomies are attached. This keeps the Tool surface small and theme-neutral.

## ResourceResult and Stable Errors

Write handlers can return `ResultForResource(value, type, id)`. The public
output remains `value`, while idempotency stores the resource reference for
replay and internal diagnosis.

Protocol-neutral errors include request/schema errors, authentication and
scope/RBAC denial, not found/conflict, risk/confirmation denial, idempotency
required/pending, timeout/cancel, audit unavailable, and internal error. The
MCP adapter maps these to structured Tool errors and hides internal causes.
