# 内容与分类 Scope API

GoPress 为内容与 taxonomy 查询分别提供请求级 Scope API。插件可以据此注入
语言、租户、频道、可见性或预览约束，而无需让 core 仓储理解某个具体扩展。

两组 Scope 相互独立，因为内容行与 taxonomy identity 的查询和写入规则不同，
但整体数据流一致：

```text
插件中间件
  -> core AddScope API
  -> 请求 Context
  -> Admin / REST / BaseTheme / 自定义 PageService
  -> 带 Scope 的仓储与命令服务
```

没有注册 Scope 时，两套 API 都保持原来的单语言行为。

## Content Scope

在当前 Gin 请求上注册内容过滤器：

```go
content.AddContentScope(c, func(db *gorm.DB) *gorm.DB {
    return db.Where("visible = ?", true)
})
```

主要内容 API：

- `content.AddContentScope(c, fn)` — 追加请求级 GORM filter。
- `content.RequestContext(c)` — 把 Gin 状态桥接到 core 服务使用的标准
  `context.Context`。
- `content.ScopedDB(c, db)` / `ScopedDBContext(ctx, db)` — 在隔离 Session 上
  应用全部 Scope。
- `Repository.FindBySlugScoped` 及其它 context-aware 仓储方法 — 让详情查询
  与列表查询处于相同 Scope。
- `EnsureUniqueSlugScoped` — 在当前 Scope 内校验 Slug 唯一性。

同 Slug 的多语言内容依赖这项一致性：列表、详情、保存和后台操作必须消费同一
请求 Context。

## Taxonomy Scope

Taxonomy Scope 同时携带 opaque key 与查询函数：

```go
taxonomy.AddScope(c, taxonomy.Scope{
    Key: "variant-a",
    Apply: func(db *gorm.DB) *gorm.DB {
        return db.Where("taxonomies.id IN (?)", visibleTaxonomyIDs)
    },
})
```

Core 不解释 `Scope.Key`。扩展可以用它把新 taxonomy 行关联到约束当前请求的
同一个变体。

公开 taxonomy Scope 能力包括：

- `taxonomy.AddScope(c, scope)` — 同时写入 Gin 请求及其标准 Context。
- `taxonomy.WithScope(ctx, scope)` — 在不依赖 Gin 的路径追加 Scope。
- `taxonomy.RequestContext(c)` — 获取仓储和命令服务消费的标准 Context。
- `taxonomy.Scopes(ctx)` / `ScopeKey(ctx)` — 读取通用 Scope 链；key 仍由扩展
  自己解释。
- `taxonomy.ScopedDB` / `ScopedDBContext` — 应用完整 Scope 链。
- `Repository.WithContext(ctx)` — 克隆请求级 taxonomy repository。

带 Scope 的仓储读取覆盖：

- 按 Slug 查找 term；
- 按 ID 或 taxonomy type + Slug 查找 taxonomy identity；
- 扁平列表与层级树；
- 实时内容引用次数；
- 某条内容关联的 taxonomy 项。

BaseTheme taxonomy 归档、归档筛选数据、内容徽标、REST term 解析、后台分类/
标签列表及内容表单选择器都会走这些 context-aware 路径。

## Scope 安全的分类写入

`taxonomy.CommandService` 是统一写入边界。后台和扩展工作流把
`taxonomy.RequestContext(c)` 传给创建、更新、删除及内容关系操作。

命令服务统一校验：

- taxonomy 类型已经注册；
- 名称和 Slug 非空；
- Slug 在当前 Scope 内唯一；
- 层级父项属于相同 taxonomy 类型与 Scope，且不会产生循环；
- 提交的关系 ID 均属于允许的 taxonomy 类型与当前 Scope；
- 写入在事务完成后才通知 mutation observer。

这些属于领域校验，不代替授权。HTTP/Admin transport 调用命令服务前仍必须
分别检查 `taxonomy.read`、`taxonomy.create`、`taxonomy.update` 或
`taxonomy.delete` capability。

## 主题如何消费

使用 BaseTheme 配置驱动路由的主题不需要添加 Scope 代码。自定义主题服务应
嵌入 `coreTheme.BasePageService`，并为每个请求创建克隆：

```go
func (h *Handler) ProductsList(c *gin.Context) {
    service := h.pageService.ForRequest(c)
    data, _ := service.GetProductsData()
    c.HTML(http.StatusOK, "products", data)
}
```

`ForRequest(c)` 会同时携带 content 与 taxonomy Context。自定义详情查询应使用
带 Scope 的仓储方法，不能用裸 DB 重新创建无 Scope repository。

## 插件与后台接入

内置 multilang 展示了完整组合方式，core 与插件之间没有硬编码耦合：

1. Early middleware 判断当前规范 URL 的语言。
2. 它注册 Content Scope，并为启用独立翻译的 taxonomy 注册 Taxonomy Scope。
3. Core 后台列表/选择器、BaseTheme、REST、SEO 和命令服务消费这些通用 Scope。
4. `admin.content_list.tabs` 与 `admin.taxonomy_list.tabs` 提供语言 Tab，core
   不需要知道“语言”的含义。
5. 通用 taxonomy mutation 通知让插件维护自己的翻译组数据。

Category/Tag 翻译策略与 URL 行为见[多语言插件](../plugins/multilang.md)。

## 契约检查表

- Scope 只在当前请求内有效，不能跨请求泄漏。
- 列表与详情读取都必须应用 Scope。
- 写入服务必须接收同一个标准 Context。
- 前端过滤不是授权；transport 负责 RBAC，领域服务负责所有权、类型与 Scope
  不变量。
- 主题与插件只通过 core 的 Scope、仓储、Hook、命令和模板 helper 交互。
- 没有注册 Scope 时，现有单语言数据与 URL 保持历史行为。
