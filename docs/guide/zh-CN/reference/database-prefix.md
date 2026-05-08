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

## 表注册表

`core.RegisterPluginTable()` / `core.RegisterThemeTable()` 不仅用于命名约定，还把表归属信息存到内存注册表，用于：

- 后台「数据库管理」面板按 Owner 列出表
- 卸载主题/插件时（未来功能）按归属安全清理
- 调试时快速定位某表来自哪个组件
