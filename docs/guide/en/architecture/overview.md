# Architecture Overview and Startup Flow

## System Architecture

```text
HTTP request
  -> Gin router and middleware
     Logger -> Recovery -> CORS -> RateLimit -> PageCache
  -> REST API | Admin | Swagger | Theme Dispatcher
  -> BaseTheme runtime
     custom route -> rewrite resolution -> template mapping -> SEO injection
  -> Content | Taxonomy | User/Auth | Media | Options | Menus
  -> PostgreSQL (dbprefix) | L1 memory cache | optional L2 Redis
```

The frontend dispatcher is the final catch-all. It delegates to the active
theme only after health, static, sitemap, API, admin, plugin, and documentation
routes have had a chance to match.

## CLI and Build Layer

`gopress serve`, `gopress build`, and `gopress gen` scan root-level theme and
plugin manifests and regenerate `internal/autoload/autoload_gen.go`. The server
entry point therefore stays generic: extensions self-register through generated
blank imports rather than manual edits to `cmd/server/main.go`.

- `gopress serve` regenerates autoload and runs the server, forwarding flags and
  shutdown signals.
- `gopress build` regenerates autoload and compiles a production server binary.
- `gopress gen` refreshes autoload only for IDE or CI workflows.

Because themes and plugins are compiled into the binary, a production build
contains exactly the extensions present at build time.

## Engine Startup

```text
cmd/server
  -> resolve site config
  -> core.BuildAndBootstrap(cfg, configPath, seed)
     1. reject an insecure JWT secret and set the database table prefix
     2. connect to PostgreSQL with the prefixed GORM naming strategy
     3. migrate core and registered extension tables
     4. optionally import site seed data
     5. bootstrap options, menus, redirects, cache, workers, and shared services
     6. load themes, restore core content types, and activate the configured theme
     7. load plugins and reconcile the active theme's declared requirements
     8. register the admin surface and build the Gin router
```

Theme activation clears the previous theme-owned registry entries, restores
core types, reads the active `theme.toml`, registers its content types and menu
locations, and runs theme setup. Plugin activation then attaches hooks,
middleware, routes, settings providers, and other public extension points.

## Router Surface

The built router contains, in order, the shared middleware chain, health and
public generated-file routes, `/api/v1/*`, `/admin/*`, `/swagger/*`, plugin
routes, and the active theme's catch-all frontend handler. Activation changes
that affect routes rebuild the router and clear relevant cache paths.

## Installer Mode

When no completed site configuration exists, the same process serves the web
installer. The installer validates and optionally creates the database, writes
a site-scoped `config.toml`, migrates tables, creates the first administrator,
and atomically switches the handler to the live application without a manual
restart.

## Engine Responsibilities

- Own stable content, taxonomy, user, session, permission, media, menu, option,
  cache, rewrite, SEO, mail, comment, public-submission, and worker services.
- Register core content types and config-driven theme types.
- Expose generic repositories, template helpers, hooks, filters, providers,
  middleware points, and protected route helpers.
- Build public URLs, canonical metadata, sitemap entries, and fallback templates
  from the same registries.
- Keep extension activation, dependency checks, migrations, and cache invalidation
  coordinated.

## Extension Boundaries

- **Core-type protection** — `post`, `page`, `contact_message`, `category`, and
  `tag` survive every theme switch.
- **Config-driven theme models** — admin CRUD, REST, rewrites, and templates read
  the active content registry rather than special-casing business type names.
- **Hot theme and plugin changes** — routes and cache are rebuilt; plugins remove
  their saved hook handles during deactivation.
- **Semantic frontend slots** — themes expose stable locations such as
  `theme.head.end` and `header.nav.after`; plugins contribute local markup only.
- **No runtime cross-imports** — themes and plugins depend on core contracts, not
  each other's packages or private option keys.
- **Provider-neutral public authentication** — core owns users, identities,
  registration policy, and revocable sessions; identity plugins verify external
  protocols, and themes consume one normalized account context.
- **Policy-driven public authoring** — themes can declare which roles may create
  or maintain a content type; core enforces active-account, RBAC, ownership,
  editorial state, validation, and abuse limits without owning the UI.

See [Public Authentication](public-authentication.md) for the account and
identity-provider contract, and
[Public Content Submission](public-content-submission.md) for authenticated
frontend authoring.
