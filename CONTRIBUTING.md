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
go run ./cmd/server/
```

If some checks depend on PostgreSQL, Redis, or local site configuration, please describe in the PR what you actually ran and which checks were not run.

## Documentation, Themes, and Plugins

Documentation changes should stay neutral, verifiable, and user-focused. For statements about performance, compatibility, or security, provide reproduction steps, constraints, or clearly label them as target behavior when applicable.

Theme and plugin contributions should avoid direct dependencies on other themes or plugins. Capabilities that can be expressed through core interfaces, hooks, filters, or configuration should not be implemented through implicit cross-package calls.
