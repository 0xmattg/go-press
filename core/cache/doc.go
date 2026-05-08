// Package cache contains GoPress's layered cache primitives.
//
// The cache Manager reads through an in-process L1 cache first and an optional
// distributed L2 cache second. Missing backends are replaced with no-op caches,
// which lets the engine keep a single code path whether Redis is available or
// not. Page and fragment helpers build on the same Manager abstraction.
package cache
