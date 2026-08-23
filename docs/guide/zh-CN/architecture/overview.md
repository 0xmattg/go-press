# 架构总览与启动流程

## 总体架构

```

前台 Theme Dispatcher 是最终 catch-all。健康检查、静态资源、Sitemap、API、
后台、插件路由和 Swagger 都有机会先匹配，最后才把请求交给当前主题。

## CLI 与构建层

`gopress serve`、`gopress build` 和 `gopress gen` 会扫描主题/插件根目录的
manifest，并重新生成 `internal/autoload/autoload_gen.go`。因此 server 入口
始终保持通用：扩展通过自动生成的 blank import 自注册，不需要手改
`cmd/server/main.go`。

- `gopress serve` 刷新 autoload 后运行服务，并透传 flag 和关停信号。
- `gopress build` 刷新 autoload 后编译生产 server 二进制。
- `gopress gen` 只刷新 autoload，适用于 IDE 或 CI。

主题和插件会被编译进二进制，因此生产构建只包含构建时实际存在的扩展。
                ┌──────────────────────────────────────────────────────┐
                │                    HTTP 请求                         │
                └──────────────┬───────────────────────────────────────┘
                               ▼
                ┌──────────────────────────────────────────────────────┐
                │           Gin Router + 中间件链                      │
                │  Logger → Recovery → CORS → RateLimit → PageCache   │
                └──┬───────────┬───────────┬──────────┬───────────────┘
                   │           │           │          │
            ┌──────▼──┐  ┌────▼────┐  ┌───▼───┐  ┌──▼──────────────┐
            │ REST API │  │  Admin  │  │Swagger│  │ Theme Dispatcher│
            │ /api/v1  │  │ /admin  │  │ /docs │  │   NoRoute(*)    │
            └──────────┘  └─────────┘  └───────┘  └────────┬────────┘
                                                           │
                ┌──────────────────────────────────────────▼──────────┐
                │              BaseTheme 运行时引擎                    │
                │  自定义路由 → Rewrite 解析 → 动态模板映射 → SEO 注入 │
                └─────────────────────────┬───────────────────────────┘
                                          │
          ┌──────────┬──────────┬─────────┼──────────┬──────────┐
          ▼          ▼          ▼         ▼          ▼          ▼
     ┌─────────┐┌────────┐┌────────┐┌─────────┐┌────────┐┌─────────┐
     │ Content ││Taxonomy││  User  ││  Media  ││ Option ││  Menu   │
     │  Repo   ││  Repo  ││  Auth  ││  Repo   ││ Store  ││  Store  │
     └────┬────┘└───┬────┘└───┬────┘└────┬────┘└───┬────┘└────┬────┘
          └─────────┴─────────┴──────────┴─────────┴──────────┘
                                    │
                    ┌───────────────┼───────────────┐
                    ▼               ▼               ▼
              ┌──────────┐  ┌────────────┐  ┌────────────┐
              │ GORM/PG  │  │ L1 Memory  │  │ L2 Redis   │
              │(dbprefix)│  │   Cache    │  │   Cache    │
              └──────────┘  └────────────┘  └────────────┘
```

## 引擎启动流程

```
main.go
  └→ core.BuildAndBootstrap(cfg, configPath, seed)
       ├→ dbprefix.Set(cfg.PG.TablePrefix)     // 1. 设置表前缀
       ├→ postgresql.NewConnection()            // 2. 连接数据库（NamingStrategy）
       ├→ engine.Migrate()                      // 3. 自动迁移建表
       ├→ engine.SeedFromFile()                 // 4. 可选：导入种子数据
       ├→ engine.Bootstrap()                    // 5. 加载 Options/Menus/Redirects 到内存
       ├→ engine.LoadAllThemes()                // 6. 注册主题 + 激活配置主题
       │    ├→ Registry.Clear()                 //    清理旧注册
       │    ├→ registerCoreTypes()              //    恢复核心内容类型与 category/tag taxonomy
       │    ├→ LoadFileConfig(theme.toml)        //    读取主题声明的内容类型/菜单/模板映射
       │    ├→ RegisterContentTypesFromConfig()  //    按 [[content_types]] 注册主题内容类型
       │    └→ theme.Setup()                    //    主题运行时初始化（菜单位置、设置、hook）
       ├→ engine.LoadAllPlugins()               // 7. 注册插件 + 激活已启用插件
       ├→ engine.SetupAdmin()                   // 8. 后台 CMS 路由
       └→ engine.SetupRouter()                  // 9. 组装 Gin 路由
             ├→ 中间件链
             ├→ /health、/sitemap.xml
             ├→ /api/v1/* (REST API)
             ├→ /admin/* (后台 CMS)
             ├→ /swagger/* (API 文档)
             └→ NoRoute → ActiveTheme.ServeHTTP (前台)
```

主题激活会清理旧主题注册项、恢复核心内容类型、读取当前 `theme.toml`、注册
内容类型与菜单位置并执行主题 Setup。随后插件通过公开扩展点挂载 Hook、
中间件、路由和设置 Provider；core 还会协调当前主题声明的插件依赖。

## 安装器模式

当不存在已完成的站点配置时，同一个进程会进入 Web 安装器。安装器验证并
按需创建数据库、写入站点级 `config.toml`、迁移数据表、创建首个管理员，
最后原子切换到正式站点 Handler，无需手动重启。

## 引擎职责

- 统一拥有内容、分类、用户、Session、权限、媒体、菜单、选项、缓存、
  Rewrite、SEO、邮件、评论、前台内容提交、Agent 和 Worker 等稳定服务。
- 注册核心内容类型和配置驱动的主题内容类型。
- 暴露通用仓储、模板 helper、Hook、Filter、Provider、中间件扩展点及受保护
  路由 helper。
- 从同一组注册表生成公开 URL、canonical、Sitemap 和回退模板。
- 协调扩展激活、依赖校验、数据迁移与缓存失效。

## Agent 与 MCP 边界

`core/agent` 是协议无关的能力层，不包含 MCP 方法名、HTTP Handler 或特定客户端
逻辑。它维护 Tool Registry、Principal、Credential、Scope/RBAC Authorizer、
Tool Policy、Executor、JSON Schema 校验、幂等记录和 Agent 审计。引擎注册通用
站点、内容类型、内容、分类与媒体 Tool，当前主题声明的 ContentType 通过同一
Registry 自动被这些通用 Tool 识别。

默认停用的 `gopress-mcp` 插件依赖上述 Core 接口，把 `/mcp` Streamable HTTP
消息映射为 `agent.Call`，再把结构化结果映射回 MCP。它不直接调用内容仓储、
不识别主题或业务插件；Core 也不 import MCP SDK 或该插件。停用插件只移除协议
入口，不改变 Core 的领域模型。

每次 Tool 执行都遵循固定管线：

```text
刷新 Principal
  -> 校验参数 Schema
  -> Scope AND Core RBAC AND 所有权
  -> 站点 Tool Profile / 逐 Tool 策略
  -> 高风险确认
  -> 写操作幂等获取
  -> 有超时和并发上限的领域执行
  -> 结果 Schema 校验
  -> 幂等落库
  -> 强制审计
```

当前 Agent 设计、Tool 与执行细节见
[Agent 与 MCP 模块](../agent/overview.md)，网络连接与运维方法见
[GoPress MCP 插件](../plugins/gopress-mcp.md)，完整演进路线见
[路线图与贡献](../reference/roadmap.md)。

## 关键解耦点

- **核心模型保护** — 引擎在 `Registry.Clear()` 后自动 `registerCoreTypes()`；
  内容类型 `post` / `page` / `contact_message` 与 taxonomy `category` / `tag`
  跨主题切换永久保留
- **通用请求 Scope** — Content 与 Taxonomy Scope 允许扩展加入语言、租户、
  可见性或预览约束，无需把这些策略写入 core 或主题
- **主题内容模型配置化** — 主题自定义内容类型由 `theme.toml` 的 `[[content_types]]` 声明，后台菜单、CRUD、REST API、Rewrite 和模板映射统一从注册表读取
- **主题热切换** — 后台一键切主题，core 重建路由 + 刷新缓存，无需重启
- **插件热拔插** — 插件 `Activate` 时记录所有 `hook.Handle`，`Deactivate` 时按 handle 摘除，运行时即可完整下线
- **前台插槽契约** — 主题在基础布局声明 `theme.head.end` / `theme.body.open` / `theme.footer.end` / `header.nav.after`，插件只对这些稳定语义位置输出 HTML
- **零主题/插件交叉耦合** — 主题只依赖 core funcmap 字符串 key，插件只向 core 注册 hook/ctx key，**主题和插件之间不存在任何直接调用或类型依赖**，core 是唯一交汇点
- **Provider-neutral 前台认证** — core 负责用户、Identity、注册策略和可撤销 Session；Google OIDC、钱包签名等协议由独立插件验证，主题只读取统一登录上下文
- **策略驱动前台创作** — 主题声明哪些角色能够创建或维护某种内容，Core 统一执行账号状态、RBAC、所有权、编辑状态、输入校验与滥用限制，而不接管主题 UI
- **协议无关 Agent Core** — Core 只定义 Tool、Principal、执行与安全契约；
  MCP 由可选插件适配，主题和业务插件无需知道协议或客户端存在

前台账号、身份插件和主题接入详见[前台用户注册与身份登录](public-authentication.md)，登录用户创作流程详见[前台用户内容提交](public-content-submission.md)。其他主题见左侧导航中的架构章节。
