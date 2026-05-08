// Package content defines GoPress's unified content model.
//
// All first-party and theme-declared content types share the same Content table
// plus key-value ContentMeta rows. A ContentTypeDef describes how a type should
// appear in admin screens, REST endpoints, archive pages, rewrite rules, and
// taxonomy relationships. Registry is the in-memory source of truth for those
// definitions at runtime.
//
// Plugins that need request-specific filtering should use ContentScope instead
// of forking repository methods. The multilang plugin uses this mechanism to
// make identical slugs resolve to different rows depending on the active
// request language.
package content
