// Package worker provides lightweight background execution for GoPress.
//
// Pool runs asynchronous tasks on a bounded goroutine set. Scheduler dispatches
// fixed-interval jobs into the pool. The package is intentionally small and
// process-local; deployments that require durable queues can replace higher
// level call sites with a persistent job system later.
package worker
