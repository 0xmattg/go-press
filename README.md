<p align="right">
  <strong>English</strong> · <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <img src="docs/logo/gopress_logo.png" alt="GoPress Logo" width="220">
</p>

> GoPress is a content management framework and CMS engine written in Go for self-hosted websites and content applications that need themes, plugins, APIs, SEO, media handling, and a practical admin experience.
> It brings content modeling, admin CRUD, theme rendering, plugin extension points, REST APIs, SEO infrastructure, multi-level caching, responsive media variants, and multi-site configuration into one composable Go codebase.
> It is suitable for company websites, editorial sites, product showcases, documentation hubs, and custom systems that want to keep a CMS authoring workflow inside a Go deployment model.

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

---

## What Is GoPress?

GoPress reorganizes the proven building blocks of a traditional CMS — content models, themes, plugins, and admin workflows — around the Go runtime and Go engineering ecosystem. It provides a unified content model, data-driven admin CRUD, a theme template engine, hook/filter extension points, REST APIs, SEO primitives, multi-level caching, media variants, and site-level configuration.

GoPress is not a line-by-line rewrite of WordPress, and it is not a statement against PHP. It focuses on a narrower engineering need: keeping the editorial experience and extension model of a CMS while gaining the deployment, concurrency, observability, and long-term maintenance advantages of Go.

## Project Status

GoPress is currently in **beta**. The core content model, admin CMS, theme engine, plugin mechanism, SEO layer, cache path, media pipeline, and bundled example themes are usable, but the project still needs more production validation, benchmark coverage, migration guides, and security review before a stable public release.

If you plan to use GoPress in production, start with internal sites, company websites, documentation sites, or content-driven applications, then validate your traffic profile, editorial workflow, backup strategy, and deployment model.

## Why GoPress?

The CMS ecosystem has proven the long-term value of the “content model + theme + plugin + admin” abstraction. GoPress keeps that product shape while using Go’s single-service deployment model, goroutine concurrency, static typing, and standardized toolchain to reduce operational complexity for self-hosted CMS projects.

The comparison below is not meant to rank technology stacks. It describes the design trade-offs GoPress makes:

| Area | WordPress (PHP) | GoPress (Go) |
|---|---|---|
| Runtime model | PHP-FPM / web server stack, centered on request lifecycle execution | Long-running Go service process, suitable for in-memory registries and workers |
| Extension model | Mature theme/plugin ecosystem with flexible runtime loading | Go interfaces and hook registration, emphasizing type safety and maintainability |
| Cache strategy | Usually enhanced through plugins, object caches, and reverse proxies | Built-in memory, Redis, and page-cache paths with graceful fallback |
| Scheduled work | Commonly handled through WP-Cron or system cron | Process-owned scheduler and worker pool |
| Deployment shape | Web server, PHP runtime, database, and optional cache services | Compiled Go service plus database and optional Redis |

## Design Principles

1. **Content first** — a unified `Content + Meta` model supports posts, contact messages, and theme-declared custom content types.
2. **Themes stay separate from the engine** — themes render output; the engine owns routing, querying, SEO, media, admin behavior, and shared infrastructure.
3. **Plugins extend through interfaces** — plugins register capabilities through Go interfaces, hooks, and filters instead of hidden runtime coupling.
4. **Cache is a core capability** — memory cache, Redis cache, and page cache are part of the core path, with graceful degradation when Redis is unavailable.
5. **SEO is built in** — URL rewriting, permalinks, canonical tags, sitemap generation, meta output, and redirects are handled at the core layer.
6. **API first** — registered content types can expose REST endpoints and Swagger / OpenAPI documentation.
7. **Instance isolation** — table prefixes and site-level configuration allow multiple instances to share infrastructure while keeping data boundaries clear.

## Theme And Admin UI Preview

GoPress ships with a practical admin CMS and a set of production-oriented example themes. The previews below show the direction of the bundled UI: theme-specific visual systems on the public side, and a focused content-management workspace on the admin side.

### Admin UI

The first-run installer guides database connection, site bootstrap, and admin account creation before the CMS opens.

| Database Setup | Site Bootstrap | Ready To Use |
|---|---|---|
| <img src="docs/resources/ui_preview/admin/install-1.png" alt="GoPress installer database setup preview"> | <img src="docs/resources/ui_preview/admin/install-2.png" alt="GoPress installer site bootstrap preview"> | <img src="docs/resources/ui_preview/admin/install-3.png" alt="GoPress installer completion preview"> |

| Content Workspace | Theme Settings | Media And Editing |
|---|---|---|
| <img src="docs/resources/ui_preview/admin/admin-ui-1.png" alt="GoPress admin content workspace preview"> | <img src="docs/resources/ui_preview/admin/admin-ui-2.png" alt="GoPress admin theme settings preview"> | <img src="docs/resources/ui_preview/admin/admin-ui-3.png" alt="GoPress admin media and editing preview"> |

### Theme Gallery

| Axis Form | FloraFi |
|---|---|
| <img src="docs/resources/ui_preview/theme/axis-form.png" alt="Axis Form theme preview"> | <img src="docs/resources/ui_preview/theme/floraFi.png" alt="FloraFi theme preview"> |

| Modern Company ([live site](https://hurricanetechs.com)) | Civic Estate |
|---|---|
| <img src="docs/resources/ui_preview/theme/modern-company.png" alt="Modern Company theme preview"> | <img src="docs/resources/ui_preview/theme/civic-estate.png" alt="Civic Estate theme preview"> |

<details>
<summary>More bundled theme previews</summary>

| Atelier Slate ([live site](https://gopress.xyz)) | Terra Trail |
|---|---|
| <img src="docs/resources/ui_preview/theme/atelier-slate.png" alt="Atelier Slate theme preview"> | <img src="docs/resources/ui_preview/theme/terra-trail.png" alt="Terra Trail theme preview"> |

| GoPress Landing Indigo | GoPress Landing Rose |
|---|---|
| <img src="docs/resources/ui_preview/theme/gopress-landing-color-2.png" alt="GoPress Landing indigo theme preview"> | <img src="docs/resources/ui_preview/theme/gopress-landing-color-1.png" alt="GoPress Landing rose theme preview"> |

</details>

---

## Quick Start

### Requirements

- Go 1.25+
- PostgreSQL 14+
- Redis 7+ (optional; GoPress falls back to memory-only cache when Redis is unavailable)
- `cwebp` (optional; used for WebP variants; missing binaries fall back to JPG/PNG variants)

### Install and Run

GoPress ships with a small orchestrator CLI named `gopress`. It scans `themes/` and `plugins/` at startup, regenerates the autoload package, and runs the server. You never have to hand-edit imports when adding a theme or plugin — drop the folder in, restart with `gopress serve`, and it is picked up automatically.

The fastest way to try GoPress is the local build — no global install required.

```bash
# Clone the repository
git clone https://github.com/0xmattg/go-press.git
cd go-press

# Download dependencies
go mod download

# Build the gopress CLI into ./build/ (no global install needed)
make gopress

# Start the server. First run opens the web installer.
./build/gopress serve

# Or start with an existing site config (any flag is forwarded to cmd/server)
./build/gopress serve -config sites/localhost/config.toml

# Produce a single production binary (autoload baked in at build time)
./build/gopress build               # -> build/gopress-server
./build/gopress build -o ./myserver # custom output path
```

`make help` lists all Make targets. `./build/gopress help` lists all CLI subcommands.

#### Optional: install globally

If you plan to use GoPress regularly, install the CLI onto `$PATH` so you can drop the `./build/` prefix:

```bash
make install      # installs gopress to $GOBIN (or $GOPATH/bin)
gopress serve     # works from any directory after install
```

> **Building on a 1c1g VM?** `go build` parallelizes across all cores and can be OOM-killed on small VPS instances. Prefix with `GOFLAGS="-p=1 -v"` to force serial compilation, e.g. `GOFLAGS="-p=1 -v" make gopress`. See [installation guide](docs/guide/en/getting-started/installation.md#building-on-low-memory-machines) for details.

After startup:

| URL | Purpose |
|---|---|
| `http://localhost:8080` | Public site |
| `http://localhost:8080/admin` | Admin CMS |
| `http://localhost:8080/swagger/index.html` | API documentation |
| `http://localhost:8080/api/v1/content` | REST API |

See the full installation guide: [docs/guide/en/getting-started/installation.md](docs/guide/en/getting-started/installation.md).

---

## Documentation

The documentation lives under [`docs/guide/`](docs/guide/) and is organized as a GitBook-style guide:

| Section | Covers |
|---|---|
| [Introduction](docs/guide/en/README.md) | Positioning and design principles |
| [Getting Started](docs/guide/en/getting-started/installation.md) | Installation, configuration, and the web installer |
| [Architecture](docs/guide/en/architecture/overview.md) | Engine boot flow, content model, public authentication, URL/SEO, cache, i18n, content scope, and hooks |
| [Admin](docs/guide/en/admin/overview.md) | Admin CMS, extension points, and menu management |
| [Themes](docs/guide/en/themes/overview.md) | Creating themes, SEO integration, image pipeline, and media variants |
| [Plugins](docs/guide/en/plugins/overview.md) | Creating plugins, hook contracts, and bundled plugins |
| [Reference](docs/guide/en/reference/project-structure.md) | Project structure, table prefixes, REST API, tech stack, and roadmap |

OpenAPI files are generated from code annotations:

| File | Description |
|---|---|
| [docs/swagger.json](docs/swagger.json) | OpenAPI specification in JSON |
| [docs/swagger.yaml](docs/swagger.yaml) | OpenAPI specification in YAML |
| [docs/docs.go](docs/docs.go) | Generated Swagger Go package imported by the server entry point |

Regenerate docs with:

```bash
go run ./cmd/gendoc/
```

---

## Feature Overview

### Public Accounts and Identity

<table>
  <tr>
    <td align="center" width="50%">
      <img src="docs/resources/brand/google-g-logo.png" alt="Google G" width="46"><br>
      <strong>Google / Gmail Sign-In · Available</strong><br>
      <sub>Bundled Google OIDC plugin for Gmail and Google Workspace accounts, with Authorization Code Flow, PKCE, verified identity binding, and revocable GoPress sessions.</sub>
    </td>
    <td align="center" width="50%">
      <img src="docs/resources/brand/metamask-fox.svg" alt="MetaMask" width="50"><br>
      <strong>MetaMask Wallet Sign-In · Available</strong><br>
      <sub>Bundled EIP-4361 SIWE plugin with server-generated one-time challenges, origin and chain binding, EOA signature verification, and policy-controlled account registration.</sub>
    </td>
  </tr>
</table>

- **Provider-neutral account core** — nullable email/password credentials, external identity bindings keyed by `(provider, issuer, subject)`, policy-controlled registration and linking, and database-backed revocable sessions.
- **Admin-controlled registration policy** — independent switches for public registration, external login, external auto-registration, account linking, and a privilege-limited default role.
- **Plugin protocol boundary** — identity plugins verify OIDC, wallet signatures, or future protocols, then pass only `VerifiedIdentity` assertions to core.
- **Theme-ready helpers** — `currentUser`, `isLoggedIn`, `loginURL`, `logoutURL`, and `loginProviders` let themes render account UI without knowing which provider plugin is active.

See [Public Authentication](docs/guide/en/architecture/public-authentication.md) for the core model, Google and MetaMask setup, plugin contracts, and theme integration.

### Engine Core

- **Unified content model** — `Content` + `ContentMeta` + `ContentType` registry; core keeps `post` and `contact_message`, while themes declare custom types in `theme.toml`.
- **Config-driven content routing** — `theme.toml` `rewrite_slug` and optional `templates = { archive = "...", single = "..." }` drive archive URLs, detail URLs, sitemap entries, admin permalinks, and dynamic template resolution. `product`, `service`, and `showcase` are examples, not framework assumptions.
- **Chainable content queries** — for example: `ContentQuery.Type("product").Published().Taxonomy("category", "hepa").Paginate(1, 20)`.
- **Hook event bus** — `AddAction` / `DoAction` / `AddFilter` / `ApplyFilter`, with removable handles for clean plugin deactivation.
- **Multi-level cache** — L1 memory cache, optional L2 Redis, graceful fallback, and page-cache middleware for sub-millisecond cache hits.
- **Worker pool** — goroutine worker pool plus cron-style scheduling.
- **Core i18n** — go-i18n with three-level fallback: database override, locale file, then message ID.

### URL and SEO

- **Shared site metadata** — admin-managed `site_name`, `site_description`, and `site_timezone` are used across themes; publish times are entered and displayed in the site timezone while stored as UTC.
- **SEOBuilder** — home, archive, and single pages generate meta descriptions, canonical links, Open Graph tags, JSON-LD, and crawler-friendly favicon links.
- **`seoHeadFor` helper** — reflection-based and safe for both `gin.H` and custom structs.
- **Per-content SEO overrides** — the bundled `seo-extras` plugin adds Yoast-style fields for title, description, Open Graph image, and robots.
- **Multilingual sitemap support** — `SitemapGenerator.AddTransformer()` lets the multilingual plugin contribute `hreflang` alternates.
- **Site-scoped public artifacts** — admin-generated sitemap files and favicon assets are written under `sites/{host}/public/`, keeping multi-site deployments isolated.
- **Redirect manager** — database-backed 301/302 redirects with in-memory lookup and hit counts.

### Admin CMS

- **Data-driven CRUD** — admin list/edit screens are generated from the registered `ContentType` definitions.
- **Theme-declared content models** — `theme.toml` drives admin navigation, CRUD, REST API exposure, rewrite rules, template mapping, and menu icons.
- **RBAC** — `admin`, `editor`, `author`, and `subscriber` roles enforced throughout the admin surface.
- **List screen options and pagination** — content lists support dynamic column visibility, title search, date/taxonomy filters, and server-side pagination.
- **Mail settings and notifications** — dedicated SMTP settings page, go-mail SMTP driver with Go stdlib option, site-level `config.toml` storage for `mail.mail_key`, test emails, Gmail-friendly `587 + STARTTLS` setup, and a switch for new contact-message notifications.
- **Drag sorting and rich text** — Quill 2.0 editor, media picker, and HTML5 drag-and-drop ordering.
- **Admin extension points** — hooks such as `admin.HookContentListTabs`, `admin.HookContentPermalinkPrefix`, `admin.content_form.fields`, `admin.content.saved`, and `mail.message`.

### Themes and Plugins

- **BaseTheme runtime** — embed it to get config-driven URL resolution, dynamic archive/detail rendering, WordPress-style fallback hierarchy, and automatic SEO integration.
- **Unified FuncMap** — `BaseFuncMap()` provides `buildURL`, `archiveURL`, `contentURL`, `pageTitleFor`, `seoHeadFor`, `menuByLocation`, `isMenuURLActive`, `T`, `currentLang`, `langPrefixURL`, `renderHook`, and `responsiveImage*`.
- **Theme template slots** — `theme.head.end`, `theme.body.open`, `theme.footer.end`, and `header.nav.after` define semantic insertion points for plugins.
- **Responsive image pipeline** — uploads generate WebP and JPG/PNG variants (`thumb`, `480w`, `768w`, `1024w`, `1440w`, `full`), and templates output `<picture>` through `responsiveImage`.
- **Hot-pluggable plugins** — `Bus.AddAction/AddFilter` return handles; `Deactivate` removes hooks cleanly without restarting the process.
- **No cross-dependency between themes and plugins** — core is the only integration boundary.

### Bundled Themes

`atelier-slate` / `axis-form` (Axis Form, architecture and design) / `florafi` (FloraFi, stablecoin and fintech) / `civic-estate` / `financial-news` / `go-press-landing` / `modern-company` / `terra-trail`

See [docs/guide/en/themes/overview.md](docs/guide/en/themes/overview.md).

### Bundled Plugins

- **multilang** — WPML-style content translation, menu translation, site setting translation, language-prefixed routing, and language-aware redirects.
- **seo-extras** — Yoast-style per-content SEO overrides for title, description, Open Graph image, and robots.
- **code-snippets** — WPCode-style site-level injection for end of `<head>`, start of `<body>`, and before `</body>`.
- **gopress-analytics** — First-party self-hosted PV, UV, new-visitor, traffic-trend, and top-page analytics.
- **google-identity** — Google OIDC login and registration for Gmail and Google Workspace accounts, built on the provider-neutral public-auth core.
- **metamask-identity** — MetaMask browser-extension login and registration through EIP-4361 Sign-In with Ethereum and one-time server challenges.

See [docs/guide/en/plugins/overview.md](docs/guide/en/plugins/overview.md).

---

## Performance Targets

These are current architecture targets. Reproducible benchmark scripts, test environment notes, and public benchmark reports still need to be added before a stable release.

| Metric | Target |
|---|---|
| Page-cache hit response | < 1 ms |
| First render without cache | < 50 ms |
| Concurrent connections | 50,000+ |
| QPS with cache hit | 100,000+ |
| QPS without cache | 5,000+ |
| Idle memory usage | < 50 MB |

---

## Tech Stack

Gin / GORM / PostgreSQL / Redis / golang-jwt / Viper + TOML / log/slog / go-i18n / Quill 2.0 / swaggo/swag

See [docs/guide/en/reference/tech-stack.md](docs/guide/en/reference/tech-stack.md).

---

## Contributing

Issues and pull requests are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before contributing. The project roadmap is available at [docs/guide/en/reference/roadmap.md](docs/guide/en/reference/roadmap.md).

## License

[MIT License](LICENSE)
