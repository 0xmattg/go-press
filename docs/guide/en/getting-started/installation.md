# Installation

GoPress can be started directly from source during development. In production, it is normally built as a Go binary and run with PostgreSQL, with Redis enabled only when L2 cache is needed.

## Requirements

- Go 1.25 or newer.
- PostgreSQL 13 or newer.
- Redis is optional.
- `cwebp` is optional and only required for WebP media variants.

## The `gopress` CLI

GoPress ships with a small orchestrator binary called `gopress` that wraps the server entry point. It auto-discovers themes and plugins on every start, so you never edit `cmd/server/main.go` by hand when adding extensions.

A directory under `themes/` or `plugins/` is picked up when it contains both:

- a marker file at its root: `theme.toml` for themes, `plugin.toml` for plugins
- at least one non-test `.go` file at its root

The CLI exposes three subcommands:

| Command | What it does |
|---|---|
| `gopress serve [flags...]` | Regenerate the autoload package, then run the server. Any flag is forwarded to `cmd/server` (e.g. `-config`, `-seed`). Signals are forwarded to the child process for graceful shutdown. |
| `gopress build [-o path]` | Regenerate the autoload package, then `go build` a single server binary. Default output is `build/gopress-server`. |
| `gopress gen` | Regenerate the autoload package only — useful in IDEs or CI hooks. |

## Clone and Run

The fastest path to a running GoPress is a local `make gopress` build — no global install needed for a first look.

```bash
git clone https://github.com/0xmattg/go-press.git
cd go-press
go mod download

# Build the gopress CLI into ./build/ (no global install).
make gopress

# Start the server. First run opens the web installer.
./build/gopress serve
```

If you plan to use GoPress regularly, install the CLI onto your `$PATH` so you can drop the `./build/` prefix:

```bash
make install            # installs gopress to $GOBIN (or $GOPATH/bin)
gopress serve           # works from any directory after install
```

`make install` prints the `export PATH=...` line to add if `$GOBIN` is not on `$PATH` yet.

Open `http://localhost:8080/install` on first run. The installer verifies the PostgreSQL connection, writes the site configuration, initializes tables, creates the administrator account, and switches the current process to the live site after setup.

## Build a Production Binary

```bash
./build/gopress build                  # -> build/gopress-server
./build/gopress build -o ./myserver    # custom output path
./build/gopress-server
```

(Drop the `./build/` prefix if you ran `make install`.)

`gopress build` regenerates `internal/autoload/autoload_gen.go` first, then runs `go build ./cmd/server`. The resulting binary has the currently-present themes and plugins baked in at compile time — production deployments do not need the Go toolchain to "discover" anything at runtime.

The service discovers site configuration from `sites/{host}/config.toml`. For local development the default host is usually `localhost`.

### Building on Low-Memory Machines

`go build` compiles packages in parallel across `GOMAXPROCS` cores, and each compile worker holds a sizeable working set in memory. On a 1 CPU / 1 GB VM (typical small VPS), parallel compilation can be killed by the OOM killer or fail with `signal: killed`.

If you hit this, force `go build` to compile one package at a time using `GOFLAGS`. The Go toolchain reads this environment variable transparently, so it applies to every step in the chain — `make gopress`, `gopress serve`, `gopress build`, and direct `go build` invocations:

```bash
GOFLAGS="-p=1"    make gopress              # serial compile of the CLI itself
GOFLAGS="-p=1"    ./build/gopress build     # serial compile of the server
GOFLAGS="-p=1"    ./build/gopress serve     # serial compile, then run
GOFLAGS="-p=1 -v" ./build/gopress build     # add -v to print package names as progress
```

Equivalent without `gopress`, after running `./build/gopress gen` to refresh autoload:

```bash
go build -p 1 -v -o build/gopress-server ./cmd/server
```

`-p 1` caps parallelism at one package; `-v` prints each package as it is compiled so the build does not look frozen. Expect a longer build time in exchange for a much lower memory peak.

## First-run Installer

The web installer has three stages:

1. Database connection and table prefix.
2. Site name, default theme, admin account, and interface language.
3. Configuration write, migrations, seed data, and live-site switch.

The generated `sites/<host>/config.toml` is written with `0600` permissions and includes a unique, randomly generated `jwt_secret`; you never set it by hand.

If the target database does not exist, GoPress attempts to connect to `postgres` or `template1` with the same account and create it automatically.

## Make Targets

| Target | Purpose |
|---|---|
| `make help` | List all available targets (also runs when you type `make` with no args, or with an unknown target). |
| `make gopress` | Build the gopress CLI to `build/gopress`. |
| `make server` | Build the server binary via `gopress build`. |
| `make gen` | Regenerate `internal/autoload` only. |
| `make install` | `go install ./cmd/gopress` (puts `gopress` into `$GOBIN`). |
| `make uninstall` | Remove the installed `gopress` binary. |
| `make clean` | Remove the `build/` directory. |

## Common Development Commands

Examples use `./build/gopress` (local build). After `make install` you can drop the `./build/` prefix.

```bash
# Run the server (with autoload regenerated)
./build/gopress serve

# Forward flags to cmd/server
./build/gopress serve -config sites/localhost/config.toml
./build/gopress serve -seed

# Regenerate autoload only (does not start anything)
./build/gopress gen

# Generate Swagger docs
go run ./cmd/gendoc

# Run tests
go test ./...
```

## Adding a New Theme or Plugin

1. Drop the folder into `themes/` or `plugins/`.
2. Make sure it contains a `theme.toml` (theme) or `plugin.toml` (plugin) and at least one non-test `.go` file at its root.
3. Re-run `./build/gopress serve`. The autoload file is regenerated and the new module is imported on startup.

No edit to `cmd/server/main.go` is required.

## After Installation

- Visit `/admin` to manage content, themes, plugins, media, users, and settings.
- Use **System Settings** to set site name, site description, site language, admin language, favicon, and Powered by GoPress display.
- Use **Themes** to activate a theme and import demo data.
- Use **Plugins** to enable multilingual support, SEO extras, code snippets, or custom plugins.

