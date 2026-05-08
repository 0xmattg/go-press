# 主题 SEO 接入规范

GoPress 把 SEO 的所有数据来源统一在框架层，主题只负责"消费"。这一节描述的是**契约**，新写主题必须遵守，老主题改 base.tmpl 时也照这套来。

本页代码和 URL 示例以主题声明的 `product` 内容类型为例。`product` 不是 core 内置类型，只是一个常见的自定义内容类型示例。

## 数据流

```
admin 「系统设置 > 网站设置」
  ├── site_name           ─┐
  └── site_description    ─┤
                           ▼
core SEOBuilder (per-page)
  ├── ForHome(siteDescription)            → home
  ├── ForArchive(typeDef)                 → /products, /blog ...
  └── ForContent(item, typeDef)           → /products/:slug, /blog/:slug ...
                           ▼
core ApplySiteOptionOverrides
  ├── 用 admin 的 site_name 覆盖 cfg.Site.Name 拼出来的 og:title / Title
  └── description 为空时回填 site_description（兜底 chain）
                           ▼
data["SEO"] = SEOMeta{...}            （BaseTheme 路径自动注入）
data.PageData.SEO = SEOMeta{...}      （自定义 struct 主题手动注入）
                           ▼
template: {{with seoHeadFor .}}{{.}}{{else}}<meta description fallback>{{end}}
                           ▼
HTML <head>: <meta description> + <link canonical> + og:* + JSON-LD
```

## 必须遵守的两条契约

### 1. `<title>` 拼接走 `site_name`

不要硬编码品牌字、不要发明 `company_name` 之类的本地 key：

```html
<!-- ✅ 正确：core 唯一来源 + 主题兜底默认 -->
<title>{{.Title}} - {{settingOr .Settings "site_name" "My Theme Default"}}</title>

<!-- ❌ 错：硬编码 -->
<title>{{.Title}} - Hurricane Techs</title>

<!-- ❌ 错：用主题自己的 company_name，绕开 core 的 site_name -->
<title>{{.Title}} - {{settingOr .Settings "company_name" "..."}}</title>
```

`company_name` 这个 key 可以保留供 footer "© CompanyName" 等正文用途（"公司名" 和 "站点名" 概念可分离），但**绝不能出现在 `<title>`**。

### 2. `<meta name="description">` 走 `seoHeadFor` + 兜底链

```html
{{with seoHeadFor .}}
  {{.}}
{{else}}
  <meta name="description" content="{{settingOr $.Settings "site_description" "My Theme Default"}}">
{{end}}
```

`seoHeadFor` 是核心提供的 reflection-based helper，对 `gin.H` 和自定义 struct **都安全**：找不到 SEO 字段就返回空字符串，模板自动 fallback 到 else 分支，永远不会因为字段缺失把页面渲染成白屏。注意 `with` 改变了 `.` 指向，要用 `$.Settings` 访问根上下文。

## 推荐写法：BaseTheme + gin.H

新主题强烈推荐这条路径——SEO 注入完全免费，未来 core 长出新能力（比如 og:image 兜底、per-page robots）也是零改动跟上。

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
<title>{{.Title}} - {{settingOr .Settings "site_name" "My Theme"}}</title>
{{with seoHeadFor .}}{{.}}{{else}}<meta name="description" content="{{settingOr $.Settings "site_description" "..."}}">{{end}}
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

// 2. PageService 引用 SEOBuilder + Registry + HookBus
type PageService struct {
    options    *option.Store
    seoBuilder *rewrite.SEOBuilder   // engine.SEO
    registry   *content.Registry     // engine.Registry
    hookBus    *hook.Bus             // engine.Hooks
    contentRepo *content.Repository  // engine.Content
}

// 3. 各 Get*Data 方法构建 SEO
// 下面以主题声明的 product 内容类型为例；product 不是 core 内置类型。
func (s *PageService) GetProductDetail(slug string) (*ProductDetailData, error) {
    item, _ := s.contentRepo.FindBySlugScoped(s.reqCtx, "product", slug)
    typeDef := s.registry.GetType("product")

    seo := s.seoBuilder.ForContent(item, typeDef)
    coreTheme.ApplySiteOptionOverrides(app, &seo)                            // admin site_name 覆盖
    coreTheme.ApplyContentMetaSEO(s.hookBus, s.contentRepo, &seo, item)     // ← 让 SEO 插件能 patch

    data := &ProductDetailData{ /* ... */ }
    data.SEO = seo
    return data, nil
}
```

两个 helper 必须都调：

- **`ApplySiteOptionOverrides`** — 把 admin 的 `site_name` 覆盖到 `seo.Title` / `seo.OGTitle`，并在 `seo.Description` 为空时回填 `site_description`
- **`ApplyContentMetaSEO`** — 触发 `seo.content.meta` filter 链，让 [seo-extras 插件](../plugins/seo-extras.md) 这类 per-content SEO 覆盖插件能修改 SEOMeta

`BaseTheme + gin.H` 主题完全不用关心，core 的 `renderSingle` 已经替你调好了。这又是一个倾向 BaseTheme 的理由——插件生态默认就工作。

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
| modern-company / financial-news / go-press-landing | 自定义 PageData struct | PageService 手动注入 |

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
ApplySiteOptionOverrides         （site_name / site_description 兜底）
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
