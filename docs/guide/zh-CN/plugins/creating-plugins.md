# 创建插件

## 最小可用插件

```go
// plugins/my-plugin/plugin.go
package myplugin

import (
    "context"

    "github.com/gin-gonic/gin"

    "go-press/core"
    "go-press/core/hook"
    "go-press/core/plugin"
)

type MyPlugin struct {
    engine      *core.Engine
    hookHandles []hook.Handle  // 用于 Deactivate 时干净摘除
}

func New() *MyPlugin { return &MyPlugin{} }

func (p *MyPlugin) Name() string        { return "my-plugin" }
func (p *MyPlugin) Version() string     { return "1.0.0" }
func (p *MyPlugin) Description() string { return "My custom plugin" }

func (p *MyPlugin) Activate(app plugin.App) {
    e := app.(*core.Engine)
    p.engine = e
    p.hookHandles = p.hookHandles[:0]

    // 注册插件自定义表（可选）
    core.RegisterPluginTable("my-plugin", "records")

    // 通过 Hook 注入功能
    p.hookHandles = append(p.hookHandles,
        e.Hooks.AddAction("routes.register", func(_ context.Context, args ...interface{}) {
            r := args[0].(*gin.Engine)
            r.GET("/my-endpoint", myHandler)
        }, 10),
    )
}

func (p *MyPlugin) Deactivate(_ plugin.App) {
    for _, h := range p.hookHandles {
        p.engine.Hooks.RemoveAction(h)
        p.engine.Hooks.RemoveFilter(h)
    }
    p.hookHandles = p.hookHandles[:0]
}
```

```go
// plugins/my-plugin/register.go
package myplugin

import "go-press/core"

func init() {
    core.RegisterPlugin("my-plugin", func(engine *core.Engine) {
        engine.LoadPlugin(New())
    })
}
```

最后在 `cmd/server/main.go` 加 blank import：

```go
_ "go-press/plugins/my-plugin"
```

## Plugin 接口可选扩展

```go
// SettingsProvider — 在后台插件管理中显示设置页面
func (p *MyPlugin) SettingsTemplatePath() string {
    return "plugins/my-plugin/templates/admin/settings.tmpl"
}

// SettingsDataProvider — 向设置页模板注入自定义数据
func (p *MyPlugin) SettingsData() map[string]interface{} {
    return map[string]interface{}{"MyItems": items}
}

// SettingsSaveProvider — 在设置保存后执行自定义逻辑
func (p *MyPlugin) OnSettingsSave(settings map[string]string) {
    // 同步设置到插件自有表...
}
```

## 注册请求级内容过滤（Content Scope API）

如果你的插件需要让前后台内容查询自动按某条件过滤（多语言、可见性、草稿预览等）：

```go
// 在 middleware.early hook 中注册请求级内容过滤
e.Hooks.AddAction("middleware.early", func(_ context.Context, args ...interface{}) {
    r := args[0].(*gin.Engine)
    r.Use(func(c *gin.Context) {
        // 通过 core API 注册过滤条件
        content.AddContentScope(c, func(db *gorm.DB) *gorm.DB {
            return db.Where("visible = ?", true)
        })
        c.Next()
    })
}, 5)
// 主题自动获得过滤后的查询结果，无需任何适配代码
```

详见 [Content Scope API](../architecture/content-scope.md)。

## 热拔插要点

GoPress 支持插件运行时完全热拔插。要做到这一点，插件实现必须遵守：

1. **`AddAction` / `AddFilter` 返回的 `Handle` 必须保存** — 插件结构体里维护一个 `hookHandles []hook.Handle`，每次注册都 append 进去
2. **`Deactivate` 中按 handle 摘除全部** — 调 `RemoveAction` + `RemoveFilter`（不知道是 action 还是 filter 时两个都调，方法对零值或不匹配的 handle 是 no-op）
3. **Gin 中间件不能从 router 摘除** — 插件注册的 `gin.HandlerFunc` 必须在函数入口自检 `engine.PluginManager.IsActive(name)`，停用即短路 `c.Next()`
4. **Sitemap transformer / 其他对称 Add/Remove API** — 同样保存 handle，对称摘除

参考 [multilang 插件](multilang.md) 是完整的热拔插实现样板。

## 内置 Hook 速查

详细列表见 [Hook 系统](../architecture/hooks.md)。常用：

| Hook | 类型 | 用途 |
|---|---|---|
| `engine.init` | action | Bootstrap 完成后 |
| `middleware.early` | action | 注册中间件（页面缓存之前） |
| `routes.register` | action | 注册路由（admin 之后、catch-all 之前） |
| `options.bulk_updated` | action | 批量保存设置后失效缓存 |
| `theme.head.end` | filter | `</head>` 前 HTML 插槽 |
| `theme.body.open` | filter | `<body>` 后立即 HTML 插槽 |
| `theme.footer.end` | filter | `</body>` 前 HTML 插槽 |
| `header.nav.after` | filter | 主题导航尾部 HTML 插槽 |
| `menu.location.resolve` | filter | 菜单按位置返回前的最终 transform |
| `admin.content_form.fields` | filter | 内容编辑页 meta box 插槽 |
| `admin.content.saved` | action | 内容保存后副作用 |
| `seo.content.meta` | filter | 单页内容 SEOMeta 渲染前 |
