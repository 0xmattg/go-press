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

`host` and `port` form the HTTP listen address. Use `release` in production;
`debug` enables verbose Gin logging for local development.

## PostgreSQL

`table_prefix` isolates core, plugin, and theme tables when sites share a
database. Pool fields control maximum open/idle connections and connection
lifetime; tune them to the PostgreSQL deployment. See
[Database Prefixes](../reference/database-prefix.md).

## Redis

Redis is optional. Remove the section or leave it unavailable to use the
in-process L1 cache only; core falls back without making the site unavailable.

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

## Runtime Files and Multiple Sites

```text
sites/
  localhost/
    config.toml
    public/            generated sitemap.xml and future public artifacts
  example.com/
    config.toml
uploads/
  YYYY/MM/             originals and generated media variants
```

Use `-config sites/example.com/config.toml` to choose a site explicitly. Keep
site configuration, credentials, uploads, and generated public artifacts out of
public source control unless a deployment policy explicitly says otherwise.
