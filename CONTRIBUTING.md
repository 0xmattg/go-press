# Contributing Guide

Thank you for considering a contribution to GoPress, whether through code, documentation, themes, or plugins. GoPress is still in beta, so feedback from real use cases, reproducible bug reports, documentation improvements, and focused incremental changes are especially valuable.

## Opening Issues

Before opening an issue, please search existing issues when possible to avoid duplicate reports. A high-quality issue usually includes:

- The GoPress version or commit hash
- The Go version, database version, and whether Redis is enabled
- Steps to reproduce, expected behavior, and actual behavior
- Relevant logs, configuration snippets, or screenshots
- For performance issues, the data volume, load-testing tool, and test environment

## Opening Pull Requests

Keep changes small and clear where possible. A pull request should ideally solve one problem at a time; avoid mixing features, refactors, formatting changes, and documentation updates in the same PR.

Before submitting, please check that:

- The code style follows the existing directory structure and naming conventions
- New behavior includes appropriate tests or manual verification notes
- User-visible behavior changes are reflected in the documentation
- The PR does not include local configuration, secrets, build artifacts, or unrelated formatting

Common verification commands:

```bash
go test ./...
gopress serve              # or `./build/gopress serve` if not installed via `make install`
```

If some checks depend on PostgreSQL, Redis, or local site configuration, please describe in the PR what you actually ran and which checks were not run.

## Licensing and Sign-Off

GoPress is released under the [MIT License](LICENSE). By contributing, you agree that your contribution is licensed under the same terms.

Every commit in a pull request must be signed off to certify the [Developer Certificate of Origin](https://developercertificate.org/), confirming that you wrote the change or otherwise have the right to submit it under the project's license. `git commit -s` adds the sign-off line:

```text
Signed-off-by: Your Name <your.email@example.com>
```

Use your real name or a consistent public identity, and an email address you control.

Go files under `core/` must start with the project license header. `make license` adds it to new files and `make license-check` verifies it; CI runs the check.

## Documentation, Themes, and Plugins

Documentation changes should stay neutral, verifiable, and user-focused. For statements about performance, compatibility, or security, provide reproduction steps, constraints, or clearly label them as target behavior when applicable.

Theme and plugin contributions should avoid direct dependencies on other themes or plugins. Capabilities that can be expressed through core interfaces, hooks, filters, or configuration should not be implemented through implicit cross-package calls.

Code marked with a `LEGACY(<id>):` comment keeps already-deployed sites working (old data, configuration, or URLs). Please do not remove or simplify it as dead code unless the removal condition written in that comment is met.

## Extension Identity

A theme or plugin is identified by its **slug**, and the same slug is used everywhere:

- The directory is `themes/<slug>/` or `plugins/<slug>/`.
- A plugin's `plugin.toml` declares `[plugin].slug = "<slug>"`, and its `Name()` returns the same value. A theme registers itself with `core.RegisterTheme("<slug>", ...)`.
- A slug is 2–64 characters of lowercase letters and digits, in words joined by single hyphens (`seo-extras`, `commerce-usdt`). Underscores are not allowed, and a theme and a plugin cannot share a slug.
- Table names are derived from the slug by replacing hyphens with underscores: use `pluginMeta.Table("events")` (from `plugin.ParseMetaString`) to get `<prefix>plgn_<storage_slug>_events`. There is no setting to override the derived name.

`internal/contracts` tests these rules for every module it finds, and `gopress gen` prints a warning when a module breaks them.

## How Pull Requests Land

Maintainers carry merged pull requests into the main development branch with `git cherry-pick -x`, which keeps your authorship and `Signed-off-by` line and records the original commit.
