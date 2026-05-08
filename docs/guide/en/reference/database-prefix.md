# Database Prefixes

GoPress supports WordPress-style table prefixes so multiple GoPress instances can share one PostgreSQL database while keeping data isolated.

## Naming Rules

| Owner | Format | Example |
|---|---|---|
| Core table | `{prefix}{table}` | `gp_contents`, `gp_users`, `gp_options` |
| Plugin table | `{prefix}plgn_{slug}_{table}` | `gp_plgn_multilang_translations` |
| Theme table | `{prefix}thm_{slug}_{table}` | `gp_thm_financial-news_tickers` |

`{prefix}` comes from `[pg] table_prefix`; the default is `gp_`.

## Usage

```go
import "go-press/pkg/dbprefix"

dbprefix.Table("contents")
dbprefix.PluginTable("multilang", "translations")
dbprefix.ThemeTable("financial-news", "tickers")

core.RegisterPluginTable("multilang", "translations")
core.RegisterThemeTable("financial-news", "tickers")
```

## Safety Layers

GoPress uses two safeguards:

1. A custom GORM naming strategy.
2. Explicit `TableName()` methods on models where needed.

This prevents common ORM paths from accidentally bypassing the prefix contract.

## Multiple Instances

```toml
[pg]
database = "shared"
table_prefix = "blog_"
```

Another site can use the same database with a different prefix such as `shop_`.

## Table Registry

The table registry records table ownership for admin database tooling, future uninstall workflows, and debugging.

