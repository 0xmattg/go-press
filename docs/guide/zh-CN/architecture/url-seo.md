# URL 与 SEO

GoPress 把 SEO 相关能力内建在引擎层，主题和插件**不需要**自己重新实现 canonical / og:* / sitemap 这些。

## URL 解析

- **Rewrite 引擎** — URL 路径 → ContentType + Slug 自动解析
- **永久链接** — 由内容类型的 `rewrite_slug` 决定 URL 结构，例如主题声明的 `product` 可以使用 `/products/:slug`，文章可以使用 `/blog/:slug`
- **模板映射** — 内容类型可通过 `templates = { archive = "...", single = "..." }` 指定归档/详情页使用哪个 `templates/pages/*.tmpl`，避免在 Go handler 中写特殊分支
- **301/302 重定向** — 数据库驱动，内存缓存，命中计数

`product` / `service` / `showcase` 都不是 core 内置假设，只是示例主题常用的业务类型。真正的 URL 行为来自当前注册表：

```toml
[[content_types]]
name = "module"
has_archive = true
rewrite_slug = "modules"
templates = { archive = "products", single = "product-detail" }
```

这个配置会产生 `/modules` 和 `/modules/{slug}`，同时复用 `products` / `product-detail` 页面模板。后台 CRUD、REST API、SEO canonical、Sitemap 和前台动态渲染都会读取同一个内容类型定义。

## XML Sitemap

- **自动生成** — 包含所有已发布内容及分类法（category/tag）URL
- **后台一键手动生成** — 「系统设置」中提供按钮触发，静态副本写入当前站点的 `public/sitemap.xml`，例如 `sites/example.com/public/sitemap.xml`
- **多语言 hreflang** — 核心 `SitemapGenerator` 暴露 `AddTransformer()` Hook 和 `xhtml:link rel="alternate"` 命名空间，多语言插件注册 transformer 后自动为每条 URL 输出 `<xhtml:link hreflang="...">` 备选链接，并把非默认语言版本作为独立 `<url>` 追加，便于 Google 识别翻译组。**主题/核心零改动**

前台 `/sitemap.xml` 仍由当前站点进程动态输出，并同时支持 `GET` 和 `HEAD`。`public/` 是站点级公开生成物目录，后续 `robots.txt`、`llms.txt` 等也应放在这里，避免多站点共用应用根目录时互相覆盖。

## SEO Meta

核心 `SEOBuilder` 为 home / archive / single 三类页面统一生成：

- `<meta description>`
- `<link rel="canonical">`
- `og:title` / `og:description` / `og:image` / `og:type`
- JSON-LD（Article / WebSite schema）

主题模板用 `{{seoHeadFor .}}` 一键渲染所有标签。详见 [主题 SEO 接入规范](../themes/seo-integration.md)。

## 统一站点信息

浏览器标题、meta description 和 favicon 都从 admin「系统设置 > 网站设置」的 `site_name` / `site_description` / `site_icon` 取（前两者对应 WordPress `blogname` / `blogdescription`），全部主题共用同一来源。

`site_icon` 非空时会优先输出 `/favicon.ico`，再输出带 `type` / `sizes` 的图片 icon 和 Apple touch icon。留空时各主题各自的兜底字符串接管，避免新装系统出现空标题。

## Per-content SEO 覆盖（插件路线）

`seo.content.meta` 过滤器允许插件在 SEO 渲染前修改单页 `SEOMeta`。配套：

- `admin.content_form.fields` — 内容编辑页 meta box 插槽
- `admin.content.saved` — 内容保存动作

三个通用 hook 组成 "WordPress + Yoast SEO" 等价模型。内置 [seo-extras 插件](../plugins/seo-extras.md) 即此模型的参考实现，激活后每条内容多出独立的 `_seo_title` / `_seo_description` / `_seo_image` / `_seo_robots` 覆盖字段。

## i18n 内链一致性

模板通过 `{{langPrefixURL .Ctx "/path"}}` 生成普通内部链接，核心 funcmap 根据当前请求语言自动补齐 `/zh`、`/ja` 等前缀，保证用户在非默认语言下点击内链不会回落到默认语言。

内容类型相关链接优先使用 Rewrite 感知 helper：

```gotemplate
{{archiveURL "module"}}
{{contentURL . "module"}}
```

`archiveURL` 返回当前内容类型归档 URL；`contentURL` 优先使用 item 上已有的 `URL` 字段，否则按 item 的 `Type` / `Slug` 和 Rewrite 注册表构造 URL，并以传入的 fallback type 兜底。
