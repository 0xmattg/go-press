# REST API 与 Swagger

## REST API 特性

GoPress 为每个注册的 ContentType 自动生成 REST 端点：

- **自动端点** — 核心类型和 `theme.toml` 声明的 ContentType 会自动生成 `GET /api/v1/{type}` 和 `GET /api/v1/{type}/:id`
- **通用查询** — 下面以主题声明的 `product` 内容类型为例：`GET /api/v1/content?type=product&status=published&search=hepa&page=1`
- **认证** — JWT Bearer Token + API Key 双模式
- **限流** — 基于 IP 的令牌桶限流
- **CORS** — 可配置跨域策略
- **Swagger 文档** — 代码注解自动生成，内置 Swagger UI（`/swagger/index.html`）

## 在线浏览

启动服务后访问：

- **Swagger UI**: `http://localhost:8080/swagger/index.html`
- **OpenAPI JSON**: `http://localhost:8080/swagger/doc.json`
- **OpenAPI YAML**: `docs/swagger.yaml`（仓库内）

## 认证

### JWT Bearer Token

```bash
# 获取 token
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"..."}'

# 使用
curl http://localhost:8080/api/v1/content \
  -H "Authorization: Bearer <token>"
```

### API Key

后台「用户管理」可为每个用户生成长期 API Key：

```bash
curl http://localhost:8080/api/v1/content \
  -H "X-API-Key: <key>"
```

## 通用查询参数

| 参数 | 说明 |
|---|---|
| `type` | 内容类型，例如核心 `post` / `contact_message`，或当前主题声明的 `product` / 自定义类型 |
| `status` | `published` / `draft` / `archived` |
| `search` | 全文模糊搜索 |
| `taxonomy` | 分类法过滤，如 `category:tech` |
| `page` | 分页页码（从 1 开始） |
| `per_page` | 每页条数（默认 20） |
| `sort` | 排序字段 + 方向，如 `created_at:desc` |
| `lang` | 语言代码（多语言插件启用时） |

## 自动生成 Swagger 文档

GoPress 使用 [swaggo/swag](https://github.com/swaggo/swag) 从代码注解自动生成 OpenAPI 文档：

```bash
# 生成/更新 Swagger 文档
go run ./cmd/gendoc/
```

输出到 `docs/docs.go` + `docs/swagger.json` + `docs/swagger.yaml`。

## 在 API handler 上添加注解

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

注解写在 handler 函数上方，`swag init` 时自动扫描。

## 文档分发

文字文档（你正在看的这部分）和 Swagger API 规范是**独立的两份产物**：

- `docs/guide/` — Markdown 文字文档（GitBook / MkDocs / Docusaurus 等都能读）
- `docs/{docs.go, swagger.json, swagger.yaml}` — Swagger Go 包 + OpenAPI 规范

两者不互相依赖，不会冲突。新机器克隆 repo 后跑一次 `go run ./cmd/gendoc/` 即可同步 Swagger，文字文档则直接读 markdown 文件。
