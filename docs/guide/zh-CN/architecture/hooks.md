# Hook 系统

GoPress 的 Hook 系统是 WordPress `do_action` / `apply_filters` 的 Go 等价物，是插件 / 主题与核心交互的主要通道。

## API

```go
// Action — 仅副作用，无返回值
e.Hooks.AddAction(name, fn, priority) hook.Handle
e.Hooks.RemoveAction(handle)
e.Hooks.DoAction(ctx, name, args...)

// Filter — 链式修改一个值
e.Hooks.AddFilter(name, fn, priority) hook.Handle
e.Hooks.RemoveFilter(handle)
e.Hooks.ApplyFilter(name, value, args...) interface{}
```

`priority` 数值越小越先执行。`AddAction` / `AddFilter` 返回的 `hook.Handle` 是热拔插的关键——插件 `Deactivate` 时按 handle 摘除，运行时即可完整下线。

## 引擎生命周期 Hook

| Hook 名称 | 触发时机 | 参数 |
|-----------|---------|------|
| `engine.init` | Bootstrap 完成后 | `*core.Engine` |
| `middleware.early` | 页面缓存中间件之前 | `*gin.Engine` |
| `routes.register` | Admin 路由注册后、catch-all 之前 | `*gin.Engine` |
| `options.bulk_updated` | admin 批量保存设置后 | 无参 |

## 主题模板插槽

插件通过模板插槽向前台主题贡献局部 HTML，主题在语义位置声明插槽即可。

| Hook 名称 | Go 常量 | 用途 |
|---|---|---|
| `header.nav.after` | `hook.ThemeHeaderNavAfter` | 导航列表尾部插槽（多语言切换器、用户菜单等） |
| `theme.head.end` | `hook.ThemeHeadEnd` | `</head>` 前插槽（站点验证 meta、analytics 脚本、preconnect、第三方 CSS 等） |
| `theme.body.open` | `hook.ThemeBodyOpen` | `<body>` 后立即插槽（GTM noscript、A/B 测试 bootstrap、全站公告条等） |
| `theme.footer.end` | `hook.ThemeFooterEnd` | `</body>` 前插槽（延迟加载脚本、客服 widget、热力图、追踪脚本等） |

导航插槽声明语法：

```gotemplate
<ul class="nav-menu">
    <li><a href="{{langPrefixURL .Ctx "/"}}">{{T .Ctx "nav_home"}}</a></li>
    <li><a href="{{langPrefixURL .Ctx "/about"}}">{{T .Ctx "nav_about"}}</a></li>
    {{renderHook "header.nav.after" .}}
</ul>
```

全局页面插槽应放在主题的基础布局里，并且每个位置只声明一次：

```gotemplate
<head>
    ...
    {{renderHook "theme.head.end" .}}
</head>
<body>
    {{renderHook "theme.body.open" .}}
    ...
    {{renderHook "theme.footer.end" .}}
</body>
```

详见 [主题前台扩展插槽](../themes/overview.md)。

## 菜单 Hook

| Hook 名称 | Go 常量 | 用途 |
|---|---|---|
| `menu.location.resolve` | `hook.MenuLocationResolve` | filter，菜单位置解析后、返回主题前。filter value: `*menu.Menu`；args: `location string` |
| `menu.deleted` | `hook.MenuDeleted` | action，菜单删除后。args: `menuID uint` |

多语言插件用这两个 hook 实现透明的语言菜单切换，core/menu 不知道多语言存在。

## Admin 扩展 Hook

集中暴露在 `core/admin/content_tabs.go` 和 `core/hook/constants.go`：

| Hook 名称 | Go 常量 | 用途 |
|---|---|---|
| `admin.content_list.tabs` | `admin.HookContentListTabs` | filter，向内容列表上方注入过滤 Tab。filter value: `[]admin.ContentListTab`；args: `*gin.Context, typeName string` |
| `admin.content.permalink_prefix` | `admin.HookContentPermalinkPrefix` | filter，内容编辑页永久链接前缀注入。filter value: `string`；args: `*gin.Context, *content.Content` |
| `admin.content_form.fields` | `hook.AdminContentFormFields` | filter，内容编辑/创建页的 meta box 插槽。filter value: `template.HTML`；args: `*content.Content, *content.ContentTypeDef`。模板中由 `{{renderHook "admin.content_form.fields" .Item .TypeDef}}` 渲染 |
| `admin.content.saved` | `hook.AdminContentSaved` | action，内容行保存后。args: `*gin.Context, *content.Content`。插件按自己的 form key 持久化到 `gp_content_meta` |

详见 [后台扩展点](../admin/extension-points.md)。

## SEO Hook

| Hook 名称 | Go 常量 | 用途 |
|---|---|---|
| `seo.content.meta` | `hook.SEOContentMeta` | filter，单页内容 SEOMeta 渲染前。filter value: `rewrite.SEOMeta`；args: `*content.Content, map[string]string contentMeta`。插件按 meta 值覆盖 SEO 字段 |

参考实现见 [seo-extras 插件](../plugins/seo-extras.md)。

## Sitemap Hook

```go
type TransformerHandle  // 由 SitemapGenerator.AddTransformer 返回
e.Sitemap.AddTransformer(fn) TransformerHandle
e.Sitemap.RemoveTransformer(handle)
```

插件可通过 `engine.Sitemap.AddTransformer()` 拦截每条 URL 条目，追加 hreflang 备选链接或衍生多语言副本（多语言插件据此实现 sitemap 翻译组）。

## 通用 Pattern

**Core 只定义**：数据结构 + hook 名 + 触发点  
**插件按需注入实现**：在 Activate 时 AddAction / AddFilter，记录 handle  
**单语言/无插件场景**：filter pass-through，行为零差异  
**Deactivate 干净下线**：RemoveAction / RemoveFilter 按 handle 摘除

这套 pattern 让 GoPress 的"核心 + 插件生态"可持续扩展，新功能不一定要进 core——通常一个插件 + 几个 hook 就能完成。
