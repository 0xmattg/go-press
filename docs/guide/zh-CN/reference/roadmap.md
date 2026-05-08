# 路线图与贡献

## 已完成里程碑

- [x] 引擎骨架 + 数据基础 (Phase 1)
- [x] 内容系统核心 (Phase 2)
- [x] 用户 + 选项 + 菜单 (Phase 3)
- [x] Hook + Cache + Worker (Phase 4)
- [x] URL / SEO / REST API (Phase 5)
- [x] 主题引擎 + 后台 CMS (Phase 6)
- [x] modern-company + financial-news 主题 (Phase 7)
- [x] Web 安装器（引导配置 + 热切换）
- [x] BaseTheme 运行时引擎（Rewrite 解析 + 模板层级 + SEO 注入）
- [x] Admin RBAC 权限加固（全后台页面权限检查）
- [x] API 双认证（JWT + API Key）
- [x] 主题热切换路由自动重建
- [x] 数据库表前缀系统（核心/插件/主题表隔离 + 表注册表）
- [x] 富文本编辑器（Quill 2.0）+ 媒体选择器
- [x] Demo 数据导入（DemoDataProvider 接口，图片自动下载 + 媒体注册）
- [x] 核心内容类型 post/contact_message/category/tag（跨主题切换保留）
- [x] 主题内容类型配置化（`theme.toml` 的 `[[content_types]]` 驱动后台菜单、CRUD、REST API、Rewrite 和图标）
- [x] 分类归档页（跨类型聚合 + 类型标签徽章 + 主题类型过滤）
- [x] 内置回退模板（分类/单页/列表，主题未提供时自动回退）
- [x] 详情页标签展示（任意挂载 tag 的内容详情页显示关联 Tags）
- [x] Sitemap 增强（含分类法 URL + 后台一键生成按钮）
- [x] 请求级内容过滤 API（`content.AddContentScope` / `content.ScopedDB`，插件/主题完全解耦）
- [x] WPML-like 多语言插件（内容翻译 + 菜单翻译 + 语言前缀路由 + 智能语言切换 + 翻译管理后台）
- [x] 菜单管理系统（Menu + Item 树形结构 + 位置注册 + 后台可视化管理）
- [x] 菜单语言分配（按位置为每种语言分配独立菜单，通过 `menu.location.resolve` 透明切换 + URL 重写）
- [x] 插件设置系统（`SettingsProvider` / `SettingsDataProvider` / `SettingsSaveProvider` 接口）
- [x] 内容状态管理（published/draft/archived + 后台状态选择器 + 列表状态徽章）
- [x] 核心 i18n 系统（`core/i18n` Manager + go-i18n + 3 层翻译回退 + `T()` 模板函数）
- [x] 主题设置翻译（`core/option` Translatable 注册表 + `TranslateSettings` + `_opt.` 前缀 + 管理后台）
- [x] 后台内容列表过滤 Tab（`admin.HookContentListTabs` filter + `ContentListTab` 抽象，多语言插件注入语言 Tab + 计数徽章）
- [x] 内容列表拖拽排序（`sort_order` 支持类型自动启用，`POST /{slug}/reorder` 事务批量写 + 前端原生 HTML5 DnD + toast）
- [x] 插件热拔插（`hook.Handle` + `RemoveAction/RemoveFilter`、`SitemapGenerator.RemoveTransformer`、Gin 中间件 `IsActive` 自守卫，`plugin_active_<name>` option 持久化启用状态）
- [x] 前台模板 Hook 插槽（`renderHook` + `theme.head.end` / `theme.body.open` / `theme.footer.end` / `header.nav.after`，站点代码片段和多语言导航切换器均由插件 filter 注入，主题不再依赖 HTML 后处理）
- [x] WPML 同 slug 跨语言语义（`FindBySlugScoped` / `EnsureUniqueSlugScoped` + 主题 PageService 注入 `reqCtx` + multilang 克隆默认复用源 slug，例如主题声明的 `product` 可对应 SEO `/products/foo` ↔ `/zh/products/foo`）
- [x] 后台编辑页永久链接前缀注入（`admin.HookContentPermalinkPrefix` filter，例如多语言时显示 `/zh/products/foo` 区分同 slug 翻译版本）
- [x] 统一站点信息（admin「系统设置 > 网站设置」`site_name` / `site_description` 作为 WordPress `blogname` / `blogdescription` 等价物，全部主题统一来源 + 主题兜底默认）
- [x] 统一 SEO 渲染管线（`seoHeadFor` 模板助手 + `ApplySiteOptionOverrides` 兜底，全主题 `<title>` / `<meta description>` / canonical / og:* / JSON-LD 输出一致）
- [x] Per-content SEO 覆盖能力（`seo-extras` 插件 + 三个通用 hook：`admin.content_form.fields` / `admin.content.saved` / `seo.content.meta`）
- [x] WPCode-like 代码片段插件（`code-snippets` 通过三个主题插槽注入站点级 HTML/JS，无需修改主题文件）

## 进行中 / 计划中

- [ ] Shortcode 解析器
- [ ] 读写分离连接池
- [ ] Prometheus 监控指标
- [ ] CI/CD 流水线
- [ ] 压力测试 & 性能调优
- [ ] 主题/插件版本升级机制（`Migrate(fromVersion)` 钩子）
- [ ] 在线主题市场 + 一键安装

## 贡献

欢迎提交 Issue 和 Pull Request。请遵循以下规范：

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

## 开源协议

[MIT License](../../../../LICENSE)
