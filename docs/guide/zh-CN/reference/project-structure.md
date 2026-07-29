# 项目结构

```
go-press/
├── cmd/
│   ├── server/main.go          # 服务启动入口
│   └── gendoc/main.go          # Swagger 文档生成工具
│
├── core/                       # ========== 引擎核心 ==========
│   ├── engine.go               # 引擎生命周期（启动/路由/关停/App 接口/registerCoreTypes）
│   ├── bootstrap.go            # BuildAndBootstrap 一键式启动编排
│   ├── migrate.go              # 数据库自动迁移（GORM AutoMigrate）
│   ├── seeder.go               # 声明式数据种子（TOML 驱动，图片自动下载 + 媒体注册）
│   ├── themes.go               # 主题注册表 + 工厂
│   ├── plugins.go              # 插件注册表 + 工厂
│   ├── handler.go              # HandlerSwitcher（安装器 ↔ 应用热切换）
│   ├── table_registry.go       # 表注册表（追踪 Core/Plugin/Theme 表归属）
│   │
│   ├── content/                # 统一内容系统
│   │   ├── content.go          #   Content + ContentMeta 模型
│   │   ├── meta.go             #   ContentMeta 键值扩展
│   │   ├── types.go            #   ContentType 注册表（ContentTypeDef + Registry + AddContentTypeToTaxonomy）
│   │   ├── query.go            #   链式查询构建器（WP_Query 风格）
│   │   ├── scope.go            #   请求级内容过滤 API（AddContentScope / ScopedDB）
│   │   └── repository.go       #   通用 CRUD（Create/Update/Delete/Find/FindBySlug）
│   │
│   ├── taxonomy/               # 分类法（Term + Taxonomy + TermRelationship）
│   ├── comment/                # 评论、一级回复、审核状态与查询仓储
│   ├── user/                   # 用户 + JWT 认证 + RBAC（角色/能力）
│   ├── mail/                   # 通用邮件对象、Sender 能力与 SMTP 投递
│   ├── i18n/                   # 核心 i18n 系统（Manager + go-i18n Bundle + T() + TranslateOption/TranslateSettings）
│   ├── option/                 # 全局设置 + Translatable 注册表（RegisterTranslatable / IsTranslatable / AllTranslatableKeys）
│   ├── menu/                   # 导航菜单（Menu + Item 树形结构 + 位置注册 + 语言 Hook）
│   ├── media/                  # 媒体库（上传/尺寸记录/响应式变体/WebP + JSON API）
│   ├── hook/                   # Hook/Filter 事件总线（WordPress 风格）
│   ├── cache/                  # L1 内存 + L2 Redis 多级缓存 + 页面缓存中间件
│   ├── worker/                 # Goroutine 工作池 + Cron 定时调度
│   ├── rewrite/                # URL 重写 + 永久链接 + SEO + Sitemap + 重定向
│   ├── api/                    # REST API（自动端点 + JWT/APIKey 双认证 + 限流 + CORS）
│   ├── installer/              # Web 安装器（DB 配置 + 站点信息 + 热切换）
│   ├── theme/                  # Theme/App 接口 + BaseTheme + 页面 bundle 加载 + FuncMap + 内置回退模板
│   ├── plugin/                 # Plugin 接口定义
│   └── admin/                  # 后台 CMS（数据驱动 CRUD + RBAC + 审计日志）
│       ├── static/css/         #   后台样式
│       ├── static/js/          #   后台 JS（Quill 编辑器 + 媒体选择器）
│       └── templates/          #   后台模板（layouts + pages）
│
├── themes/                     # ========== 主题目录 ==========
│   ├── modern-company/         #   企业官网主题（产品/服务/案例/博客）
│   │   ├── theme.go            #     主题入口 + init() 自注册
│   │   ├── theme.toml          #     主题元信息 + 内容类型 + 菜单位置/图标
│   │   ├── handlers.go         #     自定义页面处理器
│   │   ├── services.go         #     业务服务层（含 TranslateSettings 调用）
│   │   ├── functions.go        #     模板函数
│   │   ├── translatable.go     #     可翻译设置键声明（option.RegisterTranslatable）
│   │   ├── locales/            #     i18n 翻译文件（en.json, zh.json）
│   │   ├── demo/data/          #     内置演示数据（seed.toml）
│   │   ├── static/             #     CSS/JS/Images
│   │   └── templates/          #     layouts/ + partials/ + pages/ 页面模板
│   ├── atelier-slate/          #   设计工作室主题
│   ├── axis-form/              #   Axis Form 建筑设计主题
│   ├── florafi/                #   FloraFi 稳定币 / 金融科技主题
│   ├── civic-estate/           #   商业地产主题
│   ├── financial-news/         #   财经新闻门户主题
│   ├── go-press-landing/       #   SaaS Landing 主题
│   ├── mono-journal/           #   Mono Journal 个人博客主题
│   └── terra-trail/            #   户外旅行主题
│
├── plugins/                    # ========== 插件目录 ==========
│   ├── multilang/              #   WPML-like 多语言内容翻译 + 菜单翻译
│   │   ├── plugin.go           #     插件逻辑（语言检测/翻译克隆/语言切换/Content Scope/菜单翻译Hook）
│   │   ├── models.go           #     Translation/Language/StringTranslation/MenuTranslation 数据模型
│   │   ├── repository.go       #     翻译/语言/字符串/菜单翻译 CRUD
│   │   ├── register.go         #     init() 自注册
│   │   └── templates/admin/    #     语言、内容、菜单、字符串与设置翻译后台
│   ├── seo-extras/             #   Yoast-like per-content SEO 覆盖
│   │   ├── plugin.go           #     3 个 hook 实现 + meta box HTML 构造
│   │   └── register.go         #     init() 自注册
│   ├── code-snippets/          #   WPCode-like 站点级代码注入
│   ├── gopress-analytics/      #   自托管 PV/UV/访客与归属地统计
│   ├── google-identity/        #   Google OIDC 前台身份 Provider
│   └── metamask-identity/      #   EIP-4361 SIWE 钱包身份 Provider
│
├── internal/autoload/          # gopress gen 自动生成的主题/插件 blank import
├── internal/contracts/         # 跨主题公开契约测试（themes/ 根只放主题包）
│
├── sites/                      # 站点配置（Web 安装器自动生成）
│   └── localhost/              #   本地开发站点
│       ├── config.toml         #     站点配置文件
│       └── public/             #     站点级公开生成物（sitemap.xml、robots.txt、llms.txt 等）
│
├── pkg/                        # ========== 基础设施 ==========
│   ├── dbprefix/               #   表前缀工具（Set/Get/Table/PluginTable/ThemeTable）
│   ├── logger/                 #   结构化日志 (slog)
│   ├── middleware/             #   通用中间件（请求日志等）
│   └── postgresql/             #   数据库连接工厂（NamingStrategy + 连接池）
│
├── config/                     # 默认配置 + 配置解析
│   ├── config.go               #   配置结构体定义
│   ├── resolve.go              #   多站点配置发现
│   └── config.toml             #   配置模板（密钥留空，非运行时配置；实际站点用 sites/<host>/config.toml）
│
├── docs/                       # 文档
│   ├── guide/                  #   MkDocs 文字文档（你正在看的部分）
│   ├── docs.go                 #   Swagger Go 包（main.go 通过 _ "go-press/docs" 引用）
│   ├── swagger.json            #   OpenAPI 规范
│   └── swagger.yaml
│
└── uploads/                    # 上传文件目录（运行时生成）
    ├── 2026/04/                #   按年月组织的用户上传，变体与原图同目录
    │   ├── xxxx.png            #   原图，gp_media.path 指向这里
    │   ├── xxxx-480w.webp      #   响应式 WebP 变体
    │   └── xxxx-1024w.png      #   原格式 fallback 变体
    └── demo/                   #   演示数据图片
```

## Core

`core/` 是框架运行时。`engine.go`、`bootstrap.go`、`migrate.go` 和
`seeder.go` 负责启动、迁移和数据导入；`content/`、`taxonomy/`、`comment/`
和 `user/` 提供稳定领域模型；`theme/`、`plugin/` 与 `hook/` 定义公开扩展
契约；其余目录提供 Rewrite、缓存、媒体、菜单、邮件、API 和安装器等共享
服务。

## 主题

主题位于 `themes/{slug}`，根目录必须包含 `theme.toml` 与非测试 Go 源文件，
并通过 `init()` 向 core 注册。常见目录包括 `locales/`、`demo/data/`、
`static/` 及 `templates/{layouts,partials,pages}`。跨主题公开契约测试位于
`internal/contracts/`，因此 `themes/` 根目录只保留主题包。

## 插件

插件位于 `plugins/{slug}`，以 `plugin.toml` 作为 manifest。插件只能通过
core Hook、Settings Provider、受保护路由、中间件、表注册和前台语义插槽
扩展系统；插件自有表通过 `dbprefix.PluginTable` 命名，运行时不能 import
主题。

## 运行站点

`sites/{host}/config.toml` 由安装器生成或由运维维护；
`sites/{host}/public/` 保存 `sitemap.xml` 等站点级公开生成物。
`uploads/YYYY/MM/` 保存原图与响应式变体。配置、凭据、上传文件和生成物属于
运行时数据，默认不应提交到公开源码仓库。
