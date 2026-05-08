# 主题系统总览

GoPress 的主题系统借鉴 WordPress 设计：主题是一个 Go 包，通过 `init()` 自注册到核心引擎，BaseTheme 提供运行时引擎能力（路由、模板层级、SEO 注入等）。

## 核心特性

- **BaseTheme 运行时引擎** — 主题嵌入 `BaseTheme` 即可获得 URL 解析、模板层级、SEO 注入等运行时能力
- **核心类型保护** — 引擎在 `Registry.Clear()` 后自动 `registerCoreTypes()`，`post` / `contact_message` / `category` / `tag` 跨主题切换永久保留
- **配置化内容类型** — 主题自定义内容类型由 `theme.toml` 的 `[[content_types]]` 声明；后台导航、CRUD、REST API、Rewrite 和菜单图标都从注册表读取
- **内置回退模板** — 当主题未提供对应模板时，BaseTheme 自动使用内置的分类归档、单页、列表回退模板，避免 404
- **详情页标签展示** — 任意挂载 `tag` 分类法的内容详情页都可以显示关联 Tags，链接到对应分类归档页
- **Theme 接口** — 实现 `Name()` / `Setup()` / `ServeHTTP()` / `TemplateFuncs()` 即可
- **App 接口** — 主题通过 `theme.App` 接口访问 DB、ContentRepo、RewriteEngine、SEOBuilder、MediaRepo、HookBus 等引擎能力
- **模板层级回退** — 类 WordPress 的模板查找：`single-{type}-{slug}.tmpl` → `single-{type}.tmpl` → `single.tmpl` → `index.tmpl`
- **统一模板函数（Single-Source FuncMap）** — `CommonFuncMap()` + BaseTheme 的引擎感知 helpers（`buildURL`、`seoHead`、`menuByLocation`、`T`、`currentLang`、`langPrefixURL`、`renderHook`、`isMenuActive`、`responsiveImage`、`responsiveImagePriority`、`responsiveImagePreload`）通过 `BaseFuncMap()` 统一下发。**所有主题、所有模板加载路径共享同一份 funcmap**
- **前台模板 Hook 插槽** — 主题在语义位置声明 `{{renderHook "theme.head.end" .}}` / `{{renderHook "theme.body.open" .}}` / `{{renderHook "theme.footer.end" .}}` / `{{renderHook "header.nav.after" .}}` 等标准插槽，插件注册同名 filter 输出 HTML
- **LoadPageBundle 核心级页面模板编译器** — `core/theme/page_bundle.go` 提供 `LoadPageBundle(theme, pages)`：自动发现 `layouts/base.tmpl` + `partials/*.tmpl`，对每个页面独立编译（允许不同页面重新定义同名 block）
- **自定义路由 + 动态路由** — 静态页面（`/about`）通过 `AddRoute()` 注册，动态 URL（例如主题声明的 `product` 对应 `/products/:slug`）由 Rewrite 引擎按当前内容类型配置自动解析
- **SEO 自动注入** — 每个页面模板自动获得 `SEO` 数据（title、OG、JSON-LD），详见 [SEO 接入规范](seo-integration.md)
- **热切换** — 后台一键切换主题，自动重建路由 + 刷新缓存
- **DemoDataProvider** — 主题可实现 `DemoSeedPath()` 接口，后台一键导入演示内容和图片
- **init() 自注册** — 主题通过 `init()` 函数自动注册到引擎
- **零主题/插件交叉耦合** — 主题只依赖 core funcmap 的字符串 key（`{{T .Ctx "x"}}`、`{{langPrefixURL .Ctx "/blog"}}`、`{{renderHook "theme.head.end" .}}`），插件只向 core 注册 hook/ctx key。**主题和插件之间不存在任何直接调用或类型依赖**

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

后续新主题推荐走 [BaseTheme + gin.H 路径](seo-integration.md#推荐写法basetheme--ginh)，避免重复 `PageService` 这套代码。

## 下一步

- [创建主题](creating-themes.md) — 完整 step-by-step 教程
- [SEO 接入规范](seo-integration.md) — `<title>` / `<meta description>` / `seoHeadFor` 契约
- [图片接入规范](image-pipeline.md) — `responsiveImage` 等 helper 用法
- [媒体变体管线](media-variants.md) — 上传后自动生成的 thumb / 480w / WebP 等变体
