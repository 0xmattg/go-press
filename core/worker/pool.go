package worker

import (
	"context"
	"sync"

	"go-press/pkg/logger"
)

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
// critical tasks.
type Pool struct {
	tasks   chan Task
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	workers int
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

func (p *Pool) worker(id int) {
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case task, ok := <-p.tasks:
			if !ok {
				return
			}
			if err := task.Fn(p.ctx); err != nil {
				logger.Error("Worker task failed", "worker", id, "task", task.Name, "error", err)
			}
		}
	}
}

// Submit adds a task to the pool.
//
// The call is non-blocking while the queue has capacity. If the queue is full,
// the task runs inline on the caller goroutine and failures are logged.
func (p *Pool) Submit(task Task) {
	select {
	case p.tasks <- task:
	default:
		// Queue full — run inline as fallback
		logger.Info("Worker pool queue full, running inline", "task", task.Name)
		if err := task.Fn(p.ctx); err != nil {
			logger.Error("Inline task failed", "task", task.Name, "error", err)
		}
	}
}

// SubmitFunc is a convenience wrapper that creates a Task and submits it.
func (p *Pool) SubmitFunc(name string, fn func(ctx context.Context) error) {
	p.Submit(Task{Name: name, Fn: fn})
}

// Shutdown gracefully stops the pool and waits for all workers to finish.
func (p *Pool) Shutdown() {
	p.cancel()
	close(p.tasks)
	p.wg.Wait()
	logger.Info("Worker pool stopped")
}
