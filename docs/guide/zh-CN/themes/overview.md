# 主题系统总览

GoPress 的主题系统借鉴 WordPress 设计：主题是一个 Go 包，通过 `init()` 自注册到核心引擎，BaseTheme 提供运行时引擎能力（路由、模板层级、SEO 注入等）。

## 核心特性

- **BaseTheme 运行时引擎** — 主题嵌入 `BaseTheme` 即可获得 URL 解析、模板层级、SEO 注入等运行时能力
- **核心类型保护** — 引擎在 `Registry.Clear()` 后自动 `registerCoreTypes()`，`post` / `contact_message` / `category` / `tag` 跨主题切换永久保留
- **配置化内容类型** — 主题自定义内容类型由 `theme.toml` 的 `[[content_types]]` 声明；后台导航、CRUD、REST API、Rewrite、模板映射和菜单图标都从注册表读取
- **内置回退模板** — 当主题未提供对应模板时，BaseTheme 自动使用内置的分类归档、单页、列表回退模板，避免 404
- **详情页标签展示** — 任意挂载 `tag` 分类法的内容详情页都可以显示关联 Tags，链接到对应分类归档页
- **Theme 接口** — 实现 `Name()` / `Setup()` / `ServeHTTP()` / `TemplateFuncs()` 即可
- **App 接口** — 主题通过 `theme.App` 接口访问 DB、ContentRepo、RewriteEngine、SEOBuilder、MediaRepo、HookBus、SiteLocation 等引擎能力
- **模板层级回退** — 类 WordPress 的模板查找：`single-{type}-{slug}.tmpl` → `single-{type}.tmpl` → `single.tmpl` → `index.tmpl`
- **统一模板函数（Single-Source FuncMap）** — `CommonFuncMap()` + BaseTheme 的引擎感知 helpers（`buildURL`、`archiveURL`、`contentURL`、`pageTitleFor`、`seoHeadFor`、`seoHead`、`menuByLocation`、`isMenuURLActive`、`formatDate`、`formatDateTime`、`T`、`currentLang`、`langPrefixURL`、`renderHook`、`responsiveImage`、`responsiveImagePriority`、`responsiveImagePreload`）通过 `BaseFuncMap()` 统一下发。**所有主题、所有模板加载路径共享同一份 funcmap**。`formatDate` / `formatDateTime` 会按 `site_timezone` 展示内容时间；`isMenuActive` 仅保留给旧主题兼容，新主题应使用请求感知的 `isMenuURLActive`
- **前台模板 Hook 插槽** — 主题在语义位置声明 `{{renderHook "theme.head.end" .}}` / `{{renderHook "theme.body.open" .}}` / `{{renderHook "theme.footer.end" .}}` / `{{renderHook "header.nav.after" .}}` 等标准插槽，插件注册同名 filter 输出 HTML
- **LoadPageBundle 核心级页面模板编译器** — `core/theme/page_bundle.go` 提供 `LoadPageBundle(theme, pages)` 和 `LoadAllPageBundles(theme)`：自动发现 `layouts/base.tmpl` + `partials/*.tmpl` + `pages/*.tmpl`，对每个页面独立编译（允许不同页面重新定义同名 block）
- **自定义路由 + 动态路由** — 静态页面（`/about`）通过 `AddRoute()` 注册，动态 URL（例如主题声明的 `product` 对应 `/products/:slug`）由 Rewrite 引擎按当前内容类型配置自动解析；`product` / `service` / `showcase` 只是常见示例，不是 core 固定模型
- **SEO 自动注入** — 每个页面模板自动获得 `SEO` 数据（title、OG、JSON-LD），详见 [SEO 接入规范](seo-integration.md)
- **热切换** — 后台一键切换主题，自动重建路由 + 刷新缓存
- **DemoDataProvider** — 主题可实现 `DemoSeedPath()` 接口，后台一键导入演示内容和图片
- **init() 自注册** — 主题通过 `init()` 函数自动注册到引擎
- **零主题/插件交叉耦合** — 主题只依赖 core funcmap 的字符串 key（`{{T .Ctx "x"}}`、`{{langPrefixURL .Ctx "/blog"}}`、`{{renderHook "theme.head.end" .}}`），插件只向 core 注册 hook/ctx key。**主题和插件之间不存在任何直接调用或类型依赖**

## 配置驱动内容路由

core 不假设一个站点一定有 `product`、`service` 或 `showcase`。除核心保留的 `post` / `contact_message` 等类型外，主题需要的业务内容类型都由 `theme.toml` 声明。

每个内容类型的 `rewrite_slug` 决定前台归档和详情 URL；当内容类型名、URL 和视觉模板名不一致时，可以通过 `templates` 显式指定复用哪个页面模板：

```toml
[[content_types]]
name = "module"
label = "模块"
label_plural = "核心模块"
archive_title_key = "page_title_module"
has_archive = true
rewrite_slug = "modules"
templates = { archive = "products", single = "product-detail" }
```

这个配置会让 `/modules` 和 `/modules/{slug}` 解析到 `module` 内容类型，同时复用 `templates/pages/products.tmpl` 和 `templates/pages/product-detail.tmpl`。如果不写 `templates`，BaseTheme 会按内容类型名和 `rewrite_slug` 推导候选模板，再回退到通用 archive/single 模板和内置 fallback。

模板里的内容链接不要硬写 `/products` / `/services` 这类路径，应使用 core helper：

```gotemplate
<a href="{{archiveURL "module"}}">Modules</a>
<a href="{{contentURL . "module"}}">{{.Title}}</a>
```

这样后续只改 `theme.toml` 的 `rewrite_slug` 或模板映射时，菜单、页面内链、SEO canonical 和 sitemap 都能跟随注册表保持一致。

导航当前页状态也应由 core helper 判断，不要把主题配置里的内容类型名、菜单标题或 `.ActivePage` 字符串写死在模板中：

```gotemplate
{{with menuByLocation "header"}}
    {{range .Items}}
        <a href="{{.URL}}" class="{{if isMenuURLActive $.Ctx .URL}}active{{end}}">{{.Title}}</a>
    {{end}}
{{end}}
```

`isMenuURLActive` 以当前请求 URL 和菜单项 URL 为准，能跟随 `rewrite_slug`、语言前缀和详情页路径变化。

## 前台扩展插槽

GoPress 在 core/theme 层提供模板级 hook 函数，解决"插件想在主题固定语义位置注入局部 HTML，但又不应该扫描和修改整页 HTML"的问题。

插件友好的主题至少应在基础布局里声明三个全局页面插槽：

```gotemplate
<head>
    ...
    {{renderHook "theme.head.end" .}}
</head>
<body>
    {{renderHook "theme.body.open" .}}
    {{template "header" .}}
    <main>{{template "content" .}}</main>
    {{template "footer" .}}
    <script src="/static/js/main.js"></script>
    {{renderHook "theme.footer.end" .}}
</body>
```

这三个插槽是站点级代码注入插件的契约：

| 插槽 | 位置 | 常见用途 |
|---|---|---|
| `theme.head.end` | `</head>` 前 | Analytics 主脚本、验证 meta、preconnect、第三方 CSS |
| `theme.body.open` | `<body>` 后立即 | GTM noscript、A/B 测试 bootstrap、全站公告条 |
| `theme.footer.end` | `</body>` 前 | 客服 widget、热力图、延迟加载追踪脚本 |

标准导航尾部插槽为 `header.nav.after`，Go 代码侧常量为 `hook.ThemeHeaderNavAfter`：

```gotemplate
<ul class="nav-menu">
    <li><a href="{{langPrefixURL .Ctx "/"}}">{{T .Ctx "nav_home"}}</a></li>
    <li><a href="{{langPrefixURL .Ctx "/about"}}">{{T .Ctx "nav_about"}}</a></li>
    {{renderHook "header.nav.after" .}}
</ul>
```

插件侧注册同名 filter：

```go
handle := e.Hooks.AddFilter(hook.ThemeHeaderNavAfter,
    func(value interface{}, args ...interface{}) interface{} {
        data := args[0] // 通常是当前页面模板数据，包含 Ctx
        return template.HTML(fmt.Sprint(value)) + renderMyNavItem(data)
    }, 10)

// Deactivate 时必须移除，保证热禁用即时生效
e.Hooks.RemoveFilter(handle)
```

约定：

- 主题负责声明插槽位置和周围语义结构，例如导航列表里的 `<ul>`
- 插件负责输出与该插槽语义匹配的片段，例如 `header.nav.after` 输出 `<li>...</li>`
- 全局页面插槽应只在 `layouts/base.tmpl` 这类基础布局里声明一次，避免插件输出在局部模板中重复出现
- 插件禁用时通过 `RemoveFilter(handle)` 摘除，core 在插件启停后统一刷新缓存，前台不需要重启
- 禁止插件通过响应缓冲扫描 `id="nav-menu"`、字符串替换 `</ul>`、或兜底生成右上角浮层。这类做法属于插件后处理页面 HTML，会把插件和主题结构重新耦合

## 内置主题

| Slug | 主题名称 | 类型 | 渲染路径 | 状态 |
|------|----------|------|---------|------|
| **modern-company** | Modern Company | 企业官网 | 自定义 PageData struct | 完整 |
| **financial-news** | Financial News | 财经新闻门户 | 自定义 PageData struct | 完整 |
| **atelier-slate** | Atelier Slate | 数字工作室 | BaseTheme + gin.H | 完整 |
| **civic-estate** | Civic Estate | 商业地产 | BaseTheme + gin.H | 完整 |
| **florafi** | FloraFi | 稳定币 / 金融科技产品官网 | BaseTheme + gin.H | 完整 |
| **terra-trail** | Terra Trail | 户外旅行 | BaseTheme + gin.H | 完整 |
| **axis-form** | Axis Form | 建筑设计 / 室内作品集 | BaseTheme + gin.H | 完整 |
| **go-press-landing** | GoPress Landing | SaaS Landing | 自定义 PageData struct | 完整 |

上表「渲染方式」列里的「自定义 PageData struct」主题，其 `PageService` 现在都嵌入 core 的共享脚手架（`coreTheme.BasePageService`，需要 SEO 的用 `coreTheme.SEOPageService`），不再各自复制数据访问与 SEO 管道。新主题若想要类型安全的数据装配，直接嵌入这两个之一即可，成本很低；只想快速起步则走 [BaseTheme + gin.H 路径](seo-integration.md)。

## 前台账号 UI

主题可以通过 core 提供的 `currentUser`、`isLoggedIn`、`loginURL`、`logoutURL` 和 `loginProviders` helper 渲染与 Provider 无关的账号界面。主题负责决定账号入口的布局和视觉，但不能 import 或特判 Google Identity、MetaMask Identity 等插件。

主题应通过 `loginProviders` 发现当前启用的登录方式，并使用 core 发布的 Provider 登录入口。模板示例、页面缓存和安全注意事项见[前台账号与外部身份登录](../architecture/public-authentication.md#主题接入)。

## 下一步

- [创建主题](creating-themes.md) — 完整 step-by-step 教程
- [SEO 接入规范](seo-integration.md) — `<title>` / `<meta description>` / `pageTitleFor` / `seoHeadFor` 契约
- [图片接入规范](image-pipeline.md) — `responsiveImage` 等 helper 用法
- [媒体变体管线](media-variants.md) — 上传后自动生成的 thumb / 480w / WebP 等变体
