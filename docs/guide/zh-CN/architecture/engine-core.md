# 引擎核心

GoPress 引擎是 CMS 的运行时容器，负责装配存储、内容仓储、Rewrite、SEO
渲染、Hook、缓存、异步任务、后台与 API 路由、安装器路由，以及当前启用的
前台主题。

## 主要模块

| 模块 | 职责 |
|---|---|
| `core/engine.go` | 引擎生命周期、路由装配、优雅关停及共享 `App` 能力接口。 |
| `core/bootstrap.go` | 一站式构建与启动编排。 |
| `core/migrate.go` | 对 core 及已注册扩展表执行 GORM AutoMigrate。 |
| `core/seeder.go` | 基于 TOML 的声明式演示数据导入。 |
| `core/themes.go` | 主题注册表与工厂查找。 |
| `core/plugins.go` | 插件注册表与激活生命周期。 |
| `core/table_registry.go` | 跟踪 core、插件和主题拥有的数据表。 |

## 内容系统

- **统一模型** — `Content`、`ContentMeta` 与 `ContentType` 注册表共同驱动
  所有编辑型内容。
- **核心内容类型与分类法** — `post`、`page`、`contact_message` 是核心内容
  类型；`category`、`tag` 是核心 taxonomy，切换主题时都会保留。
- **主题类型** — 主题在 `theme.toml` 的 `[[content_types]]` 中声明自定义
  类型，core 在主题激活时统一注册。
- **前台用户提交** — 主题内容类型可以声明 `public_submission` 策略。主题激活期间，Core 提供通用的所有者范围写服务和临时的内容类型 RBAC 授权，路由与 UI 仍由主题负责。详见[前台用户内容提交](public-content-submission.md)。
- **注册表驱动行为** — 同一个 `ContentTypeDef` 同时驱动后台导航、CRUD
  表单、REST API、Rewrite、Sitemap、分类归档和 BaseTheme 归档/详情渲染。
  `rewrite_slug` 控制公开 URL，`templates = { archive = "...", single = "..." }`
  可把内容类型映射到不同名称的视觉页面 bundle。
- **链式查询** — 主题可使用共享查询构建器，例如
  `ContentQuery.Type("product").Published().Taxonomy("category", "hepa").Paginate(1, 20)`。
- **分类法系统** — 层级分类与扁平标签支持多对多关系和自动计数；主题通过
  `taxonomies = ["category", "tag"]` 挂载到内容类型。
- **Taxonomy 请求 Scope** — `taxonomy.Scope`、`AddScope`、`WithScope` 和
  `RequestContext` 允许扩展约束 term 列表、树、详情查找、引用次数及内容关系，
  core 不解释其中的 opaque key；没有 Scope 时保持原单语言行为。
- **安全分类命令服务** — 统一的事务型 `taxonomy.CommandService` 校验已注册
  taxonomy 类型、作用域内 Slug 唯一性、层级父项、提交的关系 ID 与写入边界，
  防止后台或 API 的作用域请求选择、修改其它作用域中的 term。
- **规范 term 归档** — `/category/{slug}` 与 `/tag/{slug}` 跨已注册内容
  类型聚合。类型徽标优先读取当前主题的 `content_type.<name>` locale key，
  缺失时回退到注册表 label。
- **安全过滤** — 分类归档会忽略当前主题未注册的内容类型。动态内容归档只
  接受该类型声明过的 taxonomy query；需要被索引的 term 链接应使用规范
  分类归档 URL，而不是 query 参数过滤页。

`product`、`service`、`showcase` 只是部分主题采用的命名约定，不是 core
要求。主题可以声明 `module`、`project`、`case_study` 或任意其它类型，并
获得相同的后台、API、路由和模板能力。

Core 不包含任何特定语言的 taxonomy 分支。内置多语言插件组合通用 Scope、
命令观察器、后台 Tab、SEO Filter 与 Sitemap Transformer，实现 Category 和
Tag 的独立翻译 identity。详见[内容与分类 Scope API](content-scope.md)和
[多语言插件](../plugins/multilang.md)。

## Hook 事件总线

`AddAction`、`DoAction`、`AddFilter`、`ApplyFilter` 为整个引擎生命周期提供
有优先级的扩展点；主题通过 `{{renderHook "slot.name" .}}` 暴露语义化前台
插槽。每次注册都会返回 handle，插件可在运行时停用时精确摘除 action 和
filter。详见 [Hook 系统](hooks.md)。

## 多级缓存

- **L1 内存 + 可选 L2 Redis** — 缓存 Key 包含语言维度；Redis 不可用时
  安全降级到进程内缓存。
- **标签失效** — 可按标签批量清理相关缓存。
- **整页缓存** — 中间件可在主题渲染前直接返回完整 HTML 响应。

详见 [缓存与 i18n](caching-and-i18n.md)。

## 异步任务

Worker Pool 组合 goroutine worker 与 Cron 风格调度器，用于执行不应阻塞
页面渲染的后台任务。

## Agent 能力层

`core/agent` 为 MCP、未来其他协议适配器和受控业务插件提供统一的协议无关能力
层：

- Registry 保存带 JSON 输入/输出 Schema、风险、权限、超时和并发限制的 Tool，
  注册返回可撤销 Handle。
- Credential 把高熵 Token 摘要绑定到用户或 Service Account、Scope、Audience、
  有效期和撤销状态；Executor 在每次执行前刷新主体和当前角色。
- Authorizer 使用 `Token Scope AND Core RBAC AND Ownership`，Tool Policy 再提供
  `read_only` / `safe_write` 与逐 Tool 风险上限。
- 写 Tool 强制幂等键；资源更新与状态转换使用 `expected_updated_at` 乐观锁；
  发布和回收还要求显式确认。
- Executor 统一执行 Schema、权限、策略、超时、并发、结果校验和强制审计，
  Tool Handler 无法选择跳过这些步骤。

Core 当前提供站点、内容类型、内容、分类和媒体的通用读 Tool，以及创建草稿、
安全更新、发布、回收、恢复和媒体描述更新 Tool。网络入口不在 Core；默认停用的
官方插件负责 MCP 适配。分层实现详见
[Agent 与 MCP 架构](../agent/architecture.md)，连接配置见
[GoPress MCP 插件](../plugins/gopress-mcp.md)。

## 用户与权限

Core 统一管理用户、JWT 与前台 Session、角色、Capability 和审计日志。受
保护 Handler 必须检查明确的 `resource.action` 权限，不能只判断是否存在
登录会话。主题和身份插件通过 Provider-neutral 的前台认证契约协作，不能
自行建立另一套用户或 Session 存储。

停用账号后，后台 Token 和前台 Session 都会在下一次请求立即失效。主题声明产生的前台提交能力通过授权 Handle 单独跟踪，切换主题时只撤销这些临时能力，不会移除主题激活前已经存在的 RBAC 配置。

## 媒体

媒体服务负责上传、元数据、响应式变体、可选 WebP 生成和后台媒体库。前台
主题通过共享响应式图片 helper 消费变体，不直接查询媒体表。

## 菜单

主题声明 `header`、`footer` 等命名位置。Core 负责存储菜单、构建嵌套菜单
树、通过 Rewrite 注册表解析内容关联 URL，并暴露位置解析 Hook 供语言菜单
分配等通用扩展使用。

## 全局选项

Options Repository 保存站点设置、主题设置、插件设置及主题/插件启用状态。
组件可注册需要翻译的 option key，运行时统一通过 core option 与 i18n helper
读取。

## 国际化

Core i18n Manager 加载核心、主题和插件 locale 文件，并向模板提供 `T`
helper。可选的数据库翻译可覆盖 UI 字符串、主题设置和网站设置，同时不引入
主题与插件之间的直接依赖。

## 演示数据

主题可通过 `DemoDataProvider` 暴露 TOML seed 路径。导入器负责创建内容、
Meta、分类关系和引用媒体，并记录该主题是否已经导入过演示数据。

## 数据库表前缀

所有表都使用当前站点配置的前缀。Core、插件和主题的表名 helper 还会编码
所有权，使多个 GoPress 实例可以安全共享同一个 PostgreSQL 数据库。详见
[数据库表前缀](../reference/database-prefix.md)。

## 运行时边界

GoPress 明确保持以下依赖方向：

- Core 拥有共享服务、数据模型、授权与扩展契约。
- 主题依赖 core 契约并负责呈现，不能 import 插件。
- 插件依赖 core 契约，通过 Hook、Provider、中间件、路由和设置页贡献行为，
  不能 import 主题。
- 主题可以在 `theme.toml` 声明模块级插件依赖，但运行时实现仍只能通过
  core 的通用 API 协作。
