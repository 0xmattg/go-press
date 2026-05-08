# Code Snippets 插件 (WPCode-like)

`code-snippets` 是一个站点级代码注入插件，用来把可信的 HTML / JavaScript 片段插入到主题的标准前台插槽中。典型用途包括 Google Analytics、Google Tag Manager、Google Search Console / Cloudflare / 百度站长验证 meta、客服 widget、热力图和第三方追踪脚本。

## 核心能力

| 插槽 | Option key | 输出位置 | 常见用途 |
|---|---|---|---|
| `theme.head.end` | `plugin_code-snippets_head` | `</head>` 前 | Analytics 主脚本、验证 meta、preconnect、第三方 CSS |
| `theme.body.open` | `plugin_code-snippets_body_open` | `<body>` 后立即 | GTM noscript、A/B 测试 bootstrap、全站公告条 |
| `theme.footer.end` | `plugin_code-snippets_footer` | `</body>` 前 | 客服 widget、热力图、延迟加载追踪脚本 |

插件没有自定义数据库表，三个 textarea 的内容直接保存到 `gp_options`。这符合当前用途：站点级、少量、全局生效的代码片段。

## 主题接入契约

这个插件完全依赖主题模板插槽，不扫描也不后处理 HTML。主题必须在基础布局中声明以下三个 hook：

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

`theme.footer.end` 建议放在主题自己的 `<script src="/static/js/main.js">` 之后、`</body>` 之前，这样第三方延迟脚本不会抢在主题脚本之前执行。

## 启用方式

1. `cmd/server/main.go` 里通过 blank import 注册插件：

```go
_ "go-press/plugins/code-snippets"
```

2. 在后台「插件管理」启用 `code-snippets`。
3. 进入插件设置页，分别填写 `<head>` 末尾、`<body>` 开头、`</body>` 前的代码片段。
4. 保存后 core 会刷新缓存，前台页面立即使用新的片段。

## 实现要点

插件激活时注册三个 filter，并保存返回的 `hook.Handle`：

```go
p.hookHandles = append(p.hookHandles,
    e.Hooks.AddFilter(hook.ThemeHeadEnd, p.injectHead, 50),
    e.Hooks.AddFilter(hook.ThemeBodyOpen, p.injectBodyOpen, 50),
    e.Hooks.AddFilter(hook.ThemeFooterEnd, p.injectFooter, 50),
)
```

停用时逐个 `RemoveFilter(handle)`。这意味着插件停用后不会继续输出旧片段，页面缓存也会在插件启停后由 core 统一清理。

## 安全边界

片段内容会以 `template.HTML` 原样输出，不做转义。这是插件的设计目的，也是风险边界：

- 只应给可信管理员开放插件设置权限。
- 不要粘贴来源不明的脚本。
- 第三方脚本可能影响性能、隐私合规和前台交互，生产站点应先在测试环境验证。

如果需要更细粒度的按页面、按内容类型、按角色或按语言条件输出，可以在未来扩展为多条 snippet 记录，但仍应复用这三个主题插槽作为输出契约。
