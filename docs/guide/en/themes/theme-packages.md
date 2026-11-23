# Theme Packages

A theme package is a theme without Go code: a plain `.zip` file holding `theme.toml`, templates, styles, images, fonts, locale files and optional demo data. A theme package does not need GoPress to be recompiled, so it can be distributed to other sites.

This page covers the package format, the tools to check, build and sign packages, the [local preview](#local-preview) on a development site, and how sites [install and manage](#installing-and-managing-packages) packages. To list a package in the official market, see [Publishing to the GoPress Market](publishing.md).

## Go themes and theme packages

| | Go theme | Theme package |
|---|---|---|
| Code | Go code: custom routes, handlers, template functions | Templates and static files only |
| Template functions | Core's functions plus the theme's own | Core's functions only, including the [read-only data functions](creating-themes.md#read-only-data-functions) |
| Template layout | Page bundles or the classic hierarchy | Page bundles only |
| Settings page | `[[settings]]` or a custom admin template | `[[settings]]` only; core generates the page |
| Scripts | Allowed | **Not allowed**; use core's [interaction components](interactions.md) |
| External resources | Unrestricted | Must be declared in `[[external]]` |

**Theme packages can do less than Go themes for now.** A package can only use what core provides: the data functions, the interaction components, and account features such as the profile form (`accountProfile`), public badges and account actions. Some features still need Go code, for example forms that post comments or contact messages, so a package can list approved comments but cannot offer a comment form yet. We keep moving common needs into core, so theme packages, and with them the themes in the market, can do more with each release.

## Layout

```text
aurora-1.2.0.zip
├── theme.toml                 required
├── templates/
│   ├── layouts/base.tmpl      required, defines {{define "base"}}
│   ├── partials/*.tmpl        optional
│   └── pages/*.tmpl           home page required, see Templates
├── static/                    styles, images, fonts
├── locales/<lang>.json        optional, front-end strings
├── locales/admin/<lang>.json  optional, settings page strings
├── demo/                      optional demo data (demo/data/seed.toml and images)
├── screenshot.png             optional, named by [theme] screenshot
├── README.md, CHANGELOG.md, LICENSE   optional
└── signatures/                added by signing: publisher.json, market.json
```

- Templates go directly in `templates/layouts/`, `templates/partials/` or `templates/pages/` and end in `.tmpl`. Only `layouts/base.tmpl` is loaded from `layouts/`.
- Template overrides for plugin pages (`templates/<namespace>/`) are not supported: plugin pages use the plugin's own fragment inside your `base` layout.
- There is no `templates/admin/`: the settings page is generated from `[[settings]]`; see [Theme Settings](creating-themes.md#theme-settings).
- The top level holds only the directories and files listed above.
- A locale file is a JSON object of strings.

## theme.toml

```toml
[theme]
slug = "aurora"
name = "Aurora"
version = "1.2.0"
description = "A clean magazine theme."
author = "Example Studio"
screenshot = "screenshot.png"

[requires]
core = ">=0.7.23"

[[settings]]
key = "hero_title"
type = "string"
label = "Hero title"
default = "Welcome"
translatable = true

[[external]]
host = "fonts.googleapis.com"
kinds = ["style"]
purpose = "Web font stylesheet"

[[external]]
host = "fonts.gstatic.com"
kinds = ["font"]
purpose = "Web font files"
```

A theme package must set:

| Field | Requirement |
|---|---|
| `[theme] slug` | The theme's identifier: 2–64 lowercase letters and digits, words joined by single hyphens. It must not clash with a theme or plugin the site already has |
| `[theme] name` | Display name |
| `[theme] version` | Semantic version, such as `1.2.0` |
| `[requires] core` | Supported GoPress versions, such as `">=0.7.23"` |

`[[settings]]`, `[[setting_groups]]`, `[[menu_locations]]`, `[[page_templates]]`, `[[content_types]]` and `[requires] plugins` work as they do for Go themes. Every page a `[[page_templates]]` entry names must exist in `templates/pages/`.

`[theme] slug` and `[[external]]` are optional for Go themes, whose slug is the name they register with.

## External resources: `[[external]]`

Declare every kind of resource the theme loads from another host:

| Key | Meaning |
|---|---|
| `host` | Lowercase domain name, such as `fonts.gstatic.com`. A `*.` prefix covers every subdomain: `*.example.com` matches `img.example.com` but not `example.com` itself. No scheme, port, path or IP address |
| `kinds` | What the theme loads from the host; see below |
| `purpose` | Required, up to 200 characters. Administrators see it when they install or switch to the theme |

At most 20 hosts, each declared once.

| Kind | Covers |
|---|---|
| `style` | `<link rel="stylesheet">`, CSS `@import` |
| `font` | `url()` inside `@font-face`, `<link rel="preload" as="font">` |
| `image` | `<img>`, `srcset`, `<video poster>`, CSS `url()`, icons, SVG `<image>`, and defaults of image settings |
| `media` | `<video>`, `<audio>`, `<source src>`, `<track>` |
| `frame` | `<iframe>` |

There is no `script` kind: theme packages cannot load scripts.

Only URLs that **load** something are checked. Links such as `<a href>` and `<form action>` need no declaration. URLs a template action builds (an image from a setting, for example) cannot be checked when the package is built; the site's Content-Security-Policy restricts them to the `[[external]]` hosts. A resource loaded over `http://` gets a warning, because browsers block it on HTTPS sites.

## No scripts

Theme packages cannot contain scripts in any form. The check rejects:

- `.js`, `.mjs` and `.cjs` files;
- `<script>` elements, except inline `<script type="application/ld+json">` structured data without `src`;
- `on*` event handler attributes such as `onclick`;
- `javascript:` and `vbscript:` URLs. Links may use `http`, `https`, `mailto`, `tel`, `sms` or a relative URL; images may also use `data:`;
- `<iframe srcdoc>`, `<object>`, `<embed>`, `<applet>`, `<base>` and `<meta http-equiv="refresh">`;
- CSS `expression()`, `behavior:` and `-moz-binding`, and `url()` values other than `http`, `https` and `data`;
- `<foreignObject>`, DTD declarations and `xml-stylesheet` instructions in SVG.

The rules also apply when template syntax splits the text. The check removes actions and `{{if}}` branches and checks the text that is left, so `<scr{{""}}ipt>` and `java{{""}}script:` are still found. Element and attribute names must not be built from actions or branches, as in `<h{{.Level}}>` or `<a o{{"nclick"}}=…>`; write the full name in each branch instead. An attribute that `{{if}}` switches on as a whole is fine, such as `<input {{if .Checked}}checked{{end}}>`, and so are actions in `data-` and `aria-` attribute names.

For menus, dialogs, carousels and other common interactions, use the [interaction components](interactions.md) that come with GoPress: call `{{uiScript}}` in the layout and configure them with `data-gp-*` attributes. The script tag that `{{uiScript}}` outputs is not part of the package text, so the rules above do not apply to it. Simpler interactions need only HTML and CSS, such as `<details>` and `:focus-within`.

## Content-Security-Policy

While a theme package is the active theme, every front-end response carries a
`Content-Security-Policy` header, so a script that got into a package anyway
would not run:

| Directive | Allows |
|---|---|
| `script-src` | This site, plus sources that active plugins declare. Never inline scripts in general or `eval` |
| `style-src` | This site, inline styles, and the hosts the theme declares with the `style` kind |
| `font-src` | This site, `data:` fonts, and the hosts declared with the `font` kind |
| `img-src`, `media-src`, `frame-src` | This site and any `https:` source (`data:` for images too), because images, videos and embeds in content can come from any site |
| `connect-src` | This site, plus sources that active plugins declare |
| `object-src`, `base-uri` | Nothing, and this site |

Admin pages, the JSON API and the API explorer do not get it, and themes built
into GoPress get no policy at all. Declare every host your templates and CSS
load styles or fonts from in `[[external]]`; anything else is blocked. When
you preview a package with `gopress serve -theme-package`, the policy applies,
and the browser console lists what it blocks.

## Limits

| Item | Limit |
|---|---|
| `.zip` file | 10 MB |
| Total unpacked size, counted as read | 25 MB |
| Files | 1000 |
| One file | 5 MB |
| One template | 512 KB |
| Path inside the package | 200 bytes |

Also:

- Paths use `/`, are relative, and contain no `..`, `:` or control characters; no segment starts with `.`.
- No symbolic links or other special files, no encryption, and only the store and deflate compression methods.
- No duplicate entries, including names that differ only in case, and no name that is both a file and a directory.

## Allowed files

| Type | Extensions | Where |
|---|---|---|
| Templates | `.tmpl` | `templates/layouts/`, `templates/partials/`, `templates/pages/` |
| Styles and pages | `.css`, `.html` | `static/` and other content directories |
| Images | `.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.avif`, `.svg`, `.ico` | `static/`, `demo/`; a screenshot may be at the top level |
| Fonts | `.woff`, `.woff2`, `.ttf`, `.otf` | `static/` |
| Data | `.json` | `locales/`, `static/` and others; `.toml` only as the top-level `theme.toml` and under `demo/` |
| Text | `.md`, `.txt`, `LICENSE` | at the top level only `README.md`, `CHANGELOG.md` and `LICENSE` |

Text files must be UTF-8. Images and fonts must start with the signature of their format.

## Templates

- Each `pages/*.tmpl` is parsed together with `layouts/base.tmpl` and every `partials/*.tmpl`, and the `base` template is executed, as for Go themes' page bundles; see [Creating Themes](creating-themes.md#template-hierarchy).
- Pages are chosen by name, as for Go themes:

  | Page | Names, first match wins |
  |---|---|
  | Home page (required) | `front-page`, `home`, `index` |
  | Not found, status 404 | `404`; without it the site answers with plain text |
  | Content and archives | `single-<type>-<slug>`, `single-<type>`, `single`, `archive-<type>`, `archive` and the other names in [Template Hierarchy](creating-themes.md#template-hierarchy); without a match core renders a built-in page |

  A page's data is the same as for Go themes: `.Item` on detail pages, `.Items` on archives, and `.Settings`, `.SEO` and `.Ctx` everywhere.
- A theme package cannot register routes, so a form that needs its own handler (a contact form, for example) is not possible yet. Archive pages show 20 items per page.
- Only core's template functions are available. A Go theme's own function fails with `template.parse`, naming the function.
- The check runs every page through the `html/template` escaper. When, for example, the two branches of an `{{if}}` end in different contexts (one inside an attribute value, the other outside the tag), the page would fail on every request, and the check reports `template.escape`.

## Static files

Files in `static/` are served at `/static/...`, as for Go themes. Opening an `.svg`, `.html` or `.xml` file of a theme package directly in the browser downloads it instead of displaying it, the same as uploaded files. Using an SVG in `<img>` or a CSS `url()` is not affected.

## Demo data

A package can ship demo data in `demo/data/seed.toml`, in the same format as Go themes; administrators import it from the Themes page. Because the data comes from a third party, the import of a theme package's demo data is stricter:

- `[[settings]]` may only set settings the theme declares in `[[settings]]` of `theme.toml`, and each value must be valid for its type;
- an `image_url` is either a file of the package, written as `/static/...`, or an `https://` URL on a host declared in `[[external]]` with the `image` kind. The site does not download remote images; it links to them;
- content bodies are cleaned like content saved in the editor.

When any rule fails, nothing is imported and the error lists every problem.

## Local preview

Run a package on your development site:

```bash
gopress serve -theme-package ./aurora
```

- The path is a package source directory or a `.zip`. Repeat the flag to load several packages.
- The package is checked exactly like an installed one, with the site's GoPress version for `[requires] core`; a package with problems is not loaded, and the Themes page and the server log list why. Signatures are not checked.
- The theme appears on the Themes page; switch to it there. Restart the server after changing files.
- The flag is refused when `server.mode` is `"release"`.
- When you start without the flag again, the site uses another theme until the package is back, and the admin shows a notice.

## Installing and managing packages

Sites install theme packages from the admin: **Themes → Theme market**. Only
roles with the `extension.*` capabilities can do it (super admins by
default): `extension.read` to open the market, `extension.install` to
install, upgrade, upload and roll back, `extension.delete` to uninstall.
Installing a package does not activate it; switch to it on the Themes page.

**From the market.** The market page fetches the catalog on every visit and
installs or upgrades to the newest version that suits the site's GoPress.
Before anything is stored, GoPress checks that the download is exactly what
the signed catalog describes (identity, size, content digest, external
hosts), then checks the package with its own rules, as for a local preview.
The install dialog lists the hosts the theme loads resources from. Versions
the market revoked or withdrew cannot be installed.

**Upload.** *Upload theme package* installs a `.zip`:

- A package with a valid market countersignature (`signatures/market.json`
  from a trusted market) is installed directly.
- Any other package (unsigned, signed only by its publisher, or changed
  after signing) needs **developer mode**, which a super admin turns on in
  Settings → Developer options. The admin then shows the package's name,
  version, content digest and external hosts, and installs it only after you
  confirm that exact package. It is labelled *Unsigned* everywhere.
- Turning developer mode off keeps installed unsigned packages working, but
  no new unsigned package or version can be installed. On production sites,
  set `[market] lock_developer_mode = true`; see
  [Configuration](../getting-started/configuration.md#market).

The package checks are the same in every case: developer mode lifts only the
signature requirement.

**Upgrades and rollback.** Upgrading the active theme replaces it at once. If
the new version needs plugins the site does not have, the upgrade is refused;
if it cannot be activated, the previous version stays current. Each theme
keeps its current version and the three most recently installed others; the
*Installed* tab rolls back to any of them after checking the stored file
again.

**Update notices.** While the update check setting is on (Settings →
*Official Updates and Beta Usage Feedback*), GoPress also asks the market,
about once a day, whether the themes installed from it have newer versions;
it sends only each theme's slug and installed version. A newer version shows
as *New version x.y.z* on the theme's card and in the *Installed* tab, in red
for security updates and for versions the market no longer supports, and
links to the theme in the market, where you upgrade as usual. Packages
installed by upload are not checked.

**Uninstall.** The theme in use cannot be uninstalled: switch first.
Uninstalling deletes every version and its file, and keeps the theme's
settings and any demo content it imported, so installing it again restores
its settings.

**Revocations.** When the market revokes a version, the site marks it
*Revoked* as soon as it fetches that catalog (opening the market page or
installing); switching themes, rolling back and uploading also check the
last catalog fetched, without going online. A revoked version cannot be
switched to, rolled back to or installed. If it is the theme in use, it keeps running and every admin page
shows a banner until you switch or upgrade.

Installed packages are stored as files in the site directory; back them up
with the database (see [Runtime Files](../getting-started/configuration.md#runtime-files-and-multiple-sites)).

## Command line

These commands work outside a GoPress checkout:

```bash
gopress pkg check ./aurora
```

```bash
gopress pkg build ./aurora
```

```bash
gopress pkg sign -key publisher.key aurora-1.2.0.zip
```

```bash
gopress pkg verify aurora-1.2.0.zip
```

- **`check`** checks a directory or a `.zip` and lists every problem by rule. It exits 1 when there are problems. With `-core 0.7.23` it also checks that `[requires] core` supports that version.
- **`build`** checks a directory and, if it passes, packs it into `<slug>-<version>.zip` (`-out` sets another name). Paths starting with `.` (`.git`, `.DS_Store`) and `signatures/` are left out. The same files always give a byte-identical archive.
- **`sign`** signs a directory or a `.zip` and writes `signatures/publisher.json` into it. Signature files are not part of the content digest, so signing does not change the digest.
- **`verify`** and **`digest`** verify signatures and print the content digest.

Build first, then sign the archive.

## Rule IDs

Every problem carries a stable rule ID:

| ID | Meaning |
|---|---|
| `zip.size` | The archive is larger than 10 MB |
| `entry.special`, `entry.encrypted`, `entry.method`, `entry.duplicate`, `entry.corrupt` | Special file, encryption, unsupported compression, duplicate name, unreadable entry |
| `path.encoding`, `path.unsafe`, `path.hidden`, `path.length` | Path encoding, unsafe path, hidden file, path too long |
| `size.total`, `size.files`, `size.file`, `size.template` | Total size, file count, one file, one template over the limit |
| `file.type`, `file.place`, `file.content`, `file.json`, `file.toml` | Type not allowed, wrong place, content does not match the type, invalid JSON or TOML |
| `script.file`, `script.tag`, `script.handler`, `script.url`, `script.css`, `script.embed`, `script.base`, `script.refresh`, `script.svg`, `script.dynamic` | Scripts of each kind; `script.dynamic` is an element or attribute name built by actions |
| `template.admin`, `template.place`, `template.layout`, `template.pages`, `template.home`, `template.base` | Template layout; `template.home` is a package without a home page |
| `template.parse`, `template.escape`, `template.complex` | Parse error, escaping error, page too large once expanded |
| `manifest.missing`, `manifest.syntax`, `manifest.slug`, `manifest.reserved`, `manifest.name`, `manifest.version`, `manifest.core`, `manifest.core-unmet`, `manifest.requires`, `manifest.settings`, `manifest.page-template`, `manifest.screenshot` | `theme.toml` requirements |
| `external.invalid`, `external.undeclared`, `external.kind` | Invalid `[[external]]`, undeclared host, host not declared for that kind |

Warnings do not fail the check: `template.unused` (files in `layouts/` other than `base.tmpl` are not loaded), `file.shadowed` (files under `static/admin/`, `static/uploads/` or `static/gopress/`, paths GoPress serves itself, can never be requested), `manifest.unknown` (unknown `theme.toml` key), `external.insecure` (an `http://` resource) and `source.hidden` (hidden paths left out of the build).
