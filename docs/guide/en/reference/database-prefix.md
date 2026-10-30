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

Such sites can also share one Redis database. Cache keys are isolated by
`[redis].namespace`, which is derived from the database connection and
`table_prefix` when unset, so sites with different prefixes never read or clear
each other's cache. See [Configuration](../getting-started/configuration.md).

## Indexes and Prefixes

PostgreSQL index names are unique per schema, not per table. A fixed index name
in a model (for example `uniqueIndex:idx_user_identity_key`) lands only on the
**first** site in the schema to migrate. GORM creates indexes with
`CREATE INDEX IF NOT EXISTS`, so the same table under every other prefix is
skipped silently and loses its uniqueness guarantee.

Core's `dbschema.AutoMigrate(db, models...)` checks each of the current site's
tables after GORM AutoMigrate. When a table lacks an index equivalent to the
declaration (same column order, uniqueness, and full or partial coverage), it
creates a site-owned index named after the table plus the declared name,
truncated with a short hash beyond 63 characters. It only adds indexes, never
drops or renames them, so the previous binary still runs on the repaired
database. If existing duplicate rows block a unique index, it logs a warning and
retries on the next start instead of preventing the site from booting; the
uniqueness protection takes effect once the duplicates are cleaned up.

Core tables already use this. Plugins that declare fixed index names should
migrate with `dbschema.AutoMigrate` instead of calling `db.AutoMigrate`
directly.

## Table Registry

The table registry records table ownership for admin database tooling, future uninstall workflows, and debugging.

