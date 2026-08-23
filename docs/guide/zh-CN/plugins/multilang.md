# 多语言插件（WPML-like）

`multilang` 在 GoPress 上提供类似 WPML 的多语言能力，同时把语言策略留在
插件层。它支持独立内容记录、可选的 Category/Tag 翻译、分语言菜单、UI
字符串、主题设置和 core 网站设置翻译。

Core 不依赖这个插件；未使用独立 taxonomy 翻译时，插件可以停用且 core 仍可
独立工作。插件通过通用 Content Scope、Taxonomy Scope 与命令服务、Hook、
Rewrite 元数据、SEO Filter 和 Sitemap Transformer 组合功能，不 import 主题，
也不要求 core 加入特定语言分支。

## 功能概览

- 管理启用语言、默认语言、显示名称、国旗和排序。
- 把内容克隆到目标语言，并通过翻译组关联不同语言版本。
- Category 与 Tag 默认跨语言共享，也可分别启用按语言独立翻译。
- 按主题菜单位置分配翻译菜单，并把站内链接重写到当前语言。
- 翻译主题 UI 字符串、已注册的主题设置、`site_name` 和
  `site_description`。
- 生成规范语言前缀 URL、翻译感知的语言切换、SEO alternate 与 Sitemap
  `hreflang`。
- 按语言隔离页面缓存。

## 内容翻译与 Slug

每个内容译文都是独立的 `Content` 行，通过翻译组 ID（`trid`）关联。同一
Slug 只要求在各自语言 Scope 内唯一，因此两个译文可以共享干净的 Slug：

```text
Content #1 (en) /products/hepa-filter
Content #24 (zh) /zh/products/hepa-filter
         \             /
          translation group: trid 5
```

示例使用主题声明的 `product` 内容类型，并假设
`rewrite_slug = "products"`。任何已注册内容类型都遵循同一规则；插件读取
core 的内容类型与 Rewrite 注册表，不硬编码 product、service 或 showcase
路由。

## Category 与 Tag 翻译策略

第一版 taxonomy 翻译支持 core 的 `category` 和 `tag`。两种类型可以分别
配置：

| 模式 | 行为 | 兼容性 |
|---|---|---|
| **共享** | 所有语言继续使用现有 Term 与 Taxonomy 行。 | 默认模式；已有 ID、内容关系、Slug 和 URL 保持不变。 |
| **按语言独立翻译**（`translated_only`） | 每种语言使用独立 Term 与 Taxonomy 行，由插件自己的翻译组关联。 | 名称、Slug、描述和层级均可按语言不同。 |

切换策略不会改写或删除现有 taxonomy 数据。没有语言关联的历史行仍归入默认
语言，因此首次启用和默认共享模式都能向后兼容；非默认语言在独立模式下只
显示已经明确关联到该语言的行。

独立翻译保留语义关联，但 core identity 完全独立：

```text
Taxonomy #10 (en)  category/news
Taxonomy #31 (zh)  category/xinwen
          \                 /
           taxonomy translation group
```

译文可以使用不同名称、Slug、描述和父级。Category 存在层级时，应先翻译
目标语言的父分类，再翻译它的子分类。

## 分类与标签 URL

Core 的 taxonomy 基础路径保持稳定，不参与翻译；只有语言前缀和 Term Slug
发生变化：

| 语言 | Category | Tag |
|---|---|---|
| 默认英文 | `/category/news` | `/tag/security` |
| 中文 | `/zh/category/xinwen` | `/zh/tag/anquan` |

也可以使用 `/zh/tag/洁净室` 这样的本地化 Unicode Slug。ASCII Slug 更方便
跨地区输入和分享，本地语言 Slug 对读者更自然；建议站点统一选择一种规范并
长期保持，已发布 Slug 变更时配置重定向。

Slug 唯一性在当前 Taxonomy Scope 内校验。因此不同语言可以使用相同 Slug，
同一语言内的重复项仍会被拒绝。

## 后台 Taxonomy 翻译流程

1. 在**插件设置 → 语言**中至少启用两种语言。
2. 打开**翻译管理 → 分类与标签翻译**。
3. 把 Category 和/或 Tag 设为**按语言独立翻译**并保存。
4. 在 taxonomy 翻译表中，从已有 Term 创建目标语言译文；有层级时先翻译父级。
5. 沿编辑链接进入常规的**分类管理**或**标签管理**页面，选择目标语言 Tab，
   再调整名称、Slug、描述与父级。
6. 编辑内容时选择相同语言 Tab；Category/Tag 选择器只显示该语言可用项。

克隆内容时，共享 taxonomy 直接保留原关系 ID。独立翻译 taxonomy 只有在
目标语言译文存在时才映射；不存在时会省略该关系，而不会错误关联源语言。
以后补建 Term 译文时，插件会协调同一翻译组中匹配内容的关系。

## 规范 URL 与语言解析

默认语言使用无前缀规范 URL：

```text
/products/example
/category/news
```

非默认语言必须使用明确前缀：

```text
/zh/products/example
/zh/category/xinwen
```

语言偏好与内容 URL 的解析被刻意分开：

- `/zh/...` 这样的非默认语言前缀具有最高且明确的语义。
- 普通无前缀前台 URL 永远按默认语言解析，即使浏览器之前选择了其它语言。
- 只有前台根路径 `/` 会依次读取 `?lang`、语言 Cookie 和
  `Accept-Language`；若选中非默认语言，则重定向到 `/zh/` 等规范根路径。
- Admin 列表与 REST 请求可以用 `?lang=zh` 选择请求 Scope，不改变前台规范
  URL。

这样可以避免浏览器残留语言 Cookie 导致有效的默认语言链接返回 404。

在内容或独立翻译的 Category/Tag 详情页切换语言时，插件会解析目标记录及其
真实 Slug。目标译文不存在时，切换器会留在当前详情页，并且不保存不可用的
目标语言；归档页和静态路由仍可切换到对应规范前缀。

## 使用的 Core 契约

| Core 契约 | 作用 |
|---|---|
| `content.AddContentScope` | 把内容读取限制到当前语言。 |
| 带 Scope 的内容仓储方法 | 在单一语言中解析同 Slug 内容并校验写入。 |
| `taxonomy.AddScope` / `taxonomy.WithScope` | 约束 Term identity、树、引用次数、内容关系及选择器。 |
| 带 Scope 的 taxonomy 仓储方法 | 解析翻译后的 Term Slug，并渲染语言正确的归档。 |
| `taxonomy.CommandService` | 统一校验 Scope 内 Slug、父级、内容关系及事务写入。 |
| `BasePageService.ForRequest` | 把 Content 与 Taxonomy 请求 Context 一起交给主题服务。 |
| `admin.HookContentListTabs` / `admin.HookTaxonomyListTabs` | 注入语言 Tab，core 不需要理解语言含义。 |
| `admin.HookContentPermalinkPrefix` | 在编辑器永久链接中显示规范语言前缀。 |
| Taxonomy SEO Filter 与 Sitemap Transformer | 输出真实译文 URL 与 `hreflang` alternate。 |

扩展契约与写入安全规则详见[内容与分类 Scope API](../architecture/content-scope.md)。

## SEO 与 Sitemap

Alternate 链接来自真实翻译组，不通过猜测路径生成：

- 内容及独立翻译的 Category/Tag 页面使用目标语言真实 Slug 生成 canonical
  与 `hreflang`。
- `x-default` 指向默认语言规范 URL。
- 共享 taxonomy 的 Sitemap 条目会在不同语言前缀下复用同一 Term Slug。
- 缺少译文时不会输出虚假的 alternate。
- Sitemap Transformer 对生成条目应用同一套规则。

## 翻译管理

插件设置页包含：

- 语言管理；
- 内容翻译；
- 分类与标签翻译；
- 菜单翻译；
- 字符串翻译；
- 主题设置翻译；
- `site_name` 与 `site_description` 网站设置翻译；
- 基本设置与帮助。

如果主题或插件只提供一种后台 locale，设置界面会回退到该可用语言，而不是
隐藏 UI。

## 插件数据表

| 表名 | 用途 |
|---|---|
| `gp_plgn_multilang_translations` | 内容翻译组与语言关联。 |
| `gp_plgn_multilang_languages` | 已启用语言、默认标记及显示排序。 |
| `gp_plgn_multilang_string_translations` | UI 字符串及主题/网站设置覆盖。 |
| `gp_plgn_multilang_menu_translations` | 菜单翻译组与语言关联。 |
| `gp_plgn_multilang_taxonomy_translation_groups` | Category/Tag 翻译组 identity。 |
| `gp_plgn_multilang_taxonomy_translations` | Taxonomy 行、语言与源语言关联。 |

## 模板 Helper 与导航

```gotemplate
{{T .Ctx "welcome"}}
{{currentLang .Ctx}}
{{langPrefixURL .Ctx "/about"}}
{{archiveURL "product"}}
{{contentURL . "product"}}
{{taxonomyURL "category" .Slug}}
{{renderHook "header.nav.after" .}}
```

语言切换器通过 `header.nav.after` 贡献。主题应把这个通用插槽放在主导航内，
并在移动端支持嵌套扩展菜单；主题不能检查插件 class、数据表或私有 Option
Key。

BaseTheme 主题会自动获得已翻译的网站设置和 taxonomy URL。自定义服务应通过
`BasePageService.ForRequest(c)` 克隆请求服务，并使用带 Scope 的仓储。完全
手写 `SEOMeta` 的主题可参考[主题 SEO 接入规范](../themes/seo-integration.md)。

## 菜单与 i18n 解析

Core 解析菜单位置后，插件通过 `menu.location.resolve` Filter 找到翻译组内
当前语言菜单，克隆菜单、为站内 URL 添加前缀，并解析翻译内容链接。主题仍
只调用 `menuByLocation "header"`。

UI 字符串依次从数据库覆盖（`domain="theme"`）、组件 locale 文件和原始
Message ID 解析。可翻译主题/网站设置使用 `domain="option"` 与 `_opt.`
命名空间。详见[缓存与 i18n](../architecture/caching-and-i18n.md)。

## 兼容性与运维安全

- 默认共享模式保持现有 taxonomy 行为。
- 启用插件不会自动复制、迁移、重命名或删除 Term。
- 某种 taxonomy 已存在独立翻译记录时，禁止直接切回共享模式。
- 存在 taxonomy 翻译记录时，插件会保护性阻止停用，避免独立 identity
  静默落入含义不明确的共享命名空间。
- 大规模调整既有 taxonomy 翻译结构前应先备份数据库。

这些保护用于维护数据结构，不替代权限校验。插件设置和翻译写入仍通过 core
后台认证及对应的 RBAC capability。
