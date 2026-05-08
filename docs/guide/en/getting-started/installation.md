# Installation

GoPress can be started directly from source during development. In production, it is normally built as a Go binary and run with PostgreSQL, with Redis enabled only when L2 cache is needed.

## Requirements

- Go 1.25 or newer.
- PostgreSQL 13 or newer.
- Redis is optional.
- `cwebp` is optional and only required for WebP media variants.

## Clone and Run

```bash
git clone https://github.com/0xlostpixel/go-press.git
cd go-press
go mod download
go run ./cmd/server
```

Open `http://localhost:8080/install` on first run. The installer verifies the PostgreSQL connection, writes the site configuration, initializes tables, creates the administrator account, and switches the current process to the live site after setup.

## Build a Binary

```bash
go build -o gopress ./cmd/server
./gopress
```

The service discovers site configuration from `sites/{host}/config.toml`. For local development the default host is usually `localhost`.

## First-run Installer

The web installer has three stages:

1. Database connection and table prefix.
2. Site name, default theme, admin account, and interface language.
3. Configuration write, migrations, seed data, and live-site switch.

If the target database does not exist, GoPress attempts to connect to `postgres` or `template1` with the same account and create it automatically.

## Common Development Commands

```bash
# Run the server
go run ./cmd/server

# Generate Swagger docs
go run ./cmd/gendoc

# Run tests
go test ./...
```

## After Installation

- Visit `/admin` to manage content, themes, plugins, media, users, and settings.
- Use **System Settings** to set site name, site description, site language, admin language, favicon, and Powered by GoPress display.
- Use **Themes** to activate a theme and import demo data.
- Use **Plugins** to enable multilingual support, SEO extras, code snippets, or custom plugins.

