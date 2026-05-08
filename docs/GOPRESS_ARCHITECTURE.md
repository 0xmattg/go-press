# GoPress 架构设计与开发规划

> GoPress 是一个用 Go 编写的内容管理框架与 CMS 引擎，面向需要自托管、主题化、插件化和 API 扩展能力的网站与内容应用。
> 它将内容模型、后台管理、主题模板、插件扩展、SEO、媒体处理和 REST API 组织为一套可组合的工程框架。
> 适合用于企业官网、内容站、产品展示站、文档站，以及需要在 Go 技术栈中保留 CMS 编辑体验的定制项目。

---

## 一、项目定位

### 1.1 核心理念

| 维度 | WordPress (PHP) | GoPress (Go) |
|------|-----------------|--------------|
| 运行方式 | PHP-FPM / Web Server 组合，围绕请求生命周期运行 | Go 单进程服务，适合常驻内存模型 |
| 扩展方式 | 主题与插件生态成熟，运行时动态加载灵活 | Go 接口与 Hook 注册，强调类型安全和可维护性 |
| 缓存策略 | 通常通过插件、对象缓存或反向代理组合增强 | 内置内存 / Redis / 数据库多级缓存路径 |
| 定时任务 | 常见方案包括 WP-Cron 或系统 Cron | 由服务进程内的调度器执行 |
| 实时通信 | 通常按项目组合 WebSocket / SSE 方案 | 可在同一 Go 服务中集成 WebSocket / SSE |
| 部署形态 | Web Server、PHP 运行时、数据库等多组件协作 | 编译后以单一服务进程交付，外接数据库与可选 Redis |

### 1.2 设计原则

1. **Content as First-Class Citizen** — 一切皆内容，统一 Content + Meta 模型
2. **Theme-Engine Separation** — 引擎与主题彻底解耦，主题可热切换
3. **Plugin via Interface** — 插件通过 Go 接口注册，类型安全
4. **Cache by Default** — 缓存不是优化手段，而是架构基础
5. **SEO Native** — URL 管理、结构化数据、Sitemap 内建于引擎层
6. **API First** — 每个 ContentType 自动暴露 REST API

---

## 二、目录架构

```
go-press/
├── cmd/
│   └── server/main.go                 # 启动入口
│
├── core/                              # ========== 引擎核心 ==========
│   ├── engine.go                      # 引擎生命周期管理
│   │
│   ├── content/                       # 内容系统
│   │   ├── content.go                 # Content 统一模型
│   │   ├── meta.go                    # ContentMeta 键值扩展
│   │   ├── types.go                   # ContentType 注册表
│   │   ├── query.go                   # ContentQuery 链式查询构建器
│   │   ├── repository.go             # 通用 CRUD
│   │   └── shortcode.go              # Shortcode 解析器
│   │
│   ├── taxonomy/                      # 分类法系统
│   │   ├── taxonomy.go               # Term + Taxonomy 模型
│   │   └── repository.go
│   │
│   ├── rewrite/                       # URL / SEO 路由引擎
│   │   ├── rewrite.go                 # 路由规则表 + 重写引擎
│   │   ├── permalink.go              # 永久链接结构
│   │   ├── sitemap.go                # XML Sitemap 生成
│   │   ├── redirect.go              # 301/302 重定向管理
│   │   └── seo.go                    # canonical, robots, meta, JSON-LD
│   │
│   ├── api/                           # REST API 引擎
│   │   ├── api.go                     # 自动 RESTful 端点
│   │   ├── auth.go                   # API 认证 (JWT Bearer / API Key)
│   │   ├── response.go              # 标准化响应
│   │   └── middleware.go             # 限流 / CORS / 版本控制
│   │
│   ├── cache/                         # 多级缓存系统
│   │   ├── cache.go                   # Cache 接口
│   │   ├── memory.go                 # L1: 进程内 LRU (ristretto)
│   │   ├── redis.go                  # L2: Redis 分布式缓存
│   │   ├── page.go                   # 整页缓存
│   │   └── fragment.go              # 片段缓存
│   │
│   ├── media/                         # 媒体库
│   │   ├── media.go                  # 模型
│   │   ├── repository.go
│   │   └── image.go                  # 缩略图 / WebP / 裁剪
│   │
│   ├── user/                          # 用户系统
│   │   ├── user.go                   # 模型
│   │   ├── auth.go                   # JWT + Session 认证
│   │   ├── rbac.go                   # 角色权限
│   │   └── repository.go
│   │
│   ├── option/                        # 全局设置
│   │   └── option.go                 # 启动加载到内存，Get() 零查询
│   │
│   ├── menu/                          # 导航菜单
│   │   └── menu.go                   # 菜单模型 + 内存缓存
│   │
│   ├── hook/                          # Hook / Filter 事件总线
│   │   └── hook.go                   # AddAction / DoAction / AddFilter / ApplyFilter
│   │
│   ├── theme/                         # 主题引擎
│   │   ├── theme.go                  # Theme 接口
│   │   ├── engine.go                 # 主题加载 / 模板编译
│   │   └── hierarchy.go             # 模板层级回退
│   │
│   ├── plugin/                        # 插件引擎
│   │   └── plugin.go                 # Plugin 接口
│   │
│   ├── worker/                        # 异步任务系统
│   │   ├── pool.go                   # Goroutine Worker Pool
│   │   ├── scheduler.go             # 定时调度 (cron 表达式)
│   │   └── task.go                   # Task 接口
│   │
│   └── admin/                         # 后台 CMS (数据驱动)
│       ├── handler.go                # 根据 ContentType 动态渲染
│       ├── routes.go
│       └── api.go                    # 后台 AJAX
│
├── pkg/                               # 基础设施
│   ├── database/
│   │   ├── postgres.go               # 连接工厂
│   │   ├── pool.go                   # 读写分离连接池
│   │   └── migrate.go               # 迁移引擎
│   ├── redis/
│   │   └── redis.go
│   ├── logger/
│   │   └── logger.go                # 结构化日志 (slog)
│   └── i18n/
│       └── i18n.go                   # 国际化
│
├── config/
│   ├── config.go
│   └── default.toml
│
├── themes/                            # 主题目录
│   └── modern-company/               # 企业官网主题
│       ├── theme.go
│       ├── theme.toml
│       ├── functions.go
│       ├── handlers.go
│       ├── templates/
│       ├── static/
│       └── screenshot.png
│
├── plugins/                           # 插件目录
│   ├── contact-form/
│   └── seo-tools/
│
├── content/
│   └── uploads/
│
└── sites/
    └── go-press/
        ├── config.toml
        └── seed.toml
```

---

## 三、核心接口设计

### 3.1 Content 统一模型

```go
// core/content/content.go
type Content struct {
    ID            uint
    Type          string     // "post", "page", "product", "service" ...
    Status        string     // "draft", "published", "archived", "trash"
    Title         string
    Slug          string
    Content       string
    Excerpt       string
    ImageURL      string
    AuthorID      uint
    ParentID      *uint
    SortOrder     int
    CommentStatus string
    PublishedAt   *time.Time
    CreatedAt     time.Time
    UpdatedAt     time.Time
    DeletedAt     gorm.DeletedAt

    // 关联
    Author     *user.User
    Meta       []ContentMeta
    Taxonomies []taxonomy.Taxonomy
}
```

### 3.2 ContentType 注册

```go
// core/content/types.go
type ContentTypeDef struct {
    Name        string           // "product"
    Label       string           // "产品"
    LabelPlural string           // "产品列表"
    Supports    []string         // ["title","content","excerpt","thumbnail","meta"]
    MetaFields  []MetaFieldDef   // 该类型的扩展字段定义
    Taxonomies  []string         // ["product_cat", "tag"]
    HasArchive  bool             // 是否有列表页
    Rewrite     RewriteRule      // URL 规则
    MenuIcon    string           // 后台菜单图标
    MenuOrder   int              // 后台菜单排序
}

type MetaFieldDef struct {
    Key      string   // "client"
    Label    string   // "客户名称"
    Type     string   // "string", "int", "bool", "text", "select", "image"
    Default  string
    Options  []string // Type=select 时的选项
    Required bool
}

type Registry struct {
    types      map[string]*ContentTypeDef
    taxonomies map[string]*TaxonomyDef
}

func (r *Registry) RegisterType(def ContentTypeDef)
func (r *Registry) RegisterTaxonomy(def TaxonomyDef)
func (r *Registry) GetType(name string) *ContentTypeDef
func (r *Registry) AllTypes() []*ContentTypeDef
```

### 3.3 ContentQuery: 链式查询

```go
// core/content/query.go — 类似 WP_Query
type ContentQuery struct {
    db *gorm.DB
}

// 用法示例:
// query.Type("product").Status("published").OrderBy("sort_order", "ASC").Limit(10).Get()
// query.Type("post").Taxonomy("category", "news").Paginate(page, 20)
// query.Type("showcase").Meta("location", "Shanghai").Get()

func (q *ContentQuery) Type(t string) *ContentQuery
func (q *ContentQuery) Status(s string) *ContentQuery
func (q *ContentQuery) Taxonomy(taxonomy, termSlug string) *ContentQuery
func (q *ContentQuery) Meta(key, value string) *ContentQuery
func (q *ContentQuery) Author(id uint) *ContentQuery
func (q *ContentQuery) Parent(id uint) *ContentQuery
func (q *ContentQuery) Search(keyword string) *ContentQuery
func (q *ContentQuery) OrderBy(field, dir string) *ContentQuery
func (q *ContentQuery) Limit(n int) *ContentQuery
func (q *ContentQuery) Offset(n int) *ContentQuery
func (q *ContentQuery) Paginate(page, perPage int) (*PaginatedResult, error)
func (q *ContentQuery) Get() ([]Content, error)
func (q *ContentQuery) First() (*Content, error)
func (q *ContentQuery) Count() (int64, error)
```

### 3.4 Theme 接口

```go
// core/theme/theme.go
type Theme interface {
    // 元信息
    Name() string
    Version() string
    Description() string

    // 生命周期
    Setup(app *engine.Engine)       // 注册 ContentType、Taxonomy、菜单位置等
    Routes(r *gin.Engine)           // 注册前端路由

    // 模板
    TemplateFuncs() template.FuncMap
    TemplateDir() string
    StaticDir() string
}
```

### 3.5 Plugin 接口

```go
// core/plugin/plugin.go
type Plugin interface {
    Name() string
    Version() string
    Activate(app *engine.Engine)
    Deactivate(app *engine.Engine)
}
```

### 3.6 Hook 系统

```go
// core/hook/hook.go
type Bus struct {
    actions map[string][]ActionFunc
    filters map[string][]FilterFunc
}

type ActionFunc func(ctx context.Context, args ...interface{})
type FilterFunc func(value interface{}, args ...interface{}) interface{}

func (b *Bus) AddAction(name string, fn ActionFunc, priority int)
func (b *Bus) DoAction(ctx context.Context, name string, args ...interface{})
func (b *Bus) AddFilter(name string, fn FilterFunc, priority int)
func (b *Bus) ApplyFilter(name string, value interface{}, args ...interface{}) interface{}

// Hook 点示例:
// "content.before_save"   — 内容保存前
// "content.after_save"    — 内容保存后
// "content.before_delete" — 内容删除前
// "the_content"           — 内容渲染时 (可注入 shortcode 解析)
// "the_title"             — 标题渲染时
// "template.head"         — <head> 区域注入
// "template.footer"       — </body> 前注入
// "admin.menu"            — 后台菜单注册
// "api.response"          — API 响应前处理
```

### 3.7 Cache 接口

```go
// core/cache/cache.go
type Cache interface {
    Get(key string) (interface{}, bool)
    Set(key string, value interface{}, ttl time.Duration)
    Delete(key string)
    DeleteByTag(tag string)       // 按标签批量失效
    Flush()
}

type Manager struct {
    L1 Cache   // 进程内 LRU (ristretto)
    L2 Cache   // Redis
}

// Get 流程: L1 命中 → 返回 | L1 未命中 → 查 L2 → 命中则写入 L1 并返回 | 都未命中 → 返回 nil
func (m *Manager) Get(key string) (interface{}, bool)
func (m *Manager) Set(key string, value interface{}, ttl time.Duration, tags ...string)
```

### 3.8 模板层级回退 (Template Hierarchy)

```go
// core/theme/hierarchy.go
// 请求 /products/air-shower → ContentType="product", Slug="air-shower"
// 模板查找顺序:
//   1. single-product-air-shower.tmpl   (精确到 slug)
//   2. single-product.tmpl              (精确到 type)
//   3. single.tmpl                     (通用单内容)
//   4. index.tmpl                      (终极回退)
//
// 请求 /products/ → ContentType="product", archive
// 模板查找顺序:
//   1. archive-product.tmpl
//   2. archive.tmpl
//   3. index.tmpl

func ResolveTemplate(contentType, slug, pageTemplate string, isArchive bool) []string
```

### 3.9 Engine 主引擎

```go
// core/engine.go
type Engine struct {
    Config    *config.Config
    DB        *gorm.DB
    Cache     *cache.Manager
    Hooks     *hook.Bus
    Registry  *content.Registry
    Options   *option.Store       // 全局选项 (内存)
    Menus     *menu.Store         // 菜单 (内存)
    Theme     theme.Theme
    Plugins   []plugin.Plugin
    Workers   *worker.Pool
    Router    *gin.Engine
}

func New(cfg *config.Config) *Engine
func (e *Engine) LoadTheme(t theme.Theme)
func (e *Engine) LoadPlugin(p plugin.Plugin)
func (e *Engine) Start() error
func (e *Engine) Shutdown(ctx context.Context) error
```

---

## 四、数据库 Schema

### 4.1 核心内容表

```sql
CREATE TABLE contents (
    id             BIGSERIAL PRIMARY KEY,
    type           VARCHAR(50)  NOT NULL DEFAULT 'post',
    status         VARCHAR(20)  NOT NULL DEFAULT 'draft',
    title          VARCHAR(500) NOT NULL,
    slug           VARCHAR(500) NOT NULL,
    content        TEXT,
    excerpt        TEXT,
    image_url      VARCHAR(500),
    author_id      BIGINT REFERENCES users(id),
    parent_id      BIGINT REFERENCES contents(id),
    sort_order     INT DEFAULT 0,
    comment_status VARCHAR(20) DEFAULT 'open',
    published_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ,
    UNIQUE(type, slug)
);

CREATE INDEX idx_contents_type_status
    ON contents(type, status, published_at DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_contents_slug
    ON contents(slug) WHERE deleted_at IS NULL;
CREATE INDEX idx_contents_author ON contents(author_id);
CREATE INDEX idx_contents_parent ON contents(parent_id);
```

### 4.2 内容扩展属性

```sql
CREATE TABLE content_meta (
    id         BIGSERIAL PRIMARY KEY,
    content_id BIGINT NOT NULL REFERENCES contents(id) ON DELETE CASCADE,
    meta_key   VARCHAR(255) NOT NULL,
    meta_value TEXT,
    UNIQUE(content_id, meta_key)
);

CREATE INDEX idx_content_meta_key
    ON content_meta(meta_key, meta_value)
    WHERE LENGTH(meta_value) < 256;
```

### 4.3 分类法系统

```sql
CREATE TABLE terms (
    id   BIGSERIAL PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    slug VARCHAR(200) NOT NULL UNIQUE
);

CREATE TABLE taxonomies (
    id          BIGSERIAL PRIMARY KEY,
    term_id     BIGINT NOT NULL REFERENCES terms(id) ON DELETE CASCADE,
    taxonomy    VARCHAR(50)  NOT NULL,
    description TEXT,
    parent_id   BIGINT REFERENCES taxonomies(id),
    count       INT DEFAULT 0,
    UNIQUE(term_id, taxonomy)
);
CREATE INDEX idx_taxonomies_type ON taxonomies(taxonomy);

CREATE TABLE term_relationships (
    content_id  BIGINT NOT NULL REFERENCES contents(id) ON DELETE CASCADE,
    taxonomy_id BIGINT NOT NULL REFERENCES taxonomies(id) ON DELETE CASCADE,
    sort_order  INT DEFAULT 0,
    PRIMARY KEY(content_id, taxonomy_id)
);
CREATE INDEX idx_term_rel_taxonomy ON term_relationships(taxonomy_id);
```

### 4.4 用户系统

```sql
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    username      VARCHAR(50)  NOT NULL UNIQUE,
    email         VARCHAR(200) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    display_name  VARCHAR(100),
    avatar_url    VARCHAR(500),
    role          VARCHAR(30)  NOT NULL DEFAULT 'subscriber',
    is_active     BOOLEAN DEFAULT true,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ
);

CREATE TABLE user_meta (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    meta_key   VARCHAR(255) NOT NULL,
    meta_value TEXT,
    UNIQUE(user_id, meta_key)
);
```

### 4.5 全局设置

```sql
CREATE TABLE options (
    id       BIGSERIAL PRIMARY KEY,
    name     VARCHAR(200) NOT NULL UNIQUE,
    value    TEXT,
    autoload BOOLEAN DEFAULT true
);
CREATE INDEX idx_options_autoload ON options(autoload) WHERE autoload = true;
```

### 4.6 导航菜单

```sql
CREATE TABLE menus (
    id       BIGSERIAL PRIMARY KEY,
    name     VARCHAR(100) NOT NULL,
    location VARCHAR(50)
);

CREATE TABLE menu_items (
    id         BIGSERIAL PRIMARY KEY,
    menu_id    BIGINT NOT NULL REFERENCES menus(id) ON DELETE CASCADE,
    parent_id  BIGINT REFERENCES menu_items(id),
    title      VARCHAR(200) NOT NULL,
    url        VARCHAR(500),
    target     VARCHAR(20) DEFAULT '_self',
    content_id BIGINT REFERENCES contents(id),
    sort_order INT DEFAULT 0
);
```

### 4.7 媒体

```sql
CREATE TABLE media (
    id            BIGSERIAL PRIMARY KEY,
    filename      VARCHAR(255) NOT NULL,
    original_name VARCHAR(255),
    mime_type     VARCHAR(100),
    size          BIGINT,
    path          VARCHAR(500) NOT NULL,
    alt_text      VARCHAR(255),
    width         INT,
    height        INT,
    uploaded_by   BIGINT REFERENCES users(id),
    created_at    TIMESTAMPTZ DEFAULT NOW()
);
```

### 4.8 SEO / URL 管理

```sql
CREATE TABLE redirects (
    id          BIGSERIAL PRIMARY KEY,
    source_path VARCHAR(500) NOT NULL UNIQUE,
    target_path VARCHAR(500) NOT NULL,
    status_code INT DEFAULT 301,
    hit_count   BIGINT DEFAULT 0,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE seo_meta (
    id             BIGSERIAL PRIMARY KEY,
    content_id     BIGINT UNIQUE REFERENCES contents(id) ON DELETE CASCADE,
    meta_title     VARCHAR(200),
    meta_desc      VARCHAR(500),
    canonical_url  VARCHAR(500),
    og_title       VARCHAR(200),
    og_description VARCHAR(500),
    og_image       VARCHAR(500),
    robots         VARCHAR(100) DEFAULT 'index,follow',
    json_ld        TEXT,
    focus_keyword  VARCHAR(100)
);
```

### 4.9 缓存控制 & 审计

```sql
CREATE TABLE cache_tags (
    tag            VARCHAR(100) PRIMARY KEY,
    invalidated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE audit_logs (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT,
    username    VARCHAR(50),
    action      VARCHAR(50) NOT NULL,
    resource    VARCHAR(50),
    resource_id BIGINT,
    details     TEXT,
    ip_address  INET,
    created_at  TIMESTAMPTZ DEFAULT NOW()
) PARTITION BY RANGE (created_at);
```

---

## 五、请求处理流水线

```
客户端请求
    │
    ▼
┌────────────────────┐
│  Rate Limiter      │  ← 令牌桶限流
└────────┬───────────┘
         ▼
┌────────────────────┐
│  Redirect Check    │  ← 查 redirects 表 (内存缓存)
│  (301/302)         │
└────────┬───────────┘
         ▼
┌────────────────────┐     命中
│  Page Cache (L1)   │ ─────────→ 直接返回 HTML (< 1ms)
└────────┬───────────┘
         │ 未命中
         ▼
┌────────────────────┐
│  Rewrite Engine    │  ← URL → ContentType + Slug 解析
└────────┬───────────┘
         ▼
┌────────────────────┐
│  Hook: before_route│  ← 插件可拦截/修改请求
└────────┬───────────┘
         ▼
┌────────────────────┐
│  Theme Handler     │  ← 主题注册的路由处理
│                    │
│  ┌──────────────┐  │
│  │ContentQuery  │  │  ← 查询内容
│  │ → L1 Cache   │  │  ← 先查内存
│  │ → L2 Redis   │  │  ← 再查 Redis
│  │ → PostgreSQL │  │  ← 最后查 DB
│  └──────────────┘  │
│                    │
│  ┌──────────────┐  │
│  │Resolve Tmpl  │  │  ← 模板层级回退查找
│  │Template Exec │  │  ← 渲染模板
│  └──────────────┘  │
└────────┬───────────┘
         ▼
┌────────────────────┐
│  Hook: the_content │  ← Shortcode 解析等
└────────┬───────────┘
         ▼
┌────────────────────┐
│  写入 Page Cache   │  ← 缓存完整响应
└────────┬───────────┘
         ▼
       返回响应
```

---

## 六、开发规划 (分阶段)

### Phase 1: 引擎骨架 + 数据基础

**目标：** 最小可运行引擎，能启动、连接 DB、执行迁移

| # | 状态 | 任务 | 产出文件 | 依赖 |
|---|:---:|------|---------|------|
| 1.1 | ✅ | 初始化 Go Module `go-press` | `go.mod` | - |
| 1.2 | ✅ | 配置系统 | `config/config.go`, `config/default.toml` | - |
| 1.3 | ✅ | 日志系统 | `pkg/logger/logger.go` | - |
| 1.4 | ✅ | 数据库连接 + 迁移引擎 | `pkg/database/postgres.go`, `core/migrate.go` | 1.2 |
| 1.5 | ✅ | Engine 主结构 | `core/engine.go` | 1.2-1.4 |
| 1.6 | ✅ | 入口 main.go | `cmd/server/main.go` | 1.5 |

**验收：** `go run ./cmd/server/` 启动成功，DB 表创建完毕。

---

### Phase 2: 内容系统核心

**目标：** Content + Meta + Taxonomy 完整可用，ContentType 可注册

| # | 状态 | 任务 | 产出文件 | 依赖 |
|---|:---:|------|---------|------|
| 2.1 | ✅ | Content 模型 | `core/content/content.go` | 1.5 |
| 2.2 | ✅ | ContentMeta 模型 | `core/content/meta.go` | 2.1 |
| 2.3 | ✅ | ContentType 注册表 | `core/content/types.go` | - |
| 2.4 | ✅ | ContentQuery 查询构建器 | `core/content/query.go` | 2.1, 2.2 |
| 2.5 | ✅ | Content Repository | `core/content/repository.go` | 2.1-2.4 |
| 2.6 | ✅ | Taxonomy 模型 | `core/taxonomy/taxonomy.go` | 1.5 |
| 2.7 | ✅ | Taxonomy Repository | `core/taxonomy/repository.go` | 2.6 |

**验收：** 能注册 "post"、"page" 类型，通过 ContentQuery 查询并返回结果。

---

### Phase 3: 用户 + 选项 + 菜单

**目标：** 用户认证、RBAC、全局设置内存缓存、菜单系统

| # | 状态 | 任务 | 产出文件 | 依赖 |
|---|:---:|------|---------|------|
| 3.1 | ✅ | User 模型 + Repository | `core/user/user.go`, `repository.go` | 1.5 |
| 3.2 | ✅ | 认证服务 (JWT + Session) | `core/user/auth.go` | 3.1 |
| 3.3 | ✅ | RBAC 角色权限 | `core/user/rbac.go` | 3.1 |
| 3.4 | ✅ | Option Store (内存 + DB) | `core/option/option.go` | 1.5 |
| 3.5 | ✅ | Menu Store (内存 + DB) | `core/menu/menu.go` | 1.5 |
| 3.6 | ✅ | Media 模型 + 上传 | `core/media/media.go`, `repository.go` | 1.5, 3.1 |
| 3.7 | ✅ | 图片处理 (缩略图/WebP) | `core/media/image.go` | 3.6 |

**验收：** 用户注册/登录/权限校验通过，Options 启动加载到内存。

---

### Phase 4: Hook + Cache + Worker

**目标：** 事件总线、多级缓存、异步任务 — GoPress 的核心竞争力

| # | 状态 | 任务 | 产出文件 | 依赖 |
|---|:---:|------|---------|------|
| 4.1 | ✅ | Hook 事件总线 | `core/hook/hook.go` | - |
| 4.2 | ✅ | L1 内存缓存 (ristretto) | `core/cache/memory.go` | - |
| 4.3 | ✅ | L2 Redis 缓存 | `core/cache/redis.go` | - |
| 4.4 | ✅ | Cache Manager (L1+L2编排) | `core/cache/cache.go` | 4.2, 4.3 |
| 4.5 | ✅ | 整页缓存中间件 | `core/cache/page.go` | 4.4 |
| 4.6 | ✅ | 片段缓存 | `core/cache/fragment.go` | 4.4 |
| 4.7 | ✅ | Worker Pool | `core/worker/pool.go` | - |
| 4.8 | ✅ | 定时调度器 | `core/worker/scheduler.go` | 4.7 |

**验收：** 首次请求写入缓存，第二次请求从 L1 返回 (< 1ms)；定时任务按 cron 执行。

---

### Phase 5: URL / SEO / REST API

**目标：** 完整的 URL 管理、SEO 基础设施、自动 REST API

| # | 状态 | 任务 | 产出文件 | 依赖 |
|---|:---:|------|---------|------|
| 5.1 | ✅ | Rewrite Engine (URL → 内容类型) | `core/rewrite/rewrite.go` | 2.3 |
| 5.2 | ✅ | Permalink 结构定义 | `core/rewrite/permalink.go` | 5.1 |
| 5.3 | ✅ | XML Sitemap 生成 | `core/rewrite/sitemap.go` | 2.5, 5.2 |
| 5.4 | ✅ | 301/302 重定向管理 | `core/rewrite/redirect.go` | 1.5 |
| 5.5 | ✅ | SEO Meta (canonical, JSON-LD) | `core/rewrite/seo.go` | 2.1 |
| 5.6 | ✅ | REST API 自动端点 | `core/api/api.go` | 2.3, 2.5 |
| 5.7 | ✅ | API 认证 + 限流 | `core/api/middleware.go` | 3.2, 5.6 |
| 5.8 | ✅ | 标准化响应 | `core/api/api.go` (内置) | 5.6 |

**验收：** `/api/v1/products` 自动可用；Sitemap 生成；永久链接结构可配置。

---

### Phase 6: 主题引擎 + 后台 CMS

**目标：** 主题加载，模板层级回退，后台根据 ContentType 动态生成

| # | 状态 | 任务 | 产出文件 | 依赖 |
|---|:---:|------|---------|------|
| 6.1 | ✅ | Theme 接口 + 加载器 | `core/theme/theme.go`, `engine.go` | 2.3, 4.1 |
| 6.2 | ✅ | 模板层级回退 | `core/theme/hierarchy.go` | 6.1 |
| 6.3 | ❌ | Shortcode 解析器 | `core/content/shortcode.go` | 4.1 |
| 6.4 | ✅ | Plugin 接口 + 加载器 | `core/plugin/plugin.go` | 4.1 |
| 6.5 | ✅ | Admin Handler (数据驱动) | `core/admin/handler.go` | 2.3, 3.3, 6.1 |
| 6.6 | ✅ | Admin Routes | `core/admin/routes.go` | 6.5 |
| 6.7 | ✅ | Admin 模板 (通用) | `core/admin/templates/` | 6.5 |

**验收：** 后台能动态展示所有注册的 ContentType，增删改查正常。

---

### Phase 7: 迁移 modern-company 主题

**目标：** 将当前 go-press 前端实现迁移为 GoPress 主题

| # | 状态 | 任务 | 说明 | 依赖 |
|---|:---:|------|------|------|
| 7.1 | ✅ | 创建 `themes/modern-company/theme.go` | 实现 Theme 接口 | 6.1 |
| 7.2 | ✅ | 创建 `functions.go` | 注册 product/service/showcase 类型 | 2.3 |
| 7.3 | ✅ | 迁移模板文件 | `web/templates/` → `themes/modern-company/templates/` | 6.2 |
| 7.4 | ✅ | 迁移静态资源 | `web/static/` → `themes/modern-company/static/` | - |
| 7.5 | ✅ | 迁移页面 Handler | `handlers.go` → 基于 ContentQuery | 2.4 |
| 7.6 | ✅ | 创建站点配置 | `sites/go-press/config.toml` + `seed.toml` | - |
| 7.7 | ❌ | 端到端测试 | 对比新旧版本所有页面一致 | 全部 |

**验收：** go-press 网站在 GoPress + modern-company 主题上完整运行。

---

### Phase 8: 高并发优化 + 生产加固

| # | 状态 | 任务 | 说明 |
|---|:---:|------|------|
| 8.1 | ❌ | 读写分离连接池 | `pkg/database/pool.go` |
| 8.2 | ❌ | 压力测试 (wrk/vegeta) | 验证缓存效果 |
| 8.3 | ✅ | Graceful Shutdown | 优雅停机 |
| 8.4 | ❌ | Prometheus 指标 | 请求延迟、缓存命中率、goroutine 数 |
| 8.5 | ✅ | Docker 多阶段构建 | 生产容器镜像 |
| 8.6 | ❌ | CI/CD 流水线 | GitHub Actions |

---

## 七、当前代码迁移映射

### 7.1 模型迁移

| 当前模型 | GoPress 映射 |
|---------|-------------|
| `models.Product` | `Content{Type: "product"}` |
| `models.Service` | `Content{Type: "service"}` |
| `models.Showcase` | `Content{Type: "showcase"}` + Meta: `client`, `location` |
| `models.Post` | `Content{Type: "post"}` |
| `models.Category` | `Term` + `Taxonomy{taxonomy: "category"}` |
| `models.Tag` | `Term` + `Taxonomy{taxonomy: "tag"}` |
| `models.ContactMessage` | `Content{Type: "contact_message"}` 或独立插件 |
| `models.SiteSetting` | `options` 表 |
| `models.AdminUser` | `users` 表 |
| `models.Media` | `media` 表 |
| `models.AuditLog` | `audit_logs` 表 |

### 7.2 modern-company 主题注册的类型

```go
// themes/modern-company/functions.go
func (t *ModernCompany) Setup(app *engine.Engine) {
    reg := app.Registry

    // 注册内容类型
    reg.RegisterType(content.ContentTypeDef{
        Name: "product", Label: "产品", LabelPlural: "产品列表",
        Supports:   []string{"title", "content", "excerpt", "thumbnail"},
        MetaFields: []content.MetaFieldDef{
            {Key: "sort_order", Label: "排序", Type: "int", Default: "0"},
        },
        Taxonomies: []string{"product_cat"},
        HasArchive: true,
        Rewrite:    content.RewriteRule{Slug: "products"},
    })

    reg.RegisterType(content.ContentTypeDef{
        Name: "service", Label: "服务", LabelPlural: "服务列表",
        Supports:   []string{"title", "content", "excerpt", "thumbnail"},
        MetaFields: []content.MetaFieldDef{
            {Key: "sort_order", Label: "排序", Type: "int", Default: "0"},
        },
        HasArchive: true,
        Rewrite:    content.RewriteRule{Slug: "services"},
    })

    reg.RegisterType(content.ContentTypeDef{
        Name: "showcase", Label: "案例", LabelPlural: "案例展示",
        Supports:   []string{"title", "content", "excerpt", "thumbnail"},
        MetaFields: []content.MetaFieldDef{
            {Key: "client", Label: "客户", Type: "string"},
            {Key: "location", Label: "位置", Type: "string"},
        },
        HasArchive: true,
        Rewrite:    content.RewriteRule{Slug: "showcase"},
    })

    // 注册分类法
    reg.RegisterTaxonomy(content.TaxonomyDef{
        Name: "product_cat", Label: "产品分类",
        ContentTypes: []string{"product"},
        Hierarchical: true,
    })

    // 注册菜单位置
    app.Menus.RegisterLocation("header", "顶部导航")
    app.Menus.RegisterLocation("footer", "底部导航")

    // 注册 Hook
    app.Hooks.AddFilter("the_content", t.processShortcodes, 10)
}
```

### 7.3 文件迁移清单

```
当前位置                                     → GoPress 新位置
───────────────────────────────────────────────────────────────
web/templates/layouts/base.tmpl              → themes/modern-company/templates/layouts/base.tmpl
web/templates/partials/header.tmpl           → themes/modern-company/templates/partials/header.tmpl
web/templates/partials/footer.tmpl           → themes/modern-company/templates/partials/footer.tmpl
web/templates/pages/home.tmpl               → themes/modern-company/templates/pages/index.tmpl
web/templates/pages/about.tmpl              → themes/modern-company/templates/pages/page-about.tmpl
web/templates/pages/products.tmpl           → themes/modern-company/templates/pages/archive-product.tmpl
web/templates/pages/services.tmpl           → themes/modern-company/templates/pages/archive-service.tmpl
web/templates/pages/showcase.tmpl           → themes/modern-company/templates/pages/archive-showcase.tmpl
web/templates/pages/blog.tmpl               → themes/modern-company/templates/pages/archive-post.tmpl
web/templates/pages/contact.tmpl            → themes/modern-company/templates/pages/page-contact.tmpl
web/static/css/style.css                    → themes/modern-company/static/css/style.css
web/static/js/main.js                       → themes/modern-company/static/js/main.js
web/templates/admin/                        → core/admin/templates/ (通用后台，不属于主题)
web/static/admin/                           → core/admin/static/
internal/handlers/handlers.go              → themes/modern-company/handlers.go
internal/services/page_service.go          → 拆解到 themes/modern-company/handlers.go (用 ContentQuery)
internal/db/repository.go                  → core/content/repository.go (通用化)
internal/db/seed.go                        → sites/go-press/seed.toml (数据配置化)
internal/models/models.go                  → 废弃，由 core/content/content.go 替代
internal/models/admin.go                   → core/user/user.go + core/media/media.go
internal/rbac/rbac.go                      → core/user/rbac.go
internal/modules/cms/                      → core/admin/ (通用化、数据驱动)
internal/middleware/middleware.go           → core/ 各中间件分散到对应模块
internal/routes/routes.go                  → 废弃，由 Theme.Routes() + core/admin/routes.go 替代
config/config.toml                         → sites/go-press/config.toml
```

---

## 八、关键技术选型

| 组件 | 选型 | 理由 |
|------|------|------|
| HTTP | Gin | 已验证，性能优秀 |
| ORM | GORM | 已使用，支持 AutoMigrate |
| 配置 | Viper + TOML | 已使用 |
| JWT | golang-jwt/jwt/v5 | 已使用 |
| L1 缓存 | dgraph-io/ristretto | 高性能并发安全 LRU |
| L2 缓存 | go-redis/redis/v9 | 业界标准 |
| 图片处理 | disintegration/imaging | 纯 Go，无 CGO |
| 日志 | log/slog (stdlib) | Go 1.21+ 标准库 |
| 定时 | robfig/cron/v3 | 成熟的 cron 调度 |
| 限流 | golang.org/x/time/rate | 标准库 |
| XML Sitemap | encoding/xml (stdlib) | 标准库 |
| 密码 | golang.org/x/crypto/bcrypt | 已使用 |

---

## 九、性能目标

| 指标 | 目标值 | 实现方式 |
|------|--------|---------|
| 页面缓存命中响应 | < 1ms | L1 内存整页缓存 |
| 首次渲染 (无缓存) | < 50ms | ContentQuery 优化索引 |
| 并发连接数 | 50,000+ | goroutine 池 |
| 内存占用 (idle) | < 50MB | 按需加载 |
| 每秒请求 (缓存命中) | 100,000+ QPS | L1 缓存直接返回 |
| 每秒请求 (无缓存) | 5,000+ QPS | 数据库连接池 + 索引 |
