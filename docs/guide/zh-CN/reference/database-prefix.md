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

## 版本化迁移

每个站点在自己的 `<前缀>schema_migrations` 表里记录表结构变更，按模块（`core`
或 `plugin:<slug>`）和步骤版本号各占一行。

- **迁移锁**：迁移前，进程按 schema + 表前缀取一个 PostgreSQL 咨询锁，整个过程
  固定在同一个连接上持有。同一站点的第二个进程会等待（最长 5 分钟，日志里会写出
  持锁进程的 backend pid）；其他前缀的站点互不等待，即使一个前缀恰好是另一个的
  开头（`gp_` 和 `gp_shop_`）。
- **基线**：core 仍在每次启动时执行 GORM AutoMigrate 和遗留修补，和以前完全一样。
  `core` 的第 1 步代表这套表结构：在版本化迁移之前就已有表的站点登记为 `adopted`，
  新安装的站点登记为 `applied`。判断时按精确表名查询，不用名称模式匹配。
- **阻塞型与在线型步骤**：阻塞型步骤在 `/readyz` 报告就绪之前完成，失败时站点
  不会启动；在线型步骤（例如 `CREATE INDEX CONCURRENTLY`）在站点就绪后于后台
  执行，失败只记录日志，下次启动时重试。
- **回滚**：迁移只新增表、可为空或带默认值的列以及索引，所以上一个版本仍能在迁移
  过的数据库上运行。旧版本会忽略记录表；发现不认识的版本号（由更新的版本写入）
  时只记录日志，照常启动。
- **插件**：实现了 `plugin.MigrationProvider` 的插件，在每次激活前（启动时和在
  后台启用时）执行它的迁移步骤。阻塞型步骤失败时插件保持未启用，后台会显示错误。
  见[插件开发](../plugins/creating-plugins.md#插件数据表)。

不启动 HTTP 服务、单独查看或执行迁移：

```bash
./build/gopress-server -config sites/example.com/config.toml -migrate=status
./build/gopress-server -config sites/example.com/config.toml -migrate=up
```

`status` 不做任何修改。`up` 会完整走一遍启动流程（core，然后是每个已启用的插件），
等在线型步骤完成后打印状态并退出；有步骤失败时以状态码 1 退出。

## 表注册表

`core.RegisterPluginTable()` / `core.RegisterThemeTable()` 不仅用于命名约定，还把表归属信息存到内存注册表，用于：

- 后台「数据库管理」面板按 Owner 列出表
- 卸载主题/插件时（未来功能）按归属安全清理
- 调试时快速定位某表来自哪个组件
