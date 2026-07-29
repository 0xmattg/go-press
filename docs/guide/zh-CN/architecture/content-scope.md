# Content Scope API

GoPress 引擎提供了核心级的请求上下文内容过滤机制，实现**插件与主题的完全解耦**。

## 为什么需要 Scope

很多 CMS 扩展都需要改变内容可见性，例如按语言过滤内容、隐藏私有变体、
应用租户或频道边界，以及只向已登录用户开放草稿预览。与其让每一个仓储
方法理解所有插件，GoPress 把过滤条件保存在当前请求上下文中。

## 设计模式

```
                   ┌─────────────────────────┐
                   │   Plugin (e.g. multilang)│
                   │ content.AddContentScope()│  ← 注册 scope（core API）
                   └────────────┬────────────┘
                                ▼
                   ┌─────────────────────────┐
           core    │  gin.Context 中间件链     │  ← scope 存储在请求上下文
                   └────────────┬────────────┘
                                ▼
                   ┌─────────────────────────┐
                   │  BaseTheme / PageService │
                   │  content.ScopedDB(c, db) │  ← 读取 scope（core API）
                   └─────────────────────────┘
```

## 核心 API

- **`content.AddContentScope(c, fn)`** — 插件在中间件中注册 GORM scope 到 `gin.Context`
- **`content.ScopedDB(c, db)`** — 返回应用了所有注册 scope 的 `*gorm.DB`（带 Session 隔离，避免查询污染）
- **BaseTheme 动态渲染** — core 的 archive / single / taxonomy 渲染路径会通过 `content.ScopedDB(c, db)` 和 `FindBySlugScoped(c, ...)` 读取内容，所以配置驱动路由天然支持多语言 scope
- **`PageService.ForRequest(c)`** — 主题的自定义 PageService 现在嵌入 `coreTheme.BasePageService`，`ForRequest(c)` 返回带请求级过滤的克隆，并把 `*gin.Context` 存到继承来的 `ReqCtx` 字段；详情页 `Get*Detail(slug)` 用 `s.Content.FindBySlugScoped(s.ReqCtx, ...)` 读取，就复用了同一套 scope —— 否则绕过 scope 会导致 WPML 同 slug 场景下错取默认语言行

## 关键属性

- **主题零感知** — 主题只调 core API，不知道有哪些插件。如果没有任何 scope 注册，`ScopedDB` 原样返回 DB，零开销
- **可扩展** — 任何需要请求级内容过滤的功能（多语言、RBAC 内容可见性、草稿预览等）都走同一通道
- **后台列表也走 scope** — `admin.Service.ListContentScoped(c, ...)` 用同一 API，所以插件只需一次注册（基于 `?lang=` 等 query 参数），前台列表和后台列表同时生效

## 行为契约

- Scope 只在当前请求内有效，不得泄露到其它请求。
- Core 仓储保持通用，不包含特定插件条件。
- 插件只能通过公开 API 注入 scope。
- 自定义主题服务需要把当前请求传给 `ForRequest(c)`；BaseTheme 动态路由已
  自动完成这一步。
- 没有注册任何 scope 时，`ScopedDB` 保持原查询行为。
- Scope 的提供者和消费者只通过 core 交互，双方都不需要对具体插件或主题
  编写特判。

## 使用示例

插件侧（注入 scope）：

```go
e.Hooks.AddAction("middleware.early", func(ctx context.Context, args ...interface{}) {
    r := args[0].(*gin.Engine)
    r.Use(func(c *gin.Context) {
        // 通过 core API 注册过滤条件
        content.AddContentScope(c, func(db *gorm.DB) *gorm.DB {
            return db.Where("visible = ?", true)
        })
        c.Next()
    })
}, 5)
```

主题侧（消费 scope，自定义 PageService 才需要；走 BaseTheme 动态渲染时 core 已经处理）：

下面以主题声明的 `product` 内容类型为例。`product` 不是 core 内置类型，只是演示 PageService 如何消费 scope。

```go
func (h *Handler) ProductsList(c *gin.Context) {
    svc := h.pageService.ForRequest(c)  // 拿到带 scope 的 PageService 克隆
    data, _ := svc.GetProductsData()    // 内部使用 ScopedDB(c, db)，自动过滤
    c.HTML(http.StatusOK, "products", data)
}
```

主题不需要知道 multilang 插件存在，也不需要写"如果是多语言模式则……"的分支。对于新主题，优先使用 BaseTheme 的配置驱动动态路由；只有确实需要自定义服务层时才维护 `PageService.ForRequest(c)`。**core 是唯一交汇点**。
