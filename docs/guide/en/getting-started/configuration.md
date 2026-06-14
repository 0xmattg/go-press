# Configuration

GoPress reads TOML configuration files from the `sites/` directory. The installer writes a site-specific file such as `sites/localhost/config.toml`; production deployments should create one file per hostname.

## File Discovery

Configuration is resolved by host:

```text
sites/{host}/config.toml
sites/default/config.toml
config/config.toml
```

This makes local development, preview environments, and production domains share the same binary while keeping runtime settings isolated.

## Database

```toml
[pg]
host = "localhost"
port = 5432
user = "postgres"
password = ""
database = "gopress"
schema = "public"
table_prefix = "gp_"
```

`table_prefix` is important when multiple GoPress instances share one PostgreSQL database. Core tables, plugin tables, and theme tables are all derived from this prefix.

## Server

```toml
[server]
addr = ":8080"
mode = "debug"
```

`addr` controls the HTTP listener. `mode` is passed to Gin and should normally be set to `release` in production.

## Site

```toml
[site]
name = "My GoPress Site"
url = "https://example.com"
theme = "atelier-slate"
language = "en"
timezone = "UTC"
```

The active theme is loaded by slug. Public site language and admin interface language can also be managed from the admin UI after installation.

`timezone` should be an IANA timezone name such as `UTC`, `Asia/Shanghai`, or `America/New_York`, or `Local` to follow the server timezone. Admin publish-time inputs are parsed in the site timezone and stored as UTC timestamps; admin and frontend displays convert them back through the same site timezone. Existing sites without this key fall back to the server local timezone until an explicit value is saved in System Settings.

## Cache

```toml
[cache]
enabled = true
redis_addr = ""
```

When Redis is not configured, GoPress keeps the in-process memory cache path active and degrades gracefully.

## Mail

```toml
[mail]
driver = "go-mail"
enabled = false
host = "smtp.example.com"
port = 587
encryption = "starttls" # starttls / ssl / none
username = "smtp-user"
mail_key = "smtp-password-or-app-key"
from_email = "no-reply@example.com"
from_name = "My GoPress Site"
reply_to = ""
timeout_seconds = 10
```

Mail transport settings are site-scoped. The admin **Mail Settings** page writes them to the active site's `config.toml`, which is saved with `0600` permissions. `mail_key` is never echoed back in the admin form; leaving the password field blank keeps the existing value.

`driver` selects the SMTP implementation. `go-mail` is the default driver; `stdlib` uses the Go standard-library SMTP branch. `enabled` is the transport-level switch. Notification rules can stay enabled while SMTP delivery is disabled. `encryption` accepts `starttls`, `ssl`, or `none`. New contact-message notifications use the sender email as Reply-To when available.

For Gmail, use `smtp.gmail.com`, port `587`, encryption `starttls`, and set both `username` and `from_email` to the Gmail address. Store the Google App Password in `mail_key`; do not use the normal Google account password.

## Runtime Files

- `uploads/` stores uploaded media and generated variants.
- `sites/{host}/config.toml` stores site configuration.
- `sites/{host}/public/` stores site-scoped generated public files such as `sitemap.xml`.

For public repositories, site-specific configuration and generated runtime files should stay ignored by Git.
