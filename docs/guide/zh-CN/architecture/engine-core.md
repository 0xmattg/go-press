# 引擎核心

GoPress 的引擎核心提供了所有 CMS 功能赖以运行的基础能力。

## 内容系统

- **统一内容模型** — `Content` + `ContentMeta` + `ContentType` 注册表，一套模型驱动所有内容类型
- **核心内容类型** — `post`（文章）、`contact_message`（联系留言）、`category`（分类）、`tag`（标签）在引擎层注册，主题切换后仍保留，不丢失数据
- **主题内容类型** — 主题在 `theme.toml` 的 `[[content_types]]` 中声明自定义内容类型，core 激活主题时自动注册
- **链式查询构建器** — 以主题声明的 `product` 内容类型为例：`ContentQuery.Type("product").Published().Taxonomy("category", "hepa").Paginate(1, 20)`
- **分类法系统** — 支持层级分类和标签，多对多关联，自动计数；主题内容类型通过 `theme.toml` 的 `taxonomies = ["category", "tag"]` 挂载核心分类法
- **分类归档页** — `/category/{slug}` 和 `/tag/{slug}` 跨内容类型聚合展示，类型标签来自当前注册的 `ContentTypeDef`
- **分类法类型过滤** — 归档页自动过滤当前主题未注册的内容类型，主题切换后仅显示有效内容

## Hook 事件总线

`AddAction` / `DoAction` / `AddFilter` / `ApplyFilter`，插件可拦截生命周期；主题模板通过 `{{renderHook "slot.name" .}}` 暴露前台扩展插槽。详见 [Hook 系统](hooks.md)。

## 多级缓存

- **L1 进程内内存缓存 + L2 Redis** — 缓存 Key 自动包含语言维度，按标签批量失效
- **整页缓存** — 中间件级别缓存完整 HTML 响应，命中时 < 1ms 返回
- 详见 [缓存与 i18n](caching-and-i18n.md)

## 异步任务

- **Worker Pool** — Goroutine 工作池 + Cron 定时调度器，异步处理后台任务

## 用户与权限

- **用户系统** — JWT 认证 + Session，RBAC 角色权限（admin/editor/author/subscriber）

## 媒体库

- **文件上传 + 响应式变体** — 上传时自动生成 thumb/480w/768w/1024w/1440w，配合 WebP 优先输出
- 详见 [媒体变体管线](../themes/media-variants.md)

## 导航菜单

- **Menu + Item 树形结构** — 多菜单位置注册（header/footer 等），后台可视化拖拽管理
- 详见 [菜单管理](../admin/menus.md)

## 全局设置

- **启动时加载到内存** — `Options.Get()` 零数据库查询
- admin「系统设置 > 网站设置」`site_name` / `site_description` 是全主题统一的"WordPress blogname / blogdescription"等价物

## 国际化（i18n）

- **核心 i18n 系统** — `core/i18n` Manager + go-i18n 引擎，`T()` 模板函数，3 层翻译回退（DB → locale 文件 → message ID）
- **Translatable 注册表** — `core/option` 提供 `RegisterTranslatable`，主题设置项无耦合翻译
- `current_lang` 作为 ctx key 由核心统一持有，`langPrefixURL` / `currentLang` / `T` / `renderHook` 均走 `BaseFuncMap` 下发，主题和插件对接点只有字符串 key，无类型耦合
- 详见 [缓存与 i18n](caching-and-i18n.md) 中的 i18n 段

## Demo 数据

- **DemoDataProvider 接口** — 主题实现该接口，后台一键导入演示数据（含远程图片下载 + 媒体库注册）

## 数据库表前缀

借鉴 WordPress 的 `wp_` 前缀机制，GoPress 内置完整的表前缀系统：

- **可配置前缀** — 默认 `gp_`，通过 `pg.table_prefix` 自定义，Web 安装器中可设置
- **核心表** — `gp_contents`、`gp_users`、`gp_options`、`gp_media_variants` 等
- **插件表** — 带 `plgn_` 中缀隔离：`gp_plgn_multilang_translations`
- **主题表** — 带 `thm_` 中缀隔离：`gp_thm_financial-news_tickers`
- **表注册表** — `core.RegisterPluginTable()` / `core.RegisterThemeTable()` 追踪所有表的归属，支持按 Owner 查询
- **双重保障** — Model `TableName()` + GORM `NamingStrategy` 双重机制确保表名正确

详见 [数据库表前缀](../reference/database-prefix.md)。
