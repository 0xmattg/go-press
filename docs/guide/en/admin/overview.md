# Admin Overview

The GoPress admin is a built-in CMS interface for managing content, media, menus, themes, plugins, cache, redirects, mail, users, and system settings.

## Main Areas

- **Dashboard** — overview metrics and recent operation logs.
- **Content** — core posts and theme-defined content types.
- **Taxonomy** — categories and tags.
- **System** — menus, themes, plugins, cache, redirects, media library, mail settings, system settings, and users.

The sidebar is generated from core content types, active theme metadata, plugin capabilities, and registered admin routes.

Every protected page and state-changing handler must check its specific RBAC
capability. Built-in roles include super administrator, editor, author,
contributor, and subscriber; the super administrator receives the `*.*`
wildcard. Administrative operations are recorded in the audit log with actor,
action, resource, source IP, and time.

## Content Management

The admin CRUD surface is data-driven. A content type declared in `theme.toml` can automatically receive list, create, edit, delete, media, taxonomy, sorting, REST, and rewrite behavior depending on its `supports` and `taxonomies` settings.

The shared editor renders registered meta fields, integrates Quill 2.0, opens
the media picker for upload/search/selection, and exposes status, publication
time, taxonomy, thumbnail, comments, and sorting controls only when the content
type declares those capabilities. Types supporting `sort_order` receive a
drag-and-drop list handle backed by a protected reorder endpoint.

The editorial status selector supports `published`, `pending`, `draft`, and
`archived`. Status input is allow-listed on the server; forged values are
ignored and do not become stored workflow states, and the normal editor cannot
move an item directly to `trash`. The `pending` state is available for front-end submissions
and other review queues without introducing a business-specific content type.

Content list pages include WordPress-style **Screen Options**. The column checkboxes are generated from the current page's actual columns, including core fields, content meta fields, and taxonomies attached to that content type. The selected columns and items-per-page value are stored per list key and are applied to server-side pagination.

List search is server-side and searches titles only, matching the admin placeholder. Date and taxonomy filters are also generated from the current content type: available months come from existing rows, and the taxonomy dropdown uses the first hierarchical taxonomy attached to the type, falling back to the first taxonomy when no hierarchical taxonomy exists. Search, tabs, date filters, taxonomy filters, and pagination compose into one query so totals and page counts stay accurate.

## System Management

- **Themes** — preflight dependencies, switch the active theme, rebuild routes,
  and import optional demo data.
- **Plugins** — activate or deactivate compiled plugins, open component-owned
  settings, rebuild routes, and clear affected cache paths.
- **Menus** — manage nested items, assign theme locations, and sort entries.
- **Cache and redirects** — inspect/clear cache and maintain `301`/`302` rules.
- **Media** — upload, search, delete, and rebuild responsive variants.
- **Users and comments** — manage roles/accounts and moderate comment status.
  Clearing **Active account** is persisted explicitly; existing admin tokens
  and public sessions are rejected on their next request rather than remaining
  usable until expiry.
- **System settings** — maintain site identity, language, timezone, favicon, and
  admin preferences.
- **Agent/MCP controls** — after the disabled-by-default `gopress-mcp` plugin is
  activated, its settings page provides connection diagnostics, read-only or
  Safe Write policy, short-lived token issue/revocation, and filtered Tool
  audit. Dedicated RBAC protects the page and every custom handler. See
  [GoPress MCP (Agent Access)](../plugins/gopress-mcp.md).

## Settings

System settings are split into website settings and admin settings. Website settings affect the public site, SEO metadata, sitemap, favicon, publish-time timezone, and branding options. The `site_icon` value is the shared favicon source for all themes. `site_timezone` is the shared timezone used to parse admin publish-time inputs and format content dates in admin lists and themes; timestamps are stored in UTC. Admin settings control the CMS interface, including the admin language.

Saving website settings invalidates the relevant page cache. Theme activation
is handled on the Themes page rather than by editing an arbitrary option value.

## Mail Settings

Mail settings are managed on a dedicated system page instead of the general options form. SMTP transport values are saved to the active site's `config.toml` under `[mail]`; notification switches and recipient preferences are saved as options. `go-mail` is the default driver, with a `stdlib` standard-library branch available.

- **SMTP transport switch** — when disabled, notification rules stay saved but no mail is delivered.
- **SMTP key** — `mail.mail_key` is written only to the site config file. The admin form shows a placeholder when a key exists; leaving the field blank preserves it, and the clear checkbox removes it.
- **Gmail setup** — use `smtp.gmail.com`, `587`, `STARTTLS`, the Gmail address as both username and sender, and a Google App Password as the SMTP key.
- **Test email** — uses the currently saved SMTP settings so operators can verify host, port, encryption, and sender configuration.
- **Contact message notifications** — the default rule sends an async email when a `contact_message` is created. Failures are logged and do not block saving the message.

## Extension Points

Plugins can add settings pages, content form fields, save handlers, content-list tabs, frontend hook output, SEO overrides, and sitemap transformers without modifying admin core templates directly.
