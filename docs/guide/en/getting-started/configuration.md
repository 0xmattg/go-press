# Configuration

GoPress uses TOML and keeps one runtime configuration per site. Multiple sites
may share PostgreSQL when each uses a different table prefix.

## File Discovery

The runtime path is resolved in this order:

```text
-config <path>                  explicit flag, highest priority
sites/{first-host}/config.toml  first discovered site configuration
sites/default/config.toml       fallback path
```

`config/config.toml` is a source template, not a runtime fallback. Its
`jwt_secret` is intentionally blank. The web installer writes a site-specific
file such as `sites/localhost/config.toml` with `0600` permissions.

## Complete Example

```toml
[site]
name = "My Website"
url = "https://example.com"
language = "en"
timezone = "UTC"
theme = "mono-journal"

[server]
host = "0.0.0.0"
port = 8080
mode = "release"
# trusted_proxies = ["127.0.0.1", "::1"]  # reverse proxies to trust; empty = loopback only

[pg]
user = "postgres"
password = "postgres"
hostname = "localhost"
port = "5432"
database = "my_website"
schema = "public"
table_prefix = "gp_"
version = "v0"
max_open_conns = 20
max_idle_conns = 10
conn_max_lifetime = "30m"

[redis]
host = "localhost"
port = 6379
password = ""
db = 0
# namespace = "my-site"     # optional cache namespace when sites share one Redis DB

[cms]
jwt_secret = ""             # required; installer generates a unique value
jwt_expire_hours = 24
upload_dir = "uploads"
upload_max_size_mb = 10

[mail]
driver = "go-mail"
enabled = false
host = "smtp.example.com"
port = 587
encryption = "starttls"     # starttls / ssl / none
username = "smtp-user"
mail_key = "smtp-password-or-app-key"
from_email = "no-reply@example.com"
from_name = "My Website"
reply_to = ""
timeout_seconds = 10

[install]
completed = true
require_token = false       # true: the web installer asks for a one-time token

# [market]                  # optional; see Market below
# lock_developer_mode = true
```

## Site

- `name` and `url` provide the static SEO baseline. Runtime `site_name` and
  `site_description` options from System Settings override rendered metadata.
- `language` is the default locale and multilingual fallback.
- `timezone` accepts an IANA name such as `UTC`, `Asia/Shanghai`, or
  `America/New_York`, or `Local`. Publish-time input is interpreted in this
  timezone, stored in UTC, and converted back for admin and frontend output.
- `theme` is the active theme slug.

## Server

`host` and `port` form the HTTP listen address. Behind a reverse proxy on the
same machine (such as Nginx), prefer `host = "127.0.0.1"` so only the proxy can
reach the application port. Use `release` in production; `debug` enables
verbose Gin logging, including the route table on every router rebuild, and is
meant for local development.

`trusted_proxies` lists the IPs or CIDRs of reverse proxies whose
`X-Forwarded-For` / `X-Real-IP` headers are used to identify the client. Every
other request is attributed to its TCP peer address. **When empty, only
loopback (`127.0.0.1`, `::1`) is trusted**, which fits a reverse proxy running
on the same host. Invalid entries are logged and fall back to loopback-only, so
a typo never reopens spoofing.

The client IP drives the admin login limiter, REST API rate limiting, identity
plugin limiters, audit logs, and analytics. Trusting the wrong peers lets
clients forge their IP to bypass those limits, so:

- Reverse proxy on the same host: keep the default. In Nginx, overwrite the
  forwarding headers with the connection address instead of appending to them:

  ```nginx
  proxy_set_header X-Real-IP $remote_addr;
  proxy_set_header X-Forwarded-For $remote_addr;
  ```

- Reverse proxy on another host: add its private address, for example
  `["10.0.0.5"]`.
- A CDN in front: also add the CDN's origin-pull ranges. GoPress walks
  `X-Forwarded-For` from right to left, skips trusted proxies, and uses the
  first untrusted address as the client IP.

## PostgreSQL

`table_prefix` isolates core, plugin, and theme tables when sites share a
database. Pool fields control maximum open/idle connections and connection
lifetime; tune them to the PostgreSQL deployment. See
[Database Prefixes](../reference/database-prefix.md).

## Redis

Redis is optional. Remove the section or leave it unavailable to use the
in-process L1 cache only; core falls back without making the site unavailable.

`namespace` is optional. Every Redis cache key starts with
`gopress:<namespace>:`, and admin cache clearing and page-cache invalidation
delete only keys under that namespace. When it is empty, core derives a stable
short hash (for example `s1a2b3c4d5e6f`) from the `[pg]` `hostname`, `port`,
`database`, `schema`, and `table_prefix`, so sites with different data never
share a namespace. Explicit values keep only letters, digits, `.`, `-`, and `_`,
up to 64 characters.

Several sites can therefore share one Redis database: pages with the same path
and language stay separate, and clearing one site's cache leaves the others
intact. Set `namespace` explicitly if it must stay the same when database
connection settings change, for example after moving the database host.

## CMS and Security

- `jwt_secret` signs admin sessions and API bearer tokens. It must be a unique
  random value for every site. The installer generates one; the server refuses
  to start when it is empty or equals the shipped legacy placeholder. Generate
  one manually with `openssl rand -base64 32`.
- `jwt_expire_hours` controls token lifetime. Disabled users and role changes
  take effect on the next request because account state is revalidated against
  the database.
- `upload_dir` is the media root; uploads are grouped by year and month.
- `upload_max_size_mb` limits each uploaded file.

Core also enables these protections without extra configuration:

- Admin cookies use `HttpOnly` and `SameSite=Lax`; `Secure` is added when the
  site URL uses HTTPS.
- State-changing admin and plugin-admin requests enforce same-origin
  `Origin`/`Referer` checks.
- Failed admin logins are rate-limited per source IP and recorded in the audit
  log.
- Client IPs cannot be spoofed: `X-Forwarded-For` / `X-Real-IP` are honored
  only from `[server].trusted_proxies` (loopback by default), so direct clients
  cannot forge headers to evade the limits above.
- Public user sessions are database-backed and revalidate the active account on
  every request. Disabling the account revokes the stored session and clears
  its cookie on the next request.
- Uploaded SVG, HTML, and XML documents are served as attachments with a
  sandbox CSP to prevent script execution in the site origin.

## Mail

Mail transport is site-scoped. The admin **Mail Settings** page writes this
section while preserving `0600` file permissions; `mail_key` is never echoed
back, and leaving its form field blank keeps the stored value.

`driver` accepts the default `go-mail` implementation or `stdlib`.
`enabled` is the transport-level switch, independent of saved notification
rules. For Gmail, use `smtp.gmail.com`, port `587`, `starttls`, the Gmail address
for both username and sender, and a Google App Password in `mail_key`.

## Install

`completed` is set by the web installer when it finishes; while it is `false`
the server runs the installer instead of the site.

`require_token = true` makes the installer ask for a one-time install token on
its first screen. The token is printed to the server log when the installer
starts and saved as `install-token` next to `config.toml` (permissions `0600`);
it is removed once the site is installed. It is off by default, so set it in a
config you create before the first start. See
[Keep the Installer Private](installation.md#keep-the-installer-private).

## Market

The `[market]` section is optional. Without it the site uses the official
theme market built into this release. The official market opens once its
signing keys are published; until then the admin's theme market page reports
that no market is available, and theme packages can still be uploaded.

```toml
[market]
# A self-hosted market instead of the official one: set all three or none.
endpoint     = "https://market.example.com/api/market/v1"
catalog_keys = ["<base64 Ed25519 public key>"]
package_keys = ["<base64 Ed25519 public key>"]

# Keep developer mode off whatever the admin says.
lock_developer_mode = true
```

- `endpoint`, `catalog_keys`, `package_keys` — point the site at a
  self-hosted market. They replace the official market as a whole, so set all
  three together; a section with only some of them stops the server from
  starting. `catalog_keys` verify the market's signed catalog and
  `package_keys` its countersignature on each package; each list holds up to
  4 keys, and a key may not appear in both. The endpoint must use HTTPS
  (plain HTTP only on a loopback address, for local testing).
- `lock_developer_mode` — keeps developer mode off: the admin cannot turn it
  on, and a mode turned on earlier counts as off. Set it on production sites,
  so a stolen admin session cannot allow packages without a market signature.
  It does not count as configuring a market.

A self-hosted market serves its catalog at `<endpoint>/catalog?schema=1`,
signed with `gopress pkg catalog sign`, and each package at the
`download_url` of its catalog entry. Downloads use HTTPS, follow no
redirects, and must be exactly the size the catalog lists, so link to the
file itself. See [Theme Packages](../themes/theme-packages.md#installing-and-managing-packages).

A self-hosted market also answers update checks at `POST <endpoint>/updates/check`.
While the update check setting is on (Settings → *Official Updates and Beta
Usage Feedback*), the site sends the themes it installed from that market
with the market's signature, and no installation identifier:

```json
{
  "protocol_version": 1,
  "targets": [
    { "kind": "theme", "slug": "aurora", "current_version": "1.2.0" }
  ]
}
```

The market answers with the themes that have a newer version:

```json
{
  "protocol_version": 1,
  "ttl_seconds": 86400,
  "updates": [
    {
      "kind": "theme",
      "slug": "aurora",
      "latest_version": "1.3.0",
      "severity": "security",
      "release_url": "https://market.example.com/themes/aurora/1.3.0"
    }
  ]
}
```

- `severity` is `security`, `unsupported` (the installed version is no longer
  supported) or left out for a normal update. `release_url` is optional and
  must use HTTPS.
- Entries for themes the site did not ask about, or with an invalid version,
  are ignored. `ttl_seconds` is how long the answer holds, from 6 hours to
  7 days.
- The answer only shows an update notice; upgrading still goes through the
  signed catalog. A market without this endpoint shows no notices, and the
  site's other update checks carry on.

## Runtime Files and Multiple Sites

```text
sites/
  localhost/
    config.toml
    public/            generated sitemap.xml and future public artifacts
    packages/
      archives/        installed theme packages (their original .zip files)
  example.com/
    config.toml
uploads/
  YYYY/MM/             originals and generated media variants
```

Back up each site's `packages/` directory together with its database:
installed theme packages are files, so a database dump does not contain
them. A package installed from the market can be installed again while the
market still offers that version; an uploaded package exists only on its
site.

Run one GoPress process per site. Package installs, like other runtime
changes such as options and menus, are not yet shared between processes.

Use `-config sites/example.com/config.toml` to choose a site explicitly. Sites
may share one PostgreSQL database through different table prefixes and one
Redis database through separate cache namespaces. Keep
site configuration, credentials, uploads, and generated public artifacts out of
public source control unless a deployment policy explicitly says otherwise.
