# REST API and Swagger

GoPress generates REST endpoints for registered content types and ships Swagger documentation for API discovery.

## Features

- Automatic endpoints for public core and theme-declared content types.
- Generic content query endpoint.
- JWT Bearer token and API key authentication.
- IP-based rate limiting.
- Configurable CORS.
- Swagger UI at `/swagger/index.html`.

## Browse the API

After starting the server:

- Swagger UI: `http://localhost:8080/swagger/index.html`
- OpenAPI JSON: `http://localhost:8080/swagger/doc.json`
- OpenAPI YAML: `docs/swagger.yaml`

## Authentication

> REST and MCP are separate security surfaces. The admin JWT and `X-API-Key`
> below apply to REST only. `/mcp` rejects them and uses dedicated Agent Bearer
> tokens, scopes, current RBAC, and Tool Policy. See
> [GoPress MCP (Agent Access)](../plugins/gopress-mcp.md).

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"..."}'

curl http://localhost:8080/api/v1/content \
  -H "Authorization: Bearer <token>"
```

API keys can be generated from the admin user management page:

```bash
curl http://localhost:8080/api/v1/content \
  -H "X-API-Key: <key>"
```

Authentication does not replace authorization. Every protected handler must
check the capability for its operation, and a resource ID from a path, query,
or JSON body must be validated for type, scope, and ownership.

## Common Query Parameters

The public REST API only exposes content types with a public archive and only
returns published content whose publication time has arrived. Internal types
such as `contact_message`, drafts, archived rows, trash, and scheduled content
are never exposed by these endpoints. Administrative access must use protected
admin workflows instead.

| Parameter | Description |
|---|---|
| `type` | Public content type, such as `post` or an archive-enabled theme type. |
| `status` | Optional; only `published` is accepted. |
| `search` | Text search. |
| `taxonomy` | Taxonomy filter such as `category:tech`. |
| `page` | Page number, starting at 1. |
| `per_page` | Items per page. |
| `sort` | Field and direction, such as `created_at:desc`. |
| `lang` | Language code when multilingual support is active; content and taxonomy filters use the same request scope. |

## Regenerate Swagger

```bash
go run ./cmd/gendoc
```

The command updates `docs/docs.go`, `docs/swagger.json`, and `docs/swagger.yaml`.

## Handler Annotations

```go
// @Summary     List content items
// @Tags        Content
// @Param       page query int false "Page number" default(1)
// @Param       per_page query int false "Items per page" default(20)
// @Success     200 {object} response{data=[]contentDTO}
// @Failure     400 {object} errorResponse
// @Router      /content [get]
func (h *Handler) ListContent(c *gin.Context) { ... }
```

`cmd/gendoc` scans handler annotations and regenerates the checked-in OpenAPI
artifacts.

## Documentation Outputs

The guide and API specification are separate products:

- `docs/guide/` contains the bilingual Markdown documentation.
- `docs/docs.go`, `docs/swagger.json`, and `docs/swagger.yaml` contain the
  generated Swagger package and OpenAPI definitions.

Updating one does not silently regenerate the other.
