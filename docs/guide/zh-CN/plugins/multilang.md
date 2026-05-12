# 多语言插件 (WPML-like)

GoPress 内置一个完整的 WPML 风格多语言插件，支持独立内容翻译和语言前缀 URL 路由。

本页 URL 示例以主题声明的 `product` 内容类型为例，并假设它的 `rewrite_slug = "products"`。`product` 不是 core 内置类型，core 只要求它已通过当前主题的 `theme.toml` 注册；`module`、`project`、`case_study` 等任意内容类型都走同一套 Rewrite 注册表。

## 架构设计

```
┌─────────────────────────────────────────────────────────────────┐
│  Content #1 (en)          Content #24 (zh)                      │
│  "HEPA Filters"           "HEPA 高效过滤器"                     │
│  /products/hepa-filters   /zh/products/hepa-filters             │
│       │                         │                                │
│       └──── trid = 5 ───────────┘   (翻译组关联)                 │
│                                                                  │
│  gp_plgn_multilang_translations                                  │
│  ┌────┬──────┬────────────┬───────────────┐                      │
│  │ id │ trid │ content_id │ language_code │                      │
│  ├────┼──────┼────────────┼───────────────┤                      │
│  │  1 │    5 │          1 │ en            │                      │
│  │  2 │    5 │         24 │ zh            │                      │
│  └────┴──────┴────────────┴───────────────┘                      │
└─────────────────────────────────────────────────────────────────┘
```

## WPML 同 slug 语义（per-language slug uniqueness）

GoPress 把"slug 全局唯一"放宽到"**每种语言内唯一**"，与 WPML 行为对齐——同一个产品内容的英文版和中文版可以共享干净的 slug `hepa-filters`，仅靠 URL 的语言前缀区分，对 SEO 友好（`hreflang` 翻译组完美对应）。

实现机制（**插件几乎零改动，全靠 core scope 通道**）：

| 组件 | 角色 |
|---|---|
| `content.AddContentScope(c, fn)` | multilang 中间件按当前请求语言注入 `WHERE id IN (SELECT content_id FROM translations WHERE language_code=?)` |
| `Repository.FindBySlugScoped(ctx, type, slug)` | core 层提供的"按 scope 解析 slug"的通用方法，BaseTheme / API / 主题 PageService 全部走这一条 |
| `Repository.EnsureUniqueSlugScoped(ctx, ...)` | 保存内容时唯一性检查也走 scope；在「翻译克隆」上下文中，目标语言 scope 提前注入，所以默认会复用源 slug 而不再加 `-zh` 后缀 |
| `PageService.reqCtx` | 主题 `ForRequest(c)` 把 ctx 存到克隆服务上，详情页 `Get*Detail` 用 `FindBySlugScoped(s.reqCtx, ...)` —— **修复了"主题用自己的 contentRepo 绕过 scope"的细节坑** |
| `admin.HookContentPermalinkPrefix` | 内容编辑页永久链接展示自动加 `/zh` 前缀，运营一眼区分语言版本 |

未启用 multilang 时所有 `*Scoped` 方法行为退化为原 `*` 方法（`ScopedDB(nil, db)` 直接返回 db），单语言场景零开销、零行为变化。

## 核心功能

| 功能 | 说明 |
|------|------|
| **内容翻译** | 每个语言版本是独立的 Content 记录，通过 `trid`（翻译组 ID）关联 |
| **菜单翻译** | 按菜单位置分配不同语言的菜单，切换语言时自动显示对应菜单（含 URL 重写） |
| **语言前缀路由** | 默认语言无前缀 `/products/hepa`，其他语言 `/zh/products/hepa` |
| **语言检测** | URL 前缀 → Cookie → Accept-Language → 默认语言，优先级依次降低 |
| **前端语言过滤** | 通过 core `Content Scope API` 自动过滤，主题无需适配 |
| **语言切换器** | multilang 注册 `header.nav.after` filter，主题在导航列表尾部通过 `{{renderHook "header.nav.after" .}}` 声明位置；点击自动跳转到对应翻译页 |
| **智能跳转** | 切换语言时自动解析当前页的翻译内容 slug，跳转到正确 URL；如果详情页没有目标语言译文，不会硬拼一个必然 404 的 `/zh/...` URL，而是停留在当前页并不写入目标语言 cookie |
| **翻译克隆** | 后台一键克隆内容到目标语言（标题/正文/Meta/排序/图片全部继承） |
| **翻译管理** | 后台设置页：语言管理、翻译管理（内容翻译 + 菜单语言分配）、基本设置、使用帮助 |
| **i18n 字符串翻译** | Core `i18n.Manager` + go-i18n 引擎，3 层回退：DB `StringTranslation`(domain="theme") → 主题 locale 文件 → message ID。模板中 `{{T .Ctx "welcome"}}`，后台「字符串翻译管理」可视化编辑 |
| **主题设置翻译** | Core `option.RegisterTranslatable()` 注册可翻译设置键，`TranslateSettings()` 自动翻译。DB `StringTranslation`(domain="option") 存储，`_opt.` 前缀防碰撞。主题和插件完全解耦 |
| **缓存隔离** | 缓存 Key 包含语言维度，不同语言的页面缓存独立 |

## 插件数据表

| 表名 | 用途 |
|------|------|
| `gp_plgn_multilang_translations` | 内容翻译关联（trid → content_id → language_code） |
| `gp_plgn_multilang_languages` | 启用的语言列表（code/name/flag/default/sort/active） |
| `gp_plgn_multilang_string_translations` | 字符串翻译（domain: `"theme"` = UI 字符串, `"option"` = 主题设置翻译；name/language_code/value） |
| `gp_plgn_multilang_menu_translations` | 菜单翻译关联（trid → menu_id → language_code） |

## URL 路由示例

| 语言 | URL 格式 |
|------|---------|
| English (默认) | `/products/hepa-filters` |
| 中文 | `/zh/products/hepa-filters` |
| 日本語 | `/ja/products/hepa-filters` |

## 模板函数

```go
// 翻译 UI 字符串
{{T .Ctx "welcome"}}

// 获取当前语言代码
{{currentLang .Ctx}}

// 生成带语言前缀的 URL
{{langPrefixURL .Ctx "/products/hepa-filters"}}

// 生成内容类型归档和详情 URL，读取 core Rewrite 注册表
{{archiveURL "product"}}
{{contentURL . "product"}}

// 渲染前台导航扩展插槽（multilang 启用时会贡献语言切换器）
{{renderHook "header.nav.after" .}}
```

## 菜单翻译实现细节

multilang 插件注册 `menu.location.resolve` filter + `menu.deleted` action，实现透明的语言菜单切换，**主题和模板代码零修改**。core/menu 只知道"菜单位置解析"和"菜单删除"这两个通用扩展点，不包含任何多语言专用接口：

```
请求 /zh/products
  → 中间件设置 goroutine 级语言: menu.SetRequestLang("zh")
  → 模板调用 menuByLocation "header"
    → Store.GetByLocation("header") → 取到 header 位置的菜单
    → menu.location.resolve filter 触发:
        1. 查翻译表确定当前菜单的实际语言
        2. 通过 trid 找到 zh 语言对应的菜单
        3. 从 menusById 缓存取出翻译菜单
        4. 克隆 + URL 重写（本地链接加 /zh 前缀，内容关联项解析翻译版 slug）
    → 返回中文菜单（含重写后的 URL）
```

后台「翻译管理 → 菜单翻译」按主题注册的菜单位置展示，每个位置每种语言一个下拉框，一键保存分配：

```
📍 header (顶部导航)
  🇬🇧 English:  [main-header ▾]
  🇨🇳 中文:      [main-header-zh ▾]

📍 footer (底部导航)
  🇬🇧 English:  [-- 未分配 -- ▾]
  🇨🇳 中文:      [-- 未分配 -- ▾]

[保存菜单分配]
```

## i18n 数据流

详见 [缓存与 i18n](../architecture/caching-and-i18n.md) 中的「核心 i18n 架构」段。
