// Package commerce defines the generic e-commerce extension contracts that the
// GoPress core exposes to a commerce engine plugin and its satellite plugins
// (payment gateways, shipping carriers, tax providers).
//
// It contains ONLY interfaces and value types — no business logic, no storage,
// no hardcoded gateway/theme/currency. It is the commerce analogue of
// core/plugin.Plugin or core/theme.Theme: a thin contract package that lives in
// core so that both the commerce engine and independent satellite plugins can
// depend on the same types without depending on each other.
//
// Dependency rule (see AGENTS.md): the commerce engine plugin and every
// satellite plugin depend on THIS package (via core) only. Satellites never
// import the commerce engine, and register themselves through the core hook bus
// (see registry.go). This keeps the dependency graph plugin→core with no
// plugin→plugin edge and no import cycle.
package commerce
