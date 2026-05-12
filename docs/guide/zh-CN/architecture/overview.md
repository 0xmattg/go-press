# 架构总览与启动流程

## 总体架构

```
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
       │    ├→ registerCoreTypes()              //    重注册 post/contact_message/category/tag（核心类型不丢失）
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

## 关键解耦点

- **核心类型保护** — 引擎在 `Registry.Clear()` 后自动 `registerCoreTypes()`，`post` / `contact_message` / `category` / `tag` 跨主题切换永久保留
- **主题内容模型配置化** — 主题自定义内容类型由 `theme.toml` 的 `[[content_types]]` 声明，后台菜单、CRUD、REST API、Rewrite 和模板映射统一从注册表读取
- **主题热切换** — 后台一键切主题，core 重建路由 + 刷新缓存，无需重启
- **插件热拔插** — 插件 `Activate` 时记录所有 `hook.Handle`，`Deactivate` 时按 handle 摘除，运行时即可完整下线
- **前台插槽契约** — 主题在基础布局声明 `theme.head.end` / `theme.body.open` / `theme.footer.end` / `header.nav.after`，插件只对这些稳定语义位置输出 HTML
- **零主题/插件交叉耦合** — 主题只依赖 core funcmap 字符串 key，插件只向 core 注册 hook/ctx key，**主题和插件之间不存在任何直接调用或类型依赖**，core 是唯一交汇点

详细分主题见左侧导航的其他架构章节。
