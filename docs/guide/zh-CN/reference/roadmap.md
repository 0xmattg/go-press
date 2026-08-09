# 路线图与贡献

## 已完成里程碑

- **引擎与存储基础** — 生命周期编排、PostgreSQL/GORM、迁移、带前缀的表名、
  表所有权注册、Options、Worker、缓存和结构化日志。
- **统一内容系统** — Content/Meta 模型、注册表驱动内容类型、链式查询、请求
  Scope、分类法、内容状态、定时发布、排序，以及不受主题切换影响的核心类型。
- **后台 CMS** — 数据驱动 CRUD、Quill 编辑器、媒体选择器、服务端筛选与
  分页、显示选项、菜单、重定向、缓存、邮件、主题、插件、用户、评论、系统
  设置、审计日志和 RBAC 强制校验。
- **主题运行时** — BaseTheme、配置驱动 Rewrite 与模板映射、页面 Bundle、
  回退层级、统一 FuncMap、语义化前台 Hook、主题设置、Logo、演示数据导入和
  主题热切换。
- **SEO 与公开 URL** — Canonical、Open Graph、JSON-LD、favicon、重定向、
  分类归档、动态/静态 Sitemap、语言感知内链和内容级 SEO 扩展 Hook。
- **媒体管线** — 上传元数据、响应式 JPEG/PNG 变体、可选 WebP、预加载与
  优先级 helper，以及历史变体重建。
- **插件运行时** — 可摘除 handle 的 Action/Filter、Settings Provider、受保护
  路由/中间件注册、Router 重建、缓存失效、插件自有表和运行时完整停用。
- **国际化** — Core locale Manager、后台语言、语言感知缓存与 URL、主题/
  网站设置翻译、内容与菜单翻译、跨语言同 slug，以及 Sitemap hreflang
  Transformer。
- **前台账号** — Provider-neutral 用户、身份绑定、可撤销 Session、注册策略、
  Google OIDC 与 EIP-4361 SIWE Provider，以及主题账号 helper。
- **评论与 Profile** — 登录评论、一级直接回复、审核与 RBAC、缓存失效、后台
  分页和仅当前账号可见的 Profile 契约。
- **内置运营插件** — 多语言管理、内容级 SEO 覆盖、站点代码片段和带保留
  策略、本地 GeoIP 的自托管访问统计。
- **Agent / MCP Phase 0–3** — 协议无关的 Core Agent Registry/Executor、短期
  Credential、Scope + RBAC + 所有权、幂等与审计，以及默认停用/默认只读的
  双协议 MCP 插件、6 个只读 Tool 和 6 个受控写 Tool。
- **交付工具** — 支持 Handler 热切换的 Web 安装器、`gopress` autoload/
  build 流程、Swagger 生成、站点级配置和站点级公开生成物。

## 进行中 / 计划中

- Shortcode 解析器。
- 读写数据库连接分离。
- Prometheus 监控指标。
- **Agent / MCP Phase 4** — OAuth 2.1 授权发现、PKCE、Refresh Token 轮换、
  Step-up Scope、OpenTelemetry、指标、告警和审计保留策略。
- **Agent / MCP Phase 5** — Resources、Prompts、Tasks、MCP Apps、订阅、
  Registry 发布与第三方 Tool 治理。
- CI/CD 流水线加固。
- Benchmark 与性能调优。
- 主题/插件版本迁移 Hook。
- 在线主题市场与一键安装。

## 贡献

1. Fork 本仓库。
2. 创建聚焦单一问题的特性分支。
3. 为受影响的公开契约补充测试和文档。
4. 提交聚焦的改动并推送分支。
5. 创建 Pull Request；涉及迁移、安全或兼容性时说明对应影响。

## 开源协议

[MIT License](../../../../LICENSE)
