# Technology Stack and Performance

## Stack

| Component | Choice | Notes |
|---|---|---|
| Web framework | Gin | High-performance HTTP framework. |
| ORM | GORM | Migrations, preloading, soft deletes, naming strategy. |
| Database | PostgreSQL | Primary data store with table-prefix isolation. |
| Cache | Redis + memory LRU | L1/L2 cache with graceful fallback. |
| Auth | golang-jwt | JWT Bearer token and API key modes. |
| Config | Viper + TOML | Declarative configuration and multi-site discovery. |
| Logging | `log/slog` | Standard-library structured logging. |
| i18n | go-i18n | Locale file loading and fallback. |
| Editor | Quill 2.0 | Rich-text editing in admin. |
| API docs | swaggo/swag | Annotation-driven OpenAPI generation. |

## Performance Targets

These targets guide architecture and future benchmark work. Public releases should include reproducible benchmarks and test environment details.

| Metric | Target |
|---|---|
| Page-cache hit response | < 1ms |
| First render without cache | < 50ms |
| Concurrent connections | 50,000+ |
| QPS with cache hit | 100,000+ |
| QPS without cache | 5,000+ |
| Idle memory | < 50MB |

## Compared with WordPress

This comparison explains architectural tradeoffs, not absolute superiority.

| Area | WordPress | GoPress |
|---|---|---|
| Runtime | PHP-FPM and web server request lifecycle. | Long-running Go service. |
| Extension | Mature theme/plugin ecosystem and dynamic runtime loading. | Go interfaces, hooks, and typed contracts. |
| Cache | Commonly added through plugins, object cache, or reverse proxy. | Built-in memory, Redis, and database cache paths. |
| Scheduled jobs | WP-Cron or system cron patterns. | In-process scheduler and worker pool. |
| Deployment | Web server, PHP runtime, database, and plugins. | Compiled service plus database and optional Redis. |

