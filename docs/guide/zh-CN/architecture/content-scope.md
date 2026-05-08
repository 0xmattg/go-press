# Content Scope API

GoPress 引擎提供了核心级的请求上下文内容过滤机制，实现**插件与主题的完全解耦**。

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
                   │  Theme PageService       │
                   │  content.ScopedDB(c, db) │  ← 读取 scope（core API）
                   └─────────────────────────┘
```

## 核心 API

- **`content.AddContentScope(c, fn)`** — 插件在中间件中注册 GORM scope 到 `gin.Context`
- **`content.ScopedDB(c, db)`** — 返回应用了所有注册 scope 的 `*gorm.DB`（带 Session 隔离，避免查询污染）
- **`PageService.ForRequest(c)`** — 主题标准模式，返回带请求级过滤的 PageService 克隆；克隆同时把 `*gin.Context` 存到 `reqCtx` 字段上，让详情页 `Get*Detail(slug)` 能调 `contentRepo.FindBySlugScoped(s.reqCtx, ...)` —— 否则主题自己的 contentRepo 会绕过 scope，导致 WPML 同 slug 场景下错取默认语言行

## 关键属性

- **主题零感知** — 主题只调 core API，不知道有哪些插件。如果没有任何 scope 注册，`ScopedDB` 原样返回 DB，零开销
- **可扩展** — 任何需要请求级内容过滤的功能（多语言、RBAC 内容可见性、草稿预览等）都走同一通道
- **后台列表也走 scope** — `admin.Service.ListContentScoped(c, ...)` 用同一 API，所以插件只需一次注册（基于 `?lang=` 等 query 参数），前台列表和后台列表同时生效

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

主题侧（消费 scope）：

下面以主题声明的 `product` 内容类型为例。`product` 不是 core 内置类型，只是演示 PageService 如何消费 scope。

```go
func (h *Handler) ProductsList(c *gin.Context) {
    svc := h.pageService.ForRequest(c)  // 拿到带 scope 的 PageService 克隆
    data, _ := svc.GetProductsData()    // 内部使用 ScopedDB(c, db)，自动过滤
    c.HTML(http.StatusOK, "products", data)
}
```

主题不需要知道 multilang 插件存在，也不需要写"如果是多语言模式则……"的分支。**core 是唯一交汇点**。
