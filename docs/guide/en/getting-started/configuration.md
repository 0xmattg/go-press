# Configuration

GoPress reads TOML configuration files from the `sites/` directory. The installer writes a site-specific file such as `sites/localhost/config.toml`; production deployments should create one file per hostname.

## File Discovery

Configuration is resolved by host:

```text
sites/{host}/config.toml
sites/default/config.toml
config/config.toml
```

This makes local development, preview environments, and production domains share the same binary while keeping runtime settings isolated.

## Database

```toml
[pg]
host = "localhost"
port = 5432
user = "postgres"
password = ""
database = "gopress"
schema = "public"
table_prefix = "gp_"
```

`table_prefix` is important when multiple GoPress instances share one PostgreSQL database. Core tables, plugin tables, and theme tables are all derived from this prefix.

## Server

```toml
[server]
addr = ":8080"
mode = "debug"
```

`addr` controls the HTTP listener. `mode` is passed to Gin and should normally be set to `release` in production.

## Site

```toml
[site]
name = "My GoPress Site"
theme = "atelier-slate"
language = "en"
```

The active theme is loaded by slug. Public site language and admin interface language can also be managed from the admin UI after installation.

## Cache

```toml
[cache]
enabled = true
redis_addr = ""
```

When Redis is not configured, GoPress keeps the in-process memory cache path active and degrades gracefully.

## Runtime Files

- `uploads/` stores uploaded media and generated variants.
- `sites/{host}/config.toml` stores site configuration.
- `sitemap.xml` can be generated from the admin UI.

For public repositories, site-specific configuration and generated runtime files should stay ignored by Git.

