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

## Versioned Migrations

Each site records its schema changes in its own `<prefix>schema_migrations`
table, one row per module (`core`, or `plugin:<slug>`) and step version.

- **Lock.** Before migrating, a process takes a PostgreSQL advisory lock keyed by
  the schema and table prefix, held on one connection for the whole pass. A
  second process of the same site waits for it (up to 5 minutes, logging the
  holder's backend pid); sites with other prefixes never wait, even when one
  prefix is the beginning of another (`gp_` and `gp_shop_`).
- **Baseline.** Core still runs GORM AutoMigrate and its legacy fixes on every
  start, exactly as before. Step 1 of `core` stands for that schema: a site
  whose tables existed before versioned migrations records it as `adopted`, a
  new install as `applied`. The check uses the exact table name, never a name
  pattern.
- **Blocking and online steps.** Blocking steps finish before `/readyz` reports
  ready, and a failure stops the start. Online steps, such as
  `CREATE INDEX CONCURRENTLY`, run in the background after the site is ready; a
  failure is logged and retried on the next start.
- **Rollback.** Migrations only add tables, nullable or defaulted columns, and
  indexes, so the previous release still runs on a migrated database. It
  ignores the record table, and a release that finds versions it does not know
  (written by a newer release) logs them and starts anyway.
- **Plugins.** A plugin that implements `plugin.MigrationProvider` has its
  steps applied before each activation, at startup and when it is enabled in
  the admin. If a blocking step fails the plugin stays inactive and the admin
  shows the error. See [Creating Plugins](../plugins/creating-plugins.md#plugin-data).

Inspect or apply migrations without serving HTTP:

```bash
./build/gopress-server -config sites/example.com/config.toml -migrate=status
./build/gopress-server -config sites/example.com/config.toml -migrate=up
```

`status` changes nothing. `up` performs a full start (core, then each active
plugin), waits for the online steps, prints the status, and exits; it exits
with status 1 if a step failed.

## Table Registry

The table registry records table ownership for admin database tooling, future uninstall workflows, and debugging.

