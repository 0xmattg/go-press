# Roadmap and Contributions

## Completed Milestones

- **Engine and storage foundation** — lifecycle orchestration, PostgreSQL/GORM,
  migrations, prefixed table naming, table ownership registry, options, workers,
  cache, and structured logging.
- **Unified content system** — content/meta models, registry-driven content
  types, fluent queries, request scopes, taxonomies, statuses, scheduled
  publishing, sorting, and theme-independent core types.
- **Admin CMS** — data-driven CRUD, Quill editor, media picker, server-side
  filtering and pagination, Screen Options, menus, redirects, cache, mail,
  themes, plugins, users, comments, settings, audit logs, and RBAC enforcement.
- **Theme runtime** — BaseTheme, config-driven rewrites and template mapping,
  page bundles, fallback hierarchy, common funcmap, semantic frontend hook
  slots, theme settings, logos, demo import, and hot switching.
- **SEO and public URLs** — canonical URLs, Open Graph, JSON-LD, favicon output,
  redirects, taxonomy archives, dynamic/static sitemap output, language-aware
  internal links, and per-content SEO extension hooks.
- **Media pipeline** — upload metadata, responsive JPEG/PNG variants, optional
  WebP output, preload/priority helpers, and historical variant rebuilds.
- **Plugin runtime** — actions and filters with removable handles, settings
  providers, protected route/middleware registration, router rebuilds, cache
  invalidation, plugin-owned tables, and clean runtime deactivation.
- **Internationalization** — core locale manager, admin language, language-aware
  cache and URLs, translatable theme/site options, content and menu translation,
  same-slug language variants, and sitemap hreflang transformers.
- **Public accounts** — provider-neutral users, linked identities, revocable
  sessions, registration policy, Google OIDC and EIP-4361 SIWE providers, and
  theme account helpers.
- **Comments and profiles** — authenticated comments, one direct reply level,
  moderation/RBAC, cache invalidation, admin pagination, and own-account profile
  contracts.
- **Bundled operational plugins** — multilingual management, SEO overrides,
  site-level code snippets, and self-hosted traffic analytics with retention and
  local GeoIP support.
- **Delivery tooling** — web installer with live handler switch, `gopress`
  autoload/build workflow, Swagger generation, site-scoped configuration, and
  generated public artifacts.

## Planned

- Shortcode parser.
- Read/write database connection splitting.
- Prometheus metrics.
- CI/CD pipeline hardening.
- Benchmark suite and performance tuning.
- Theme and plugin version migration hooks.
- Online theme marketplace and one-click installation.

## Contributing

1. Fork the repository.
2. Create a focused feature branch.
3. Add tests and documentation for the affected public contract.
4. Commit focused changes and push the branch.
5. Open a pull request with behavior, migration, security, and compatibility
   notes where applicable.

## License

[MIT License](../../../../LICENSE)
