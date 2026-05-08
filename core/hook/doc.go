// Package hook provides GoPress's action and filter bus.
//
// Actions are fire-and-forget notifications. Filters receive a value, return a
// possibly modified value, and run in priority order. The model is deliberately
// small and WordPress-inspired, but handles are explicit so plugins can
// unregister callbacks during runtime deactivation.
//
// Public hook names that form part of the framework contract belong in
// constants.go. Ad hoc strings should stay inside the package that emits them
// unless themes, plugins, or other core packages are expected to consume them.
package hook
