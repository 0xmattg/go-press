# 后台扩展点

GoPress 在 core 侧暴露了一组通用扩展点，允许任何插件向 admin 后台注入功能（不限于多语言或 SEO）。所有扩展点都遵循相同 pattern：

1. **Core 只定义**：数据结构 + hook 名 + 触发点
2. **插件按需注入实现**：在 `Activate` 时注册 filter / action
3. **单插件 / 无插件场景**：filter pass-through、action 无副作用，行为完全不变

下面按已开放的扩展点逐一介绍。

## 内容列表过滤 Tab

允许任何插件向 admin 列表页注入过滤条：

```go
// core/admin/content_tabs.go
type ContentListTab struct {
    Key    string // "all" / "en" / "zh" ...
    Label  string // 显示文案
    Count  int    // 计数徽章
    Active bool   // 当前 Tab
    URL    string // href
}
const HookContentListTabs = "admin.content_list.tabs"

// 插件侧注册（以 multilang 为例）
e.Hooks.AddFilter(admin.HookContentListTabs,
    func(v interface{}, args ...interface{}) interface{} {
        tabs := v.([]admin.ContentListTab)
        c := args[0].(*gin.Context)
        typeName := args[1].(string)
        // append 语言 tabs...
        return tabs
    }, 10)
```

配合 [Content Scope API](../architecture/content-scope.md) 注入对应 WHERE 条件，Tab 点击 → URL `?lang=zh` → scope 过滤 → 列表只显示该语言内容。

**如果没有插件注册 filter，hook 返回空切片，Tab 条区域不渲染，列表完全恢复旧行为**。

## 内容编辑页永久链接前缀

让多语言/多站点等插件按需在 URL 前面插入 `/zh`、`/site-2` 之类的段：

```go
// core/admin/content_tabs.go
const HookContentPermalinkPrefix = "admin.content.permalink_prefix"
// filter value: string  (默认 ""，pass-through 即可)
// args: [*gin.Context, *content.Content]

// multilang 实现示例（节选）
func (p *Plugin) adminContentPermalinkPrefix(value any, args ...any) any {
    prefix, _ := value.(string)
    item := args[1].(*content.Content)
    trans, _ := p.repo.GetTranslation(item.ID)
    if trans != nil && trans.LanguageCode != "" && trans.LanguageCode != p.getDefaultLang() {
        return prefix + "/" + trans.LanguageCode  // e.g. "/zh"
    }
    return prefix
}
```

效果：以主题声明的 `product` 内容类型为例，编辑英文产品时永久链接显示 `/products/foo`，编辑中文产品（同 slug）时显示 `/zh/products/foo` —— 在 WPML 模式（同 slug 跨语言共享）下避免运营误判。**未启用插件时 prefix 默认空字符串，行为与单语言完全一致**。

## 内容编辑页 Meta Box 插槽

插件可向内容编辑/创建表单注入额外字段（"meta box"），不需要修改 core 代码：

```go
const AdminContentFormFields = "admin.content_form.fields"
// filter value: template.HTML (初始为空)
// args: [*content.Content, *content.ContentTypeDef]
// 模板中由 {{renderHook "admin.content_form.fields" .Item .TypeDef}} 渲染
```

参考实现见 [seo-extras 插件](../plugins/seo-extras.md)：激活后内容编辑页底部出现折叠的「SEO 设置（可选）」面板，4 个独立字段（_seo_title / _seo_description / _seo_image / _seo_robots），全部可选，留空走默认。

## 内容保存后 Action Hook

插件用来持久化自己的 meta box 字段：

```go
const AdminContentSaved = "admin.content.saved"
// args: [*gin.Context, *content.Content]

e.Hooks.AddAction(hook.AdminContentSaved, func(_ context.Context, args ...interface{}) {
    c := args[0].(*gin.Context)
    item := args[1].(*content.Content)
    // 用 c.PostForm("my_key") 读自己的字段，存到 gp_content_meta
    engine.Content.SaveMeta(item.ID, "my_key", c.PostForm("my_key"))
}, 50)
```

`ContentCreate` 和 `ContentUpdate` 都会触发此 action，所以创建和编辑场景一致。

## 前台模板插槽

虽然不属于"后台"，但同样是 core 暴露的扩展点：

| Hook | 用途 |
|---|---|
| `theme.head.end` | `</head>` 前注入站点级 `<meta>` / `<script>` / `<link>` |
| `theme.body.open` | `<body>` 后立即注入 GTM noscript、公告条等 |
| `theme.footer.end` | `</body>` 前注入延迟脚本、客服 widget、热力图等 |
| `header.nav.after` | 导航列表尾部注入多语言切换器、用户菜单等 |

主题在导航尾部声明：

```gotemplate
{{renderHook "header.nav.after" .}}
```

主题在基础布局中声明全局页面插槽：

```gotemplate
{{renderHook "theme.head.end" .}}
{{renderHook "theme.body.open" .}}
{{renderHook "theme.footer.end" .}}
```

插件注册同名 filter 输出 HTML。多语言插件消费 `header.nav.after`，`code-snippets` 插件消费三个 `theme.*` 插槽。详见 [主题前台扩展插槽](../themes/overview.md) 和 [Hook 系统](../architecture/hooks.md)。

## 设计原则回顾

- 主题/插件之间**不直接交互**，core 是唯一交汇点
- 每个扩展点都有"无插件时的默认行为"——core 自身完整可用，插件只是增强
- Hook 都返回 `Handle`，插件 `Deactivate` 时按 handle 摘除，运行时即可完整下线，无需重启
