// Package core wires the GoPress runtime together.
//
// The Engine type is the application container. It owns long-lived framework
// subsystems such as the content registry, option store, menu store, hook bus,
// cache manager, rewrite and SEO builders, repositories, worker scheduler,
// theme runtime, plugin manager, admin handler, and Gin router.
//
// Core intentionally acts as an integration layer rather than a business
// feature package. Content modeling lives in core/content, URL and SEO rules in
// core/rewrite, rendering in core/theme, and extension contracts in core/hook
// and core/plugin. Keeping those packages separate makes themes and plugins
// depend on stable interfaces instead of private Engine internals.
package core
