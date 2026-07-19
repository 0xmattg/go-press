# 插件系统总览

GoPress 的插件系统借鉴 WordPress 的 hook 模型，但用 Go 接口实现，编译期类型安全。所有插件都通过 `Activate` / `Deactivate` 生命周期注册和摘除 hook，支持运行时**完全热拔插**——无需重启服务即可即时生效。

## 核心特性

- **Plugin 接口** — 实现 `Name()` / `Activate()` / `Deactivate()` 即可
- **Hook 驱动** — 通过 `middleware.early` / `routes.register` / `engine.init` 等生命周期钩子注入功能，也可通过 `theme.head.end` / `theme.body.open` / `theme.footer.end` / `header.nav.after` 等模板插槽向前台主题贡献局部 HTML
- **Hook Remove API（热插拔基石）** — `Bus.AddAction` / `AddFilter` 返回 `hook.Handle`，`RemoveAction(Handle)` / `RemoveFilter(Handle)` 按 id 精准摘除；`SitemapGenerator.AddTransformer` 对称返回 `TransformerHandle` + `RemoveTransformer()`。插件在 `Deactivate` 中统一回收所有 handle，运行时即可完整下线，无需重启服务
- **插件状态持久化** — 启用状态写入 `plugin_active_<name>` option：`"true"` 启用 / `"false"` 停用 / 缺失 = 首次加载默认启用。`LoadPlugin` 启动时按此决定是否调用 `Activate`，所以用户停用某插件后重启服务，它仍保持停用
- **启停缓存刷新** — 后台激活/停用插件后 core 统一 `Cache.Flush()`，避免页面缓存保留旧插件输出（如导航语言切换器），保证运行时热切换即时体现在前台
- **Router 热重建** — 后台启用或停用插件后，core 会重建 Gin Router，重新应用当前有效的 `middleware.early` 和 `routes.register` 注册；中间件仍建议保留运行态自守卫，覆盖关停并发窗口
- **Sitemap Transformer Hook** — 插件可通过 `engine.Sitemap.AddTransformer()` 拦截每条 URL 条目，追加 hreflang 备选链接或衍生多语言副本（多语言插件据此实现 sitemap 翻译组）
- **表注册** — 插件通过 `core.RegisterPluginTable()` 注册自定义表，引擎统一追踪生命周期
- **Settings 接口** — `SettingsProvider` 提供设置页模板，`SettingsDataProvider` 注入自定义数据，`SettingsSaveProvider` 监听设置保存事件

## 扩展接口速查

Core 在 `core/admin/content_tabs.go` 和 `core/hook/constants.go` 集中暴露常用扩展点：

| Hook | 用途 |
|---|---|
| `admin.HookContentListTabs` | 内容列表上方过滤 Tab（数据结构 + filter 名） |
| `admin.HookContentPermalinkPrefix` | 内容编辑页永久链接前缀注入 |
| `hook.AdminContentFormFields` | 内容编辑/创建页的 meta box 插槽 |
| `hook.AdminContentSaved` | 内容行保存后的 action hook |
| `hook.SEOContentMeta` | 内容详情页 `SEOMeta` 渲染前的 filter |
| `hook.ThemeHeadEnd` | `</head>` 前 HTML 插槽 |
| `hook.ThemeBodyOpen` | `<body>` 后立即 HTML 插槽 |
| `hook.ThemeFooterEnd` | `</body>` 前 HTML 插槽 |

详见 [Hook 系统](../architecture/hooks.md) 和 [后台扩展点](../admin/extension-points.md)。

## 内置插件

| 插件 | 功能 |
|---|---|
| **multilang (WPML-like)** | 完整的内容翻译系统 + 支持完整热拔插（停用后主题语言切换器/admin 语言 Tab/sitemap hreflang/菜单翻译全部实时消失），详见 [多语言插件](multilang.md) |
| **seo-extras (Yoast-like)** | 给每条内容加 4 个独立 SEO 覆盖字段（`_seo_title` / `_seo_description` / `_seo_image` / `_seo_robots`），激活后内容编辑页底部出现可折叠的「SEO 设置（可选）」面板。零核心改动、零插件表。详见 [SEO Extras 插件](seo-extras.md) |
| **code-snippets (WPCode-like)** | 通过 `theme.head.end` / `theme.body.open` / `theme.footer.end` 三个主题插槽注入站点级 HTML/JS，适合 Analytics、GTM、站点验证、客服 widget。详见 [Code Snippets 插件](code-snippets.md) |
| **gopress-analytics** | GoPress 官方自托管访问统计，异步采集 PV、UV、新访客、趋势和热门页面，数据存储在插件自有表。详见 [GoPress Analytics](gopress-analytics.md) |
| **google-identity** | 通过 core 公共认证契约实现 Google OIDC 登录和注册。详见 [前台用户注册与身份登录](../architecture/public-authentication.md#google-identity-插件) |
| **metamask-identity** | 通过 EIP-4361 SIWE、服务端一次性 Challenge 和 core 公共认证契约实现 MetaMask 钱包登录与注册。详见 [前台用户注册与身份登录](../architecture/public-authentication.md#metamask-identity-插件) |

## 通用 Pattern

每个扩展点都遵循：

- **Core 只定义** 数据结构 + hook 名 + 触发点
- **插件按需注入实现**，单语言/无插件场景下行为零差异
- **主题/插件之间不直接交互**，core 是唯一交汇点
- **Deactivate 干净下线**，按 handle 摘除全部 hook

## 下一步

- [创建插件](creating-plugins.md) — 完整 step-by-step 教程
- [多语言插件](multilang.md) — WPML 风格的完整参考
- [SEO Extras 插件](seo-extras.md) — Yoast 风格 per-content SEO 覆盖
- [Code Snippets 插件](code-snippets.md) — WPCode 风格站点级代码注入
- [GoPress Analytics](gopress-analytics.md) — 官方自托管访问统计
- [Google Identity](../architecture/public-authentication.md#google-identity-插件) — Google OIDC 与通用身份 Provider 接入
- [MetaMask Identity](../architecture/public-authentication.md#metamask-identity-插件) — EIP-4361 SIWE 钱包登录与注册
