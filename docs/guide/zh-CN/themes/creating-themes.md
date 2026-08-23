# 创建主题

## 最小可用主题

```go
// themes/my-theme/theme.go
package mytheme

import (
    "html/template"
    "path/filepath"

    "go-press/core"
    coreTheme "go-press/core/theme"
    "github.com/gin-gonic/gin"
)

func init() {
    core.RegisterTheme("my-theme", func(engine *core.Engine, themeDir string) coreTheme.Theme {
        return New(engine, themeDir)
    })
}

type MyTheme struct {
    coreTheme.BaseTheme              // 嵌入 BaseTheme 获得运行时引擎能力
    engine *core.Engine
}

func New(engine *core.Engine, themeDir string) *MyTheme {
    t := &MyTheme{engine: engine}
    t.InitBase(engine, themeDir, nil) // 初始化 BaseTheme

    // 注册自定义静态页面路由（可选）
    t.AddRoute("GET", "/about", myAboutHandler)

    // 加载模板（支持层级回退）
    t.LoadTemplates(t)
    return t
}

// Name / Version / Description / Author 由内嵌 BaseTheme 从 theme.toml 解析，
// 不要在 Go 中再手写一份。

// Setup 只放主题运行时初始化，例如菜单位置、可翻译设置键、自定义 hook。
// 内容类型由 theme.toml 的 [[content_types]] 声明，core 在激活主题时自动注册。
func (t *MyTheme) Setup(app coreTheme.App) {}

// ServeHTTP 委托给 BaseTheme 处理
// BaseTheme 自动处理：自定义路由 → Rewrite 引擎解析 → 模板层级 → SEO 注入
func (t *MyTheme) ServeHTTP(c *gin.Context) { t.BaseTheme.ServeHTTP(c) }

func (t *MyTheme) TemplateFuncs() template.FuncMap { return t.BaseFuncMap() }
func (t *MyTheme) TemplateDir() string             { return filepath.Join(t.ThemeDir, "templates") }
func (t *MyTheme) StaticDir() string               { return filepath.Join(t.ThemeDir, "static") }
```

**不需要手动改 `cmd/server/main.go`**。把目录拖到 `themes/`，确保根目录同时有 `theme.toml` 和至少一个非 test `.go` 文件，然后重新执行 `gopress serve`。autoload 包会被重新生成，新主题的 `init()` 在启动时自动调用 `core.RegisterTheme` 完成注册。详见 [安装与运行](../getting-started/installation.md)。

配置文件 `[site] theme = "my-theme"` 即可激活该主题。

> `theme.toml` 是必需的——它既是 gopress 自动发现的标记（缺它则 `themes/<name>/` 目录会被忽略），也承载内容类型与菜单位置声明，由 core 在激活时读取。

## 内容类型配置

主题自定义内容类型写在 `theme.toml`，不要在 `Setup()` 里重复调用 `RegisterType()`。引擎激活主题时会先注册核心类型 `post` / `page` / `contact_message` / `category` / `tag`，再读取当前主题的 `[[content_types]]` 并自动挂载配置的分类法。

下面以一个由主题声明的 `product` 内容管理项为例。`product` 不是 core 内置类型，只是一个常见的自定义内容类型示例。

```toml
[theme]
name = "My Theme"
version = "1.0.0"
description = "Example theme"
author = "Me"

[[content_types]]
name = "product"
label = "产品"
label_plural = "产品列表"
archive_title_key = "page_title_product"
supports = ["title", "content", "excerpt", "thumbnail", "sort_order"]
taxonomies = ["category", "tag"]
has_archive = true
rewrite_slug = "products"
menu_icon = "blocks"
menu_order = 1

[[content_types.meta_fields]]
key = "client"
label = "客户"
type = "string"

[[menu_locations]]
name = "header"
label = "顶部导航"
```

`menu_icon` 使用 admin 内置图标 key（例如 `blocks` / `edit` / `collection` / `post` / `contact_message` / `media`），也可以传入完整 SVG 字符串。`post` 和 `contact_message` 是核心内容类型，主题不应在 `theme.toml` 中重新声明。

主题版本必须是合法 semver。`BaseTheme` 会解析 `[theme]` 并实现 `Name`、
`Version`、`Description`、`Author`；主题不要在 Go 方法中重复这些值，确保
后台卡片和依赖校验都以 `theme.toml` 为唯一来源。

`product` 只是一个常见示例，不是 core 的固定假设。主题可以声明 `module`、`project`、`case_study`、`destination` 等任意业务内容类型。

前台多语言展示名应写在主题 locale 文件中，key 约定为 `content_type.<name>`。BaseTheme 会用这些 key 渲染分类归档页上的内容类型徽标，缺失时回退到 `theme.toml` 里的 `label`：

```json
{
  "content_type.product": "产品"
}
```

### Rewrite Slug 与模板映射

`rewrite_slug` 是该内容类型的公开 URL base。上面的 `product` 配置会生成：

```text
/products
/products/{content-slug}
```

当内容类型名、URL 和视觉模板名不一致时，不要在 Go handler 里手写特殊路由，而是在 `theme.toml` 里加 `templates`：

```toml
[[content_types]]
name = "module"
label = "模块"
label_plural = "核心模块"
archive_title_key = "page_title_module"
supports = ["title", "content", "excerpt", "thumbnail", "sort_order"]
taxonomies = ["category", "tag"]
has_archive = true
rewrite_slug = "modules"
templates = { archive = "products", single = "product-detail" }
menu_icon = "blocks"
menu_order = 1
```

这样数据模型是 `module`，前台 URL 是 `/modules` / `/modules/{slug}`，视觉层复用 `products` / `product-detail` 页面模板。内容模型、URL slug 和模板名互相独立，统一由 core 注册表驱动。`archive_title_key` 指向主题 locales 里的标题 key，用于归档页 `<title>` / Open Graph 标题，避免多语言站点直接使用静态 `label_plural`。

### 允许前台用户创作的内容类型

需要已登录用户在前台创作内容时，可以为主题内容类型声明 Core 通用提交策略：

```toml
[content_types.public_submission]
enabled = true
roles = ["subscriber", "contributor"]
default_status = "pending"
allow_update_own = true
allow_delete_own = true
```

主题激活期间，这项声明会产生内容类型级别的临时 RBAC 能力。路由和 UI 仍归主题负责，但写操作必须通过 `theme.PublicSubmissionApp.PublicSubmissionService()`，由 Core 统一执行账号状态、允许角色、能力、所有权、状态、输入校验、Slug 和限流检查。服务中的可信 `PublishImmediately` 字段绝不能直接绑定浏览器输入。详见[前台用户内容提交](../architecture/public-content-submission.md)。

## 模板命名约定

将模板放在 `themes/my-theme/templates/`。推荐使用 `layouts/` + `partials/` + `pages/` 的页面 bundle 结构：

```
templates/
├── layouts/base.tmpl           # 基础布局，定义 {{define "base"}}
├── partials/header.tmpl        # 可选局部模板
└── pages/
    ├── home.tmpl
    ├── products.tmpl           # 列表页页面 bundle
    ├── product-detail.tmpl     # 详情页页面 bundle
    ├── archive.tmpl            # 通用列表页（回退）
    └── single.tmpl             # 通用详情页（回退）
```

BaseTheme 会自动编译 `templates/pages/*.tmpl`。对于 `product` 类型、slug 为 `air-shower` 的详情页，会优先查找这些页面 bundle：

```text
single-product-air-shower
single-product
product-detail
products-detail
<theme.toml 中 templates.single>
single
```

对于 `product` 类型、`rewrite_slug = "products"` 的归档页，会查找：

```text
archive-product
products
product
<theme.toml 中 templates.archive>
archive
```

如果页面 bundle 没命中，BaseTheme 仍会回退到旧的根模板层级（`archive-product.tmpl` / `single-product.tmpl` / `archive.tmpl` / `single.tmpl` / `index.tmpl`），最后再使用内置 fallback 模板。

模板内链应走 core helper，避免路径和 `theme.toml` 配置脱节：

```gotemplate
<a href="{{archiveURL "product"}}">产品</a>
<a href="{{contentURL . "product"}}">{{.Title}}</a>
<a href="{{taxonomyURL "category" .Slug}}">{{.Name}}</a>
```

`archiveURL`、`contentURL` 和 `taxonomyURL` 会读取 Rewrite 注册表；后续把 `rewrite_slug = "products"` 改成 `catalog` 时，模板不需要跟着硬改。

动态归档页也会识别该内容类型声明过的 taxonomy query 参数。例如 `post` 声明了 `taxonomies = ["category", "tag"]` 时，`/blog?category=industry-news` 和 `/blog?tag=cleanroom` 会应用对应过滤；未挂载到该内容类型的 taxonomy query 会被忽略。

上述查询参数应只作为兼容入口或不参与索引的界面筛选。可索引的 term 落地页必须使用 `taxonomyURL`，并按需套用 `langPrefixURL`，让站内链接、taxonomy canonical 与 sitemap 统一指向 `/category/{term}` 或 `/tag/{term}`。

BaseTheme 提供给模板的 taxonomy 行已经应用当前请求 Scope。插件启用独立 term
翻译时，`.Name`、`.Slug`、父级关系与 `.URL` 都代表当前语言 identity；主题不得
自行用无 Scope 的 ID 查询，也不得解析翻译组。保持稳定的 `category` / `tag`
基础路径，由 `taxonomyURL` 与 `langPrefixURL` 组合规范 URL。

导航当前页状态同样应走 core helper，让模板只关心菜单 URL，不关心业务内容类型名或菜单标题：

```gotemplate
{{with menuByLocation "header"}}
    {{range .Items}}
        <a href="{{.URL}}" class="{{if isMenuURLActive $.Ctx .URL}}active{{end}}">{{.Title}}</a>
    {{end}}
{{end}}
```

不要在通用主题里写 `.ActivePage == "products"` 这类判断。菜单名称、内容类型名和 `rewrite_slug` 都是配置，不应成为模板代码里的固定契约。

`goPressVersion` 会从唯一版本源 `version/version.go` 返回当前运行中的 Core 版本。主题渲染文档版本或诊断信息时应使用该 helper，不要把版本号复制到主题设置或模板里：

```gotemplate
<span>GoPress Core v{{goPressVersion}}</span>
```

## 基础布局契约

主题的 `layouts/base.tmpl` 是前台插件接入的主要契约面。GoPress 内置主题和面向生产使用的第三方主题**必须**声明这些标准插槽，插件才能在不修改主题文件的前提下注入站点级代码、导航扩展或其它局部 HTML。只用于封闭场景的最小主题可以主动省略插槽，但应明确视为不兼容通用前台插件。

```gotemplate
{{define "base"}}<!DOCTYPE html>
<html lang="{{currentLang .Ctx}}">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    {{$fallbackTitle := printf "%s - %s" .Title (settingOr .Settings "site_name" "My Theme")}}
    <title>{{pageTitleFor . $fallbackTitle}}</title>
    {{with seoHeadFor .}}{{.}}{{else}}<meta name="description" content="{{settingOr $.Settings "site_description" "My theme default description."}}">{{end}}
    <link rel="stylesheet" href="/static/css/style.css">
    {{renderHook "theme.head.end" .}}
</head>
<body>
    {{renderHook "theme.body.open" .}}
    {{template "header" .}}
    <main>
        {{template "content" .}}
    </main>
    {{template "footer" .}}
    <script src="/static/js/main.js"></script>
    {{renderHook "theme.footer.end" .}}
</body>
</html>{{end}}
```

导航扩展插槽属于 Header 契约，应放在主导航列表末尾，并传入完整的当前模板数据：

```gotemplate
{{define "header"}}
<header class="site-header">
    <nav class="site-nav" aria-label="Primary navigation">
        <ul>
            {{with menuByLocation "header"}}
                {{range .Items}}
                <li><a href="{{.URL}}" class="{{if isMenuURLActive $.Ctx .URL}}active{{end}}">{{.Title}}</a></li>
                {{end}}
            {{end}}
            {{renderHook "header.nav.after" .}}
        </ul>
    </nav>
</header>
{{end}}
```

位置约定：

- `theme.head.end` 放在 `</head>` 前，用于站点验证 meta、Analytics、preconnect、第三方 CSS 等。
- `theme.body.open` 放在 `<body>` 后立即输出，用于 GTM noscript、A/B 测试 bootstrap、全站公告条等。
- `theme.footer.end` 放在 `</body>` 前且在主题脚本之后，用于客服 widget、热力图、延迟加载追踪脚本等。
- `header.nav.after` 放在导航列表尾部，插件输出应匹配周围结构，通常是 `<li>...</li>`。

这些插槽应在基础布局或对应语义位置只声明一次，避免插件输出重复。

### 导航扩展的样式与交互

导航样式应限定在主题拥有的顶层结构，避免覆盖扩展项内部的下拉菜单：

```css
/* 推荐：只定义主导航的直接子项。 */
.site-nav > ul > li > a { /* ... */ }

/* 避免：会同时覆盖扩展注入的嵌套菜单。 */
.site-nav ul { /* ... */ }
.site-nav a { /* ... */ }
```

移动端导航必须允许顶层扩展项包含直接子级 `<ul>`。展开逻辑应按 DOM 结构识别子菜单，而不是判断某个插件类名；同时维护触发项的 `aria-expanded`，关闭主导航时收起已打开的子菜单。主题不得引用插件包、插件私有配置或类似 `.gp-lang-*` 的实现类名。

### 标准前台契约检查表

| 检查项 | 要求 |
|---|---|
| `theme.head.end` | 恰好一次，位于 `</head>` 前 |
| `theme.body.open` | 恰好一次，位于 `<body>` 后 |
| `theme.footer.end` | 恰好一次，位于主题脚本之后、`</body>` 前 |
| `header.nav.after` | 恰好一次，位于主导航 `<ul>` 内并传入 `.` |
| CSS 作用域 | 顶层导航使用直接子级选择器，不覆盖扩展项的嵌套 `<ul>` |
| 移动端 | 通用支持含子菜单的扩展项，并同步 `aria-expanded` 状态 |
| 架构边界 | 只使用 core Hook；主题不识别或依赖具体插件实现 |

仓库级 `internal/contracts/theme_contracts_test.go` 会自动发现所有包含 `theme.toml` 的内置主题，并检查四个标准 Hook 的数量、模板数据参数和语义位置。新增主题无需登记，但必须通过：

```bash
go test ./internal/contracts
```

## 主题目录结构（推荐）

```
themes/my-theme/
├── theme.go                  # 主题入口 + init() 自注册
├── theme.toml                # 主题元信息 + 内容类型 + 菜单位置
├── handlers.go               # 自定义页面处理器（可选）
├── services.go               # 业务服务层（可选，自定义 struct 主题）
├── functions.go              # 模板函数扩展（可选）
├── translatable.go           # 可翻译设置键声明（可选，多语言主题用）
├── locales/                  # i18n 翻译文件
│   ├── en.json
│   └── zh.json
├── demo/data/seed.toml       # 内置演示数据（可选）
├── static/
│   ├── logo.svg              # 后台主题卡片图标（可选，见下文）
│   ├── css/style.css
│   └── js/main.js
└── templates/
    ├── layouts/
    ├── partials/
    └── pages/
```

## 可选接口

```go
// DemoDataProvider — 实现后，后台可一键导入演示数据
func (t *MyTheme) DemoSeedPath() string {
    return filepath.Join(t.ThemeDir, "demo", "data", "seed.toml")
}
```

## 依赖插件

如果主题需要某些插件才能正常工作，可在 `theme.toml` 的 `[requires]` 段按 slug 声明依赖，core 会在切换主题时预检、并自动启用未激活的所需插件。详见 [主题依赖与版本](dependencies.md)。

## 后台卡片 Logo

后台「主题管理」的卡片会在标题旁显示主题图标。约定极简：**在 `static/logo.svg` 放一个 SVG 即可**——`BaseTheme` 已实现可选接口 `LogoProvider.LogoSVG()`，会自动读取该文件，**无需写任何 Go 代码**。

- 建议用 `viewBox="0 0 48 48"` 的方形图标（卡片按约 34px 显示）。
- core 会先用 `content.SanitizeSVG` 清洗（剥离 `<script>` / `on*` 事件 / `javascript:` 等）再内联进后台页面，所以即使第三方主题的 logo 也不会把脚本带进后台 origin。
- 没有该文件时卡片不显示图标，行为不变。
- 如需动态生成 logo，可在主题上自行覆盖 `LogoSVG() string` 方法。

## 主题设置页

主题通常会提供一个「主题设置」页让运营调内容（hero 图、品牌名、CTA 文案等）。约定：

- 设置 key 用 `home_` / `about_` / `social_` / `footer_` 等前缀，引擎才会持久化
- 全主题共用的"站点名称 / 简介" **不要** 用 `company_name` 之类的本地 key 收集，统一走 admin「系统设置 > 网站设置」的 `site_name` / `site_description`。详见 [SEO 接入规范](seo-integration.md)
- 把 `home_logo_image` / `home_logo_combined_image` 这类图片字段配上「选择图片」按钮调用 `openMediaPicker(callback)`

## 日期与站点时区

新主题展示内容发布时间时，优先使用 `BaseFuncMap()` 提供的 `formatDate` / `formatDateTime`。这两个 helper 会读取 admin「系统设置 > 网站设置」里的 `site_timezone`，把数据库中的 UTC 时间转换到站点时区后再输出。

如果主题确实需要自定义日期格式函数，不要直接 `tm.Format(...)`，应先转到 `engine.SiteLocation()`：

```go
func New(engine *core.Engine, themeDir string) *MyTheme {
    t := &MyTheme{engine: engine}
    t.InitBase(engine, themeDir, template.FuncMap{
        "formatLongDate": func(tm *time.Time) string {
            if tm == nil {
                return ""
            }
            return tm.In(engine.SiteLocation()).Format("2006-01-02")
        },
    })
    t.LoadTemplates(t)
    return t
}
```

这样新主题、后台列表和 sitemap 使用的是同一套发布时间语义：输入按站点时区解析，数据库统一存 UTC，展示再按站点时区转换。老站点没有 `site_timezone` 时会回退到服务器本地时区，建议在系统设置里保存一个明确值。

## 推荐：BaseTheme + gin.H 路径

新主题强烈推荐这条路径——SEO 注入完全免费，未来 core 长出新能力（比如 og:image 兜底、per-page robots）也是零改动跟上：

```go
type MyTheme struct {
    coreTheme.BaseTheme
    engine *core.Engine
}

func (t *MyTheme) ServeHTTP(c *gin.Context) {
    t.BaseTheme.ServeHTTP(c)  // 自动注入 .SEO 到 home / archive / single
}
```

这样最省事：完全不用写 `PageService` / 自定义 `PageData struct`，BaseTheme 把 home / archive / single 三类页面渲染都做了。

如果你更想要类型安全的数据装配，也可以写一个 `PageService`——现在只需嵌入 core 的共享脚手架 `coreTheme.BasePageService`（需要 SEO 的用 `coreTheme.SEOPageService`），DB / 仓储 / Options / 请求作用域 / SEO 构建都是继承来的，不用自己抄。详见 [SEO 接入规范](seo-integration.md) 的"类型化写法"段。

## 类型安全担忧？

类型安全和 BaseTheme 不冲突——可以用 `BaseTheme + gin.H` 的路由 / SEO，同时把内部数据写成类型化切片塞进 map：

```go
data := gin.H{
    "Title":    "Products",
    "Products": productViews, // []ProductView，模板里照样有字段提示
}
```

这样既享受框架级免维护，又保留了模板里的智能提示。

## 登录评论与 Profile 路由

主题实现前台内容提交、评论、账号页、收藏等登录态工作流时：

- 只使用 core 提供的 `currentUser`、`loginURL`、`loginProviders`、`loginProviderURL`、`PublicSubmissionApp`、`CommentApp`、`PublicAuthorizationApp` 通用契约。
- 所需身份插件只能在 `theme.toml` 声明；运行时不得 import 插件、读取私有配置或判断 Provider ID。
- 每个写路由都必须执行同源校验和明确的 `resource.action` 权限检查。
- 表单中的内容 ID、评论 ID 必须校验类型、目标、所有权和父子归属。
- 当前用户资料页使用固定路径，并设置 `Cache-Control: private, no-store`，不能泄露其他用户邮箱。
- 测试必须证明匿名或无权限角色被拒绝，并覆盖有权限角色成功执行。

详见[前台用户内容提交](../architecture/public-content-submission.md)和[评论与回复](../architecture/comments.md)。
