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
  and import optional demo data. Theme package cards show where the package
  came from, mark unsigned or revoked versions, and show a newer version the
  market offers (*New version*, red for security updates); a revoked version
  cannot be switched to.
- **Theme market** — opened from the Themes page. *Browse* fetches the market
  catalog on every visit, with search, install and upgrade; when the market
  gives a theme a page of its own, the card's title, screenshot and *Details*
  open it in a new tab. When the catalog cannot be fetched it shows the error
  and a retry button, never an older list. *Installed* lists every installed
  theme package, including damaged ones, with its versions, update notices,
  rollback and uninstall. *Upload theme package* installs a `.zip`. See
  [Theme Packages](../themes/theme-packages.md#installing-and-managing-packages).
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
is handled on the Themes page rather than by editing an arbitrary option value,
and plugins are switched on the Plugins page: the settings form shows those
states read-only, and its custom-option field cannot overwrite options GoPress
manages.

**Developer options** is a separate block below the settings, shown only to
roles with the `extension.developer_mode` capability (super admins by
default). Developer mode allows installing theme packages that no trusted
market signed. Turning it on takes a confirmation of the risks and the
account's password; too many wrong passwords block the account for 15
minutes. Turning it off needs neither. While it is on, every admin page shows
a banner. `[market] lock_developer_mode` in `config.toml` keeps it off; see
[Configuration](../getting-started/configuration.md#market).

When the market revokes the version of the theme in use, the theme keeps
running and every admin page shows a banner until you switch to another theme
or upgrade.

## Notices

Notices from GoPress and from plugins appear at the top of every admin page,
below the top bar: red for critical, amber for warnings and blue for
information, with a label such as Security or Configuration. Up to three are
open; the rest fold under "N more notices".

Each notice offers one or both ways to dismiss it, for you only:

- **×** hides it for now. It comes back after the time the tooltip names
  (24 hours unless the notice sets another) if it still applies.
- **Got it** hides it for good. If its content changes enough to need
  reading again, it shows again.

Dismissals are stored on the server per user, so they follow you to other
browsers, and both actions are written to the audit log. A notice disappears
on its own when it expires or the problem it reports is fixed. Notices need
the `notice.read` capability (super admins by default) unless the notice names
another one; plugins can add notices through `admin.notices`, see
[Admin Extension Points](extension-points.md#admin-notices).

**Announcements from GoPress.** With update checks on (Settings → *Official
Updates and Beta Usage Feedback*), the daily update check can also bring
announcements from the GoPress project, such as a security advisory for the
version you run. They appear in the notice center for those with
`notice.read`, offer both **×** and **Got it**, and disappear when they expire
or when the site is upgraded past the versions they are about. The project
signs them offline and the site accepts only bundles signed with the keys
built into GoPress, so whoever controls the update server cannot write them;
a site also refuses a bundle older than the one it has. Turning update checks
off stops them.

## Account Security

**Account security**, in the top bar, is where every admin user turns on
two-step verification for their own account. After the password, signing in
then also asks for a 6-digit code from an authenticator app on their phone,
such as Google Authenticator.

- **Setting it up** — confirm your password, scan the QR code (or type the key)
  in the app, and enter the code it shows. You then get 10 recovery codes,
  shown once; download or copy them. Turning it on keeps your current session
  and signs the account out everywhere else.
- **Signing in** — after the password, enter the code from the app, or use a
  recovery code. Each recovery code works once; the notice center reminds you
  when fewer than 3 are left. Five wrong codes in 15 minutes lock the code step
  for 15 minutes.
- **Sensitive actions** — accounts with two-step verification on enter a fresh
  code to turn on developer mode, make someone a super admin, set a password
  on the Users page, reset someone's two-step verification, change the roles
  that must use it, and replace their recovery codes. Each code works once, so
  two such actions in the same 30 seconds need two codes. Recovery codes are
  accepted only to sign in and to turn two-step verification off.
- **Required roles** — those who may change settings choose, under
  **Required for roles**, roles that must use it. Accounts with those roles can
  only open the account security page until they set it up, and cannot turn it
  off. Nothing is required by default.
- **Turning it off** — takes the password and a code or a recovery code.
- **Lost phone and recovery codes** — another super admin opens the account on
  the Users page and chooses **Reset two-step verification**; the account is
  signed out everywhere and sets it up again at its next sign-in. With no one
  else to do it, run `-reset-2fa` on the server, see
  [Installation](../getting-started/installation.md#resetting-two-step-verification).

Two-step verification protects signing in to the admin. API keys in
`config.toml` and Agent credentials do not sign in, so it does not apply to
them. Turning it on or off, resets, uses of recovery codes and wrong codes are
written to the audit log. When mail is set up, the account owner gets an email
when their two-step verification is turned off or reset, a recovery code is
used, or new recovery codes are created.

## Mail Settings

Mail settings are managed on a dedicated system page instead of the general options form. SMTP transport values are saved to the active site's `config.toml` under `[mail]`; notification switches and recipient preferences are saved as options. `go-mail` is the default driver, with a `stdlib` standard-library branch available.

- **SMTP transport switch** — when disabled, notification rules stay saved but no mail is delivered.
- **SMTP key** — `mail.mail_key` is written only to the site config file. The admin form shows a placeholder when a key exists; leaving the field blank preserves it, and the clear checkbox removes it.
- **Gmail setup** — use `smtp.gmail.com`, `587`, `STARTTLS`, the Gmail address as both username and sender, and a Google App Password as the SMTP key.
- **Test email** — uses the currently saved SMTP settings so operators can verify host, port, encryption, and sender configuration.
- **Contact message notifications** — the default rule sends an async email when a `contact_message` is created. Failures are logged and do not block saving the message.

## Extension Points

Plugins can add settings pages, content form fields, save handlers, content-list tabs, admin notices, sensitive actions that need a two-step verification code, frontend hook output, SEO overrides, and sitemap transformers without modifying admin core templates directly.
