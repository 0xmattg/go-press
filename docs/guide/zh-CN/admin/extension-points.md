# 后台扩展点

GoPress 在 core 侧暴露了一组通用扩展点，允许任何插件向 admin 后台注入功能（不限于多语言或 SEO）。所有扩展点都遵循相同 pattern：

1. **Core 只定义**：数据结构 + hook 名 + 触发点
2. **插件按需注入实现**：在 `Activate` 时注册 filter / action
3. **单插件 / 无插件场景**：filter pass-through、action 无副作用，行为完全不变

下面按已开放的扩展点逐一介绍。

## Dashboard Widget 插槽

插件可通过 `admin.dashboard.widgets` 向控制面板追加可信的
`template.HTML` 组件。filter 的第一个参数是 Dashboard 模板根数据，插件
必须根据 `CurrentRole` 判断是否渲染；组件使用的后台 API 仍须复用 core
认证与独立 RBAC capability，不能只依赖前端隐藏。

`gopress-analytics` 使用该插槽展示访问趋势，其 JSON 接口要求
`analytics.read`。

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

## 邮件与通知 Hook

GoPress 的邮件能力拆成两层：`core/mail` 只负责邮件对象和 SMTP 投递；通知规则监听 core 事件并调用邮件服务。插件可以过滤邮件对象、监听发送结果，或改写默认联系留言通知的收件人/主题/正文。

| Hook | 类型 | 用途 |
|---|---|---|
| `content.created` | action | 内容创建并保存 meta 后触发。args: `*content.Content, map[string]string` |
| `mail.message` | filter | 发送前修改 `mail.Message` |
| `mail.before_send` | action | SMTP 投递前观察邮件对象 |
| `mail.sent` | action | 投递成功后触发 |
| `mail.failed` | action | 投递失败后触发。args: `mail.Message, error` |
| `notification.contact_message.recipients` | filter | 修改新联系留言通知收件人，value: `[]string` |
| `notification.contact_message.subject` | filter | 修改新联系留言通知主题，value: `string` |
| `notification.contact_message.body` | filter | 修改新联系留言通知正文，value: `string` |

示例：把联系留言通知同时发给销售邮箱：

```go
e.Hooks.AddFilter(hook.NotificationContactMessageRecipients,
    func(value interface{}, args ...interface{}) interface{} {
        recipients, _ := value.([]string)
        return append(recipients, "sales@example.com")
    }, 20)
```

插件如果需要主动发送自己的通知邮件，不要直接访问 SMTP 配置或具体 driver，而是通过 core 暴露的 `mail.Sender` 能力：

```go
sender := plugin.MailSender(app)
if sender == nil {
    return
}

err := sender.Send(ctx, mail.Message{
    To:      []string{"admin@example.com"},
    Subject: "Plugin notification",
    Text:    "Something happened.",
})
```

主题侧也可以通过 `theme.App.MailSender()` 或嵌入 `BaseTheme` 后的 `t.MailSender()` 获取同一个能力，用于主题自有表单或前台 workflow 的通知。主题仍然不应该关心 SMTP 主机、Gmail app key、`go-mail` / `stdlib` 等传输细节；更推荐的默认模式仍是：主题保存内容或触发 core hook，通知规则/插件监听后发邮件。

SMTP 配置、通知开关和投递逻辑都留在 core 或插件扩展点里。

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
