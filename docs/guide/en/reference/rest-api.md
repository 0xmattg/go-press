# REST API and Swagger

GoPress generates REST endpoints for registered content types and ships Swagger documentation for API discovery.

## Features

- Automatic endpoints for core and theme-declared content types.
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

## Common Query Parameters

| Parameter | Description |
|---|---|
| `type` | Content type, such as `post` or a theme-defined type. |
| `status` | `published`, `draft`, or `archived`. |
| `search` | Text search. |
| `taxonomy` | Taxonomy filter such as `category:tech`. |
| `page` | Page number, starting at 1. |
| `per_page` | Items per page. |
| `sort` | Field and direction, such as `created_at:desc`. |
| `lang` | Language code when multilingual support is active. |

## Regenerate Swagger

```bash
go run ./cmd/gendoc
```

The command updates `docs/docs.go`, `docs/swagger.json`, and `docs/swagger.yaml`.

