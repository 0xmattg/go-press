# GoPress Documentation

GoPress is a modern content management framework and CMS engine written in Go. It keeps the proven ideas of classic CMS platforms—content modeling, theme rendering, plugin extension, SEO, media handling, and an admin UI—while rebuilding the runtime around Go's deployment model and engineering constraints. A protocol-neutral Core Agent layer and an optional read-only-by-default MCP adapter allow remote agents to use governed site tools without bypassing scopes, RBAC, ownership, or audit.

This documentation is available in English and Simplified Chinese.

- [English documentation](en/README.md)
- [简体中文文档](zh-CN/README.md)

## Documentation Structure

- **Getting Started** — installation, configuration, and first-run setup.
- **Architecture** — engine lifecycle, content scope, hooks, caching, i18n, URL/SEO, public authentication, and comments.
- **Admin** — the built-in CMS admin, menu management, and extension points.
- **Theme Development** — theme registration, dependencies, templates, media helpers, SEO integration, and responsive image variants.
- **Plugin Development** — plugin lifecycle, settings pages, multilingual content, SEO extensions, code snippets, analytics, external identity, and protocol adapters.
- **Agent and MCP** — [Core Agent architecture, governed Tool execution, security, extensions, operations, and testing](en/agent/overview.md), plus the optional [MCP adapter setup](en/plugins/gopress-mcp.md).
- **Commerce** — the opt-in e-commerce module: core contracts, catalog, cart, checkout, orders, inventory, payment gateways (including the PayPal satellite), and shop-theme integration.
- **Reference** — project structure, database prefixes, REST API, technology stack, and roadmap.
