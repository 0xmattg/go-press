// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package worker

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/0xmattg/go-press/pkg/logger"
)

// ErrPoolClosed is returned by Submit after Shutdown has started.
var ErrPoolClosed = errors.New("worker pool is closed")

// Task represents a unit of background work executed by the pool.
//
// Fn receives the pool context so tasks can stop promptly during shutdown.
// Implementations should be idempotent when possible because callers may retry
// failed tasks at a higher layer in the future.
type Task struct {
	Name string
	Fn   func(ctx context.Context) error
}

// Pool is a bounded goroutine worker pool for asynchronous tasks.
//
// It is intentionally best-effort: when the queue is full, Submit runs the task
// inline as a backpressure fallback instead of dropping work. Use it for cache
// maintenance, sitemap generation, media processing, and similar non-request
// critical tasks. It is not a durable queue: tasks still queued when a
// shutdown deadline expires are dropped.
//
// A panicking task is recovered and logged so one faulty job cannot terminate
// the process.
type Pool struct {
	tasks   chan Task
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	workers int

	mu        sync.RWMutex // guards closed and the send/close race on tasks
	closed    bool
	closeOnce sync.Once
}

// NewPool creates a worker pool with the given number of workers.
func NewPool(workers int) *Pool {
	if workers <= 0 {
		workers = 4
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		tasks:   make(chan Task, workers*10),
		ctx:     ctx,
		cancel:  cancel,
		workers: workers,
	}
	p.start()
	return p
}

func (p *Pool) start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	logger.Info("Worker pool started", "workers", p.workers)
}

// worker drains the queue until Shutdown closes it. Once the shutdown deadline
// cancels the pool context, remaining queued tasks are skipped.
func (p *Pool) worker(id int) {
	defer p.wg.Done()
	for task := range p.tasks {
		if p.ctx.Err() != nil {
			logger.Warn("Worker task dropped during shutdown", "worker", id, "task", task.Name)
			continue
		}
		if err := RunTask(p.ctx, task); err != nil {
			logger.Error("Worker task failed", "worker", id, "task", task.Name, "error", err)
		}
	}
}

// RunTask executes task.Fn, converting a panic into an error so background
// goroutines never crash the process.
func RunTask(ctx context.Context, task Task) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Background task panicked", "task", task.Name, "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("task %q panicked: %v", task.Name, r)
		}
	}()
	if task.Fn == nil {
		return nil
	}
	return task.Fn(ctx)
}

// Submit adds a task to the pool.
//
// The call is non-blocking while the queue has capacity. If the queue is full,
// the task runs inline on the caller goroutine and failures are logged. After
// Shutdown has started, Submit returns ErrPoolClosed without running the task.
func (p *Pool) Submit(task Task) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		logger.Warn("Worker pool closed, task rejected", "task", task.Name)
		return ErrPoolClosed
	}
	select {
	case p.tasks <- task:
		p.mu.RUnlock()
		return nil
	default:
	}
	p.mu.RUnlock()

	// Queue full — run inline as fallback, outside the lock so a slow task
	// cannot delay Shutdown.
	logger.Info("Worker pool queue full, running inline", "task", task.Name)
	if err := RunTask(p.ctx, task); err != nil {
		logger.Error("Inline task failed", "task", task.Name, "error", err)
	}
	return nil
}

// SubmitFunc is a convenience wrapper that creates a Task and submits it.
func (p *Pool) SubmitFunc(name string, fn func(ctx context.Context) error) error {
	return p.Submit(Task{Name: name, Fn: fn})
}

// Shutdown stops accepting tasks and waits for queued work to finish. It is
// equivalent to ShutdownContext without a deadline.
func (p *Pool) Shutdown() {
	_ = p.ShutdownContext(context.Background())
}

// ShutdownContext stops accepting new tasks, lets workers drain the queue, and
// waits until they finish or ctx expires. On expiry the pool context is
// cancelled so running tasks can abort and queued tasks are dropped; the
// method then returns ctx.Err() without waiting further. It is safe to call
// more than once.
func (p *Pool) ShutdownContext(ctx context.Context) error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		close(p.tasks)
		p.mu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		p.cancel()
		logger.Info("Worker pool stopped")
		return nil
	case <-ctx.Done():
		p.cancel()
		logger.Warn("Worker pool shutdown deadline exceeded; remaining tasks cancelled")
		return ctx.Err()
	}
}
