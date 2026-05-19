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

func (t *MyTheme) Name() string        { return "My Theme" }
func (t *MyTheme) Version() string     { return "1.0.0" }
func (t *MyTheme) Description() string { return "My custom theme" }
func (t *MyTheme) Author() string      { return "Me" }

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

主题自定义内容类型写在 `theme.toml`，不要在 `Setup()` 里重复调用 `RegisterType()`。引擎激活主题时会先注册核心类型 `post` / `contact_message` / `category` / `tag`，再读取当前主题的 `[[content_types]]` 并自动挂载配置的分类法。

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

`product` 只是一个常见示例，不是 core 的固定假设。主题可以声明 `module`、`project`、`case_study`、`destination` 等任意业务内容类型。

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
supports = ["title", "content", "excerpt", "thumbnail", "sort_order"]
taxonomies = ["category", "tag"]
has_archive = true
rewrite_slug = "modules"
templates = { archive = "products", single = "product-detail" }
menu_icon = "blocks"
menu_order = 1
```

这样数据模型是 `module`，前台 URL 是 `/modules` / `/modules/{slug}`，视觉层复用 `products` / `product-detail` 页面模板。内容模型、URL slug 和模板名互相独立，统一由 core 注册表驱动。

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
```

`archiveURL` 和 `contentURL` 会读取 Rewrite 注册表；后续把 `rewrite_slug = "products"` 改成 `catalog` 时，模板不需要跟着硬改。

## 基础布局契约

主题的 `layouts/base.tmpl` 是前台插件接入的主要契约面。新主题应在基础布局中声明这些标准插槽，插件才能在不修改主题文件的前提下注入站点级代码、语言切换器或其它局部 HTML。

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

位置约定：

- `theme.head.end` 放在 `</head>` 前，用于站点验证 meta、Analytics、preconnect、第三方 CSS 等。
- `theme.body.open` 放在 `<body>` 后立即输出，用于 GTM noscript、A/B 测试 bootstrap、全站公告条等。
- `theme.footer.end` 放在 `</body>` 前且在主题脚本之后，用于客服 widget、热力图、延迟加载追踪脚本等。
- `header.nav.after` 放在导航列表尾部，插件输出应匹配周围结构，通常是 `<li>...</li>`。

这些插槽应在基础布局或对应语义位置只声明一次，避免插件输出重复。

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

## 主题设置页

主题通常会提供一个「主题设置」页让运营调内容（hero 图、品牌名、CTA 文案等）。约定：

- 设置 key 用 `home_` / `about_` / `social_` / `footer_` 等前缀，引擎才会持久化
- 全主题共用的"站点名称 / 简介" **不要** 用 `company_name` 之类的本地 key 收集，统一走 admin「系统设置 > 网站设置」的 `site_name` / `site_description`。详见 [SEO 接入规范](seo-integration.md)
- 把 `home_logo_image` / `home_logo_combined_image` 这类图片字段配上「选择图片」按钮调用 `openMediaPicker(callback)`

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

完全不用写 `PageService` / 自定义 `PageData struct`，BaseTheme 把 home / archive / single 三类页面渲染都做了。详见 [SEO 接入规范](seo-integration.md) 的"推荐写法"段。

## 类型安全担忧？

类型安全和 BaseTheme 不冲突——可以用 `BaseTheme + gin.H` 的路由 / SEO，同时把内部数据写成类型化切片塞进 map：

```go
data := b.buildBaseData("Products")
data["Products"] = productViews  // []ProductView，模板里照样有字段提示
```

这样既享受框架级免维护，又保留了模板里的智能提示。
