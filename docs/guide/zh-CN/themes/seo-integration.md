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

## 兼容写法：自定义 PageData struct

如果你出于类型安全或代码风格选择自己写 `PageService` + 自定义 data struct（参考 modern-company / financial-news / go-press-landing），需要**手动**把 SEO 接进去——否则模板里 `seoHeadFor` 返回空，只会走 fallback 兜底，拿不到 canonical / og / JSON-LD：

```go
import (
    "go-press/core/hook"
    "go-press/core/rewrite"
    coreTheme "go-press/core/theme"
)

// 1. PageData 加 SEO 字段
type PageData struct {
    Title      string
    Settings   map[string]string
    SEO        rewrite.SEOMeta   // ← 必加
    // ...
}

// 2. PageService 引用 SEOBuilder + Registry + HookBus + I18n
type PageService struct {
    options    *option.Store
    seoBuilder *rewrite.SEOBuilder   // engine.SEO
    registry   *content.Registry     // engine.Registry
    hookBus    *hook.Bus             // engine.Hooks
    contentRepo *content.Repository  // engine.Content
    i18nMgr    *coreI18n.Manager     // engine.I18n
    reqCtx     *gin.Context          // ForRequest(c) 注入
}

// 3. 每个请求先克隆服务，保存 gin.Context
func (s *PageService) ForRequest(c *gin.Context) *PageService {
    clone := *s
    clone.reqCtx = c
    return &clone
}

// 4. 各 Get*Data 方法构建 SEO
func (s *PageService) buildArchiveSEO(typeName string) rewrite.SEOMeta {
    typeDef := s.registry.GetType(typeName)
    title := coreTheme.LocalizedArchiveTitle(s.reqCtx, s.i18nMgr, typeDef)
    seo := s.seoBuilder.ForArchiveTitle(typeDef, title)
    coreTheme.ApplySiteOptionOverridesFromOptionsForRequest(s.reqCtx, s.options, s.i18nMgr, s.seoBuilder, &seo)
    return seo
}

// 下面以主题声明的 product 内容类型为例；product 不是 core 内置类型。
func (s *PageService) GetProductDetail(slug string) (*ProductDetailData, error) {
    item, _ := s.contentRepo.FindBySlugScoped(s.reqCtx, "product", slug)
    typeDef := s.registry.GetType("product")

    seo := s.seoBuilder.ForContent(item, typeDef)
    coreTheme.ApplySiteOptionOverridesFromOptionsForRequest(s.reqCtx, s.options, s.i18nMgr, s.seoBuilder, &seo) // 当前语言 site_name/site_description + site_icon 覆盖
    coreTheme.ApplyContentMetaSEO(s.hookBus, s.contentRepo, &seo, item)     // ← 让 SEO 插件能 patch

    data := &ProductDetailData{ /* ... */ }
    data.SEO = seo
    return data, nil
}
```

这些 core helper 不要在主题里复制实现：

- **`LocalizedArchiveTitle`** — 按当前请求语言读取 `archive_title_key` 或 `page_title_<rewrite_slug>`，让归档页标题跟随 locales
- **`ApplySiteOptionOverridesFromOptionsForRequest`** — 按当前请求语言翻译系统 `site_name` / `site_description`，替换标题里的站点名后缀，在描述为空或等于原始系统描述时写入当前语言描述，并把 `site_icon` 写入 `seo.SiteIcon`
- **`ApplyContentMetaSEO`** — 触发 `seo.content.meta` filter 链，让 [seo-extras 插件](../plugins/seo-extras.md) 这类 per-content SEO 覆盖插件能修改 SEOMeta

`BaseTheme + gin.H` 主题完全不用关心，core 的 `renderSingle` 已经替你调好了。这又是一个倾向 BaseTheme 的理由——插件生态默认就工作。

如果自定义主题只调用旧的 `ApplySiteOptionOverridesFromOptions`，单语言 SEO 仍能工作，但 multilang 的「网站设置翻译」不会进入 `<title>` / `<meta description>`。因此只要主题自己构建 `SEOMeta`，就应该保存当前请求 `gin.Context` 和 `i18n.Manager`，并改用 request-aware helper。

## 类型安全 vs 框架免维护

类型安全和 BaseTheme 不冲突——可以用 `BaseTheme + gin.H` 的路由 / SEO，同时把内部数据写成类型化切片塞进 map：

```go
data := b.buildBaseData("Products")
data["Products"] = productViews  // []ProductView，模板里照样有字段提示
```

这样既享受框架级免维护，又保留了模板里的智能提示。

## 现状参考表

| 主题 | 渲染路径 | SEO 接入方式 |
|---|---|---|
| atelier-slate / civic-estate / florafi（FloraFi） / terra-trail / axis-form（Axis Form） | BaseTheme + gin.H | 框架自动 |
| modern-company / atelier-slate-gp / financial-news | 自定义 PageData struct + core SEOBuilder | PageService 手动注入，必须使用 `ApplySiteOptionOverridesFromOptionsForRequest` |
| go-press-landing | 自定义 PageData struct + core SEOBuilder | 需要把 `gin.Context` / `i18n.Manager` 传入 PageService 后使用 request-aware helper，才能支持网站设置翻译 |
| bitcuz-mag | 完全自定义 SEO 生成 | 需要在自定义 `homeSEO` / `postSEO` 中调用 core 翻译 helper，或改造为 `SEOMeta` 构建后统一走 request-aware override |

未来新主题除非有非常明确的理由，**默认走 BaseTheme + gin.H**，避免重复 modern-company 那套 PageService 代码。

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
