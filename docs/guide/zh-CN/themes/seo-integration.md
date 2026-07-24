# 主题 SEO 接入规范

GoPress 把 SEO 的所有数据来源统一在框架层，主题只负责"消费"。这一节描述的是**契约**，新写主题必须遵守，老主题改 base.tmpl 时也照这套来。

本页代码和 URL 示例以主题声明的 `product` 内容类型为例。`product` 不是 core 内置类型，只是一个常见的自定义内容类型示例。

## 数据流

```
admin 「系统设置 > 网站设置」
  ├── site_name           ─┐
  ├── site_description    ─┤
  └── site_icon           ─┤
                           ▼
core SEOBuilder (per-page)
  ├── ForHome(siteDescription)            → home
  ├── ForArchiveTitle(typeDef, title)     → /products, /blog ...
  └── ForContent(item, typeDef)           → /products/:slug, /blog/:slug ...
                           ▼
core ApplySiteOptionOverridesForRequest
  ├── 用 admin 的 site_name 覆盖 cfg.Site.Name 拼出来的 og:title / Title
  ├── 按当前请求语言翻译 site_name / site_description
  ├── description 为空或等于原始 site_description 时写入当前语言描述
  └── site_icon 非空时写入 SEOMeta.SiteIcon
                           ▼
data["SEO"] = SEOMeta{...}            （BaseTheme 路径自动注入）
data.PageData.SEO = SEOMeta{...}      （自定义 struct 主题手动注入）
                           ▼
template: {{pageTitleFor . $fallbackTitle}} + {{with seoHeadFor .}}{{.}}{{else}}<meta description fallback>{{end}}
                           ▼
HTML <head>: <title> + <meta description> + <link canonical> + og:* + JSON-LD + favicon links
```

## 必须遵守的三条契约

### 1. `<title>` 走 `pageTitleFor` + 主题 fallback

`pageTitleFor` 是 core 的页面标题 helper，不是 SEO 插件依赖。它会优先使用 core 注入的页面 metadata 标题；如果没有，就返回主题传入的 fallback。因此插件禁用时页面标题仍然正常。不要硬编码品牌字、不要发明 `company_name` 之类的本地 key：

```gotemplate
<!-- ✅ 正确：core 页面标题 + 主题兜底默认 -->
{{$fallbackTitle := printf "%s - %s" .Title (settingOr .Settings "site_name" "My Theme Default")}}
<title>{{pageTitleFor . $fallbackTitle}}</title>

<!-- ❌ 错：硬编码 -->
<title>{{.Title}} - Hurricane Techs</title>

<!-- ❌ 错：用主题自己的 company_name，绕开 core 的 site_name -->
<title>{{.Title}} - {{settingOr .Settings "company_name" "..."}}</title>
```

`company_name` 这个 key 可以保留供 footer "© CompanyName" 等正文用途（"公司名" 和 "站点名" 概念可分离），但**绝不能出现在 `<title>`**。

### 2. `<meta name="description">` 走 `seoHeadFor` + 兜底链

```html
{{$siteIcon := settingOr .Settings "site_icon" ""}}
{{with seoHeadFor .}}
  {{.}}
{{else}}
  <meta name="description" content="{{settingOr $.Settings "site_description" "My Theme Default"}}">
  {{faviconLinks $siteIcon}}
{{end}}
```

`seoHeadFor` 是核心提供的 reflection-based helper，对 `gin.H` 和自定义 struct **都安全**：找不到 SEO 字段就返回空字符串，模板自动 fallback 到 else 分支，永远不会因为字段缺失把页面渲染成白屏。注意 `with` 改变了 `.` 指向，要用 `$.Settings` 访问根上下文。

### 3. favicon 统一走 `site_icon`

后台「系统设置 > 网站设置」里的 `site_icon` 是全主题统一的网站图标来源。主题不要再发明 `favicon_url`、`theme_icon` 之类的本地 key。

- 正常 SEO 分支：`ApplySiteOptionOverrides` 会把 `site_icon` 写入 `SEOMeta.SiteIcon`，`seoHeadFor` 会先渲染 `/favicon.ico`，再渲染带 `type` / `sizes` 的图片 icon 和 Apple touch icon
- fallback 分支：如果页面没有 `SEO` 字段，layout 的 `else` 分支应调用 `{{faviconLinks $siteIcon}}`，确保和 SEO 分支输出一致

`/favicon.ico`、`/static/*`、`/sitemap.xml`、`/robots.txt` 都支持 `HEAD` 和 `GET`。静态文件不存在时返回 `Cache-Control: no-store`，避免搜索引擎或中间缓存把 favicon / 图片的 404 结果长期缓存。

浏览器和搜索引擎会强缓存 favicon。修改 `site_icon` 后如果标签已经输出但仍显示旧图标，先强刷或清理站点缓存，再在搜索引擎站长工具中重新抓取关键 URL。

## 推荐写法：BaseTheme + gin.H

新主题强烈推荐这条路径——SEO 注入完全免费，未来 core 长出新能力（比如 og:image 兜底、per-page robots）也是零改动跟上。

归档页标题如果需要多语言，内容类型应在 `theme.toml` 里声明 `archive_title_key`：

```toml
[[content_types]]
name = "service"
label_plural = "服务列表"
archive_title_key = "page_title_service"
rewrite_slug = "services"
```

BaseTheme 会按当前请求语言从主题 locales 读取该 key，用它生成归档页 `<title>` / Open Graph 标题。未配置时，core 会尝试 `page_title_<rewrite_slug>` 这类通用 key，最后才回退到 `label_plural`。

```go
// theme.go
type MyTheme struct {
    coreTheme.BaseTheme
    engine *core.Engine
}

func (t *MyTheme) ServeHTTP(c *gin.Context) {
    t.BaseTheme.ServeHTTP(c)  // 自动注入 .SEO 到 home / archive / single
}
```

```html
<!-- templates/layouts/base.tmpl -->
{{$fallbackTitle := printf "%s - %s" .Title (settingOr .Settings "site_name" "My Theme")}}
<title>{{pageTitleFor . $fallbackTitle}}</title>
{{$siteIcon := settingOr .Settings "site_icon" ""}}
{{with seoHeadFor .}}{{.}}{{else}}
<meta name="description" content="{{settingOr $.Settings "site_description" "..."}}">
{{if $siteIcon}}<link rel="icon" href="{{$siteIcon}}">
<link rel="apple-touch-icon" href="{{$siteIcon}}">{{end}}
{{end}}
```

收工。所有 SEO 标签自动出现。

## 类型化写法：自定义 PageData struct + PageService

如果你出于类型安全或代码风格选择自己写 `PageService` + 自定义 data struct（参考 modern-company / financial-news），现在成本很低：**直接嵌入 core 提供的脚手架**即可，公共管道（DB / 仓储 / Options / 请求作用域）和 SEO 构建都不用自己写。

- 需要 SEO 的主题嵌入 **`coreTheme.SEOPageService`**（自带 `BuildHomeSEO / BuildArchiveSEO / BuildContentSEO`）。
- 不需要 SEO 的主题嵌入 **`coreTheme.BasePageService`**（只有数据访问管道，没有 SEO 方法）。

```go
import (
    "github.com/gin-gonic/gin"
    "gorm.io/gorm"

    "go-press/core"
    "go-press/core/rewrite"
    coreTheme "go-press/core/theme"
)

// 1. PageData 加 SEO 字段
type PageData struct {
    Title    string
    Settings map[string]string
    SEO      rewrite.SEOMeta // ← 必加
    // ...
}

// 2. PageService 嵌入 SEOPageService
//    （继承 DB / Content / Tax / Options / SEOBuilder / Registry / Hooks / I18n
//      + ReqCtx + BuildHomeSEO / BuildArchiveSEO / BuildContentSEO）
type PageService struct {
    coreTheme.SEOPageService
}

func NewPageService(engine *core.Engine) *PageService {
    return &PageService{coreTheme.NewSEOPageService(
        coreTheme.NewBasePageService(engine.DB, engine.Content, engine.Taxonomy, engine.Options),
        engine.SEO, engine.Registry, engine.Hooks, engine.I18n)}
}

// DB-only 构造（CLI / 测试）；SEO 字段传 nil 时构建方法自动返回零值 SEOMeta
func NewPageServiceDB(db *gorm.DB) *PageService {
    return &PageService{coreTheme.NewSEOPageService(coreTheme.NewBasePageServiceDB(db), nil, nil, nil, nil)}
}

// 3. 请求作用域克隆：替换内嵌 base、保留自定义字段
func (s *PageService) ForRequest(c *gin.Context) *PageService {
    clone := *s
    clone.BasePageService = s.BasePageService.ForRequest(c)
    return &clone
}

// 4. 各 Get*Data 直接调用继承来的 SEO 构建方法
//    下面以主题声明的 product 内容类型为例；product 不是 core 内置类型。
func (s *PageService) GetProductDetail(slug string) (*ProductDetailData, error) {
    item, _ := s.Content.FindBySlugScoped(s.ReqCtx, "product", slug)

    data := &ProductDetailData{ /* ... */ }
    data.SEO = s.BuildContentSEO(item, "product") // 已含站点选项覆盖 + per-content meta filter
    return data, nil
}
```

`SEOPageService` 内部已经调好了下面这几个 core helper（主题不用再手动调用，也不要复制它们的实现）：

- **`LocalizedArchiveTitle`** — 按当前请求语言读取 `archive_title_key` 或 `page_title_<rewrite_slug>`，让归档页标题跟随 locales
- **`ApplySiteOptionOverridesFromOptionsForRequest`** — 按当前请求语言翻译系统 `site_name` / `site_description`，替换标题里的站点名后缀，在描述为空或等于原始系统描述时写入当前语言描述，并把 `site_icon` 写入 `seo.SiteIcon`
- **`ApplyContentMetaSEO`** — 触发 `seo.content.meta` filter 链，让 [seo-extras 插件](../plugins/seo-extras.md) 这类 per-content SEO 覆盖插件能修改 SEOMeta

不需要 SEO 的主题只嵌入 `BasePageService`：

```go
type PageService struct {
    coreTheme.BasePageService
}

func NewPageService(engine *core.Engine) *PageService {
    return &PageService{coreTheme.NewBasePageService(engine.DB, engine.Content, engine.Taxonomy, engine.Options)}
}

func (s *PageService) ForRequest(c *gin.Context) *PageService {
    return &PageService{s.BasePageService.ForRequest(c)}
}
```

`BaseTheme + gin.H` 渲染的归档 / 单内容页完全不用关心 SEO，core 的 `renderSingle` 已经替你调好了。

> 注意：`SEOPageService` 用的是 **request-aware** 的 `ApplySiteOptionOverridesFromOptionsForRequest`，会按当前语言把「网站设置翻译」接进 `<title>` / `<meta description>`。少数单页、无逐请求多语言的主题（如 go-press-landing）可以只嵌入 `BasePageService` 并自写一个用非请求版 `ApplySiteOptionOverridesFromOptions` 的 `buildHomeSEO`。

## 类型安全 vs 框架免维护

类型安全和 BaseTheme 不冲突——可以用 `BaseTheme + gin.H` 的路由 / SEO，同时把内部数据写成类型化切片塞进 map：

```go
data := b.buildBaseData("Products")
data["Products"] = productViews  // []ProductView，模板里照样有字段提示
```

这样既享受框架级免维护，又保留了模板里的智能提示。

## 现状参考表

所有内置主题的 `PageService` 现在都嵌入 core 提供的脚手架，接入方式已统一：

| 主题 | PageService 脚手架 | SEO 来源 |
|---|---|---|
| atelier-slate / civic-estate / florafi（FloraFi） / terra-trail / axis-form（Axis Form） | `coreTheme.BasePageService` | 归档 / 单内容页由 BaseTheme 自动注入；首页等自定义页走 gin.H |
| modern-company / financial-news | `coreTheme.SEOPageService` | 继承 `BuildHomeSEO` / `BuildArchiveSEO` / `BuildContentSEO` |
| go-press-landing | `coreTheme.BasePageService` + 自写 `buildHomeSEO` | 单页，无逐请求多语言，用非请求版 `ApplySiteOptionOverridesFromOptions` |

新主题按需二选一嵌入 `BasePageService` 或 `SEOPageService`，只写自己的 `Get*Data` 与 view 类型即可，不用再复制那套管道与 SEO 代码。

## Per-content SEO 覆盖

默认情况下，单内容页的 SEO 字段是从内容自身字段推断的：

| SEOMeta 字段 | 默认数据源 |
|---|---|
| `<meta description>` / `og:description` | `Content.Excerpt`（自动 truncate 到 160） |
| `og:image` | `Content.ImageURL` |
| `og:title` | `Content.Title` |
| `<meta robots>` | `index, follow` |

**如果你希望像 WordPress + Yoast SEO 那样允许编辑给每条内容写独立的 SEO 标题/描述/分享图/robots**，激活内置 [seo-extras 插件](../plugins/seo-extras.md) 即可——内容编辑页底部会多出一个折叠的「SEO 设置（可选）」面板。

实现层面这套是**纯插件**：core 完全不动，插件消费 3 个公开 hook：

```
admin.content_form.fields  filter → 渲染 meta box HTML
admin.content.saved         action → 把 form 值存进 gp_content_meta（_seo_*）
seo.content.meta            filter → 在 SEOBuilder 输出后 patch SEOMeta
```

整套数据流（包含插件介入）变成：

```
SEOBuilder.ForContent(item, typeDef)
                ▼
ApplySiteOptionOverridesForRequest（当前语言 site_name / site_description / site_icon 兜底）
                ▼
ApplyContentMetaSEO              （触发 seo.content.meta filter 链）
   ├── seo-extras 插件插入        （读 _seo_* meta，覆盖 Title/Description/OGImage/Robots）
   └── 你的其它 SEO 插件...
                ▼
data.SEO / data["SEO"]          （注入模板）
                ▼
{{seoHeadFor .}}                （渲染 HTML）
```

## 自己写"扩展 SEO"插件

如果你想加自己的 SEO 字段（比如 schema.org 产品规格、自定义 og:type），完全可以再写一个插件，跟 seo-extras 并存。每个插件订阅同一组 hook，按 `priority` 顺序累加修改 SEOMeta，互不干扰。

这才是这套架构的真正价值：**SEO 不是 core 写死的特性，而是可叠加的插件能力**。
