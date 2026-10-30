# 数据库表前缀

GoPress 实现了 WordPress 风格的表前缀机制，允许多个 GoPress 实例共享同一数据库。

## 命名规则

| 层级 | 格式 | 示例 |
|------|------|------|
| 核心表 | `{prefix}{table}` | `gp_contents`、`gp_users`、`gp_options` |
| 插件表 | `{prefix}plgn_{slug}_{table}` | `gp_plgn_multilang_translations` |
| 主题表 | `{prefix}thm_{slug}_{table}` | `gp_thm_financial-news_tickers` |

`{prefix}` 来自配置文件 `[pg] table_prefix`，默认 `gp_`。Web 安装器中可在第一步设置。

## 在代码中使用

```go
import "go-press/pkg/dbprefix"

// 核心表
tableName := dbprefix.Table("contents")           // → "gp_contents"

// 插件表
tableName := dbprefix.PluginTable("multilang", "translations")
// → "gp_plgn_multilang_translations"

// 主题表
tableName := dbprefix.ThemeTable("financial-news", "tickers")
// → "gp_thm_financial-news_tickers"

// 注册表追踪
core.RegisterPluginTable("multilang", "translations")
core.RegisterThemeTable("financial-news", "tickers")

tables := core.AllTables()                  // 全部已注册表
tables := core.TablesForPlugin("multilang") // 某插件的所有表
```

## 双重保障机制

GoPress 使用两层机制确保表名永远正确：

1. **GORM `NamingStrategy`** — 通过自定义的 `dbprefix` 实现 `TableName(table string) string` 接口，所有 GORM 操作走这条管道
2. **Model `TableName()` 方法** — 在 model 上显式声明，避免某些 GORM 路径绕过 NamingStrategy

## 多实例共享数据库

通过不同的 `table_prefix`，多个 GoPress 实例可以共享同一 PostgreSQL 数据库：

```toml
# 实例 1：博客
[pg]
database = "shared"
table_prefix = "blog_"

# 实例 2：商城
[pg]
database = "shared"
table_prefix = "shop_"
```

实例间的内容、用户、设置完全隔离，但可以共享 PG 实例的连接池和运维成本。

这些实例也可以共用同一个 Redis DB：缓存 Key 按 `[redis].namespace` 隔离，
未配置时会根据数据库连接与 `table_prefix` 自动派生，因此不同前缀的站点不会
读取或清理彼此的缓存。详见[配置](../getting-started/configuration.md)。

## 索引与表前缀

PostgreSQL 的索引名在同一个 schema 内唯一，而不是在每张表内唯一。模型里写死的
索引名（例如 `uniqueIndex:idx_user_identity_key`）只会落到同一 schema 中**第一个**
完成迁移的站点表上；GORM 用 `CREATE INDEX IF NOT EXISTS` 创建索引，其他前缀的
同类表会被静默跳过，唯一性约束随之失效。

core 的 `dbschema.AutoMigrate(db, models...)` 在 GORM AutoMigrate 之后检查当前
站点的每张表：如果缺少与声明等价的索引（相同的列顺序、唯一性、是否为部分索引），
就以「表名 + 声明名」创建一个本站专属索引，超过 63 个字符时截断并追加短哈希。
它只做加法，不删除或重命名已有索引，旧版本二进制仍可直接运行在补建后的数据库上。
若已有重复数据导致唯一索引无法建立，它会记录警告并在下次启动时重试，不会阻止
站点启动；需要先清理重复数据，相关唯一性保护才会生效。

core 表已经使用这一机制。插件声明了带固定名称的索引时，迁移应调用
`dbschema.AutoMigrate`，而不是直接调用 `db.AutoMigrate`。

## 表注册表

`core.RegisterPluginTable()` / `core.RegisterThemeTable()` 不仅用于命名约定，还把表归属信息存到内存注册表，用于：

- 后台「数据库管理」面板按 Owner 列出表
- 卸载主题/插件时（未来功能）按归属安全清理
- 调试时快速定位某表来自哪个组件
