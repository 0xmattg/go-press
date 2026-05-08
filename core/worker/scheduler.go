package worker

import (
	"context"
	"sync"
	"time"

	"go-press/pkg/logger"
)

// ScheduledJob defines a recurring in-process job.
//
// Jobs are fixed-interval rather than cron-expression based. They are suitable
// for maintenance loops that can tolerate process restarts and do not require
// durable execution guarantees.
type ScheduledJob struct {
	Name     string
	Interval time.Duration
	Fn       func(ctx context.Context) error
}

// Scheduler runs jobs at fixed intervals using simple time.Ticker.
// For production cron-expression support, swap internals with robfig/cron/v3.
type Scheduler struct {
	mu      sync.Mutex
	jobs    []ScheduledJob
	stops   []chan struct{}
	pool    *Pool
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
}

// NewScheduler creates a scheduler that dispatches jobs to the given pool.
func NewScheduler(pool *Pool) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		pool:   pool,
		ctx:    ctx,
		cancel: cancel,
	}
}

// AddJob registers a recurring job.
//
// Jobs should be added before Start. Adding jobs after Start records them in
// memory but does not start a ticker for them until the scheduler is recreated.
func (s *Scheduler) AddJob(name string, interval time.Duration, fn func(ctx context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, ScheduledJob{
		Name:     name,
		Interval: interval,
		Fn:       fn,
	})
}

// Start begins running all registered jobs on their intervals.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	s.started = true

	for _, job := range s.jobs {
		stop := make(chan struct{})
		s.stops = append(s.stops, stop)
		go s.runJob(job, stop)
	}
	logger.Info("Scheduler started", "jobs", len(s.jobs))
}

func (s *Scheduler) runJob(job ScheduledJob, stop chan struct{}) {
	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			s.pool.Submit(Task{
				Name: "scheduled:" + job.Name,
				Fn:   job.Fn,
			})
		}
	}
}

// Stop terminates all scheduled jobs.
func (s *Scheduler) Stop() {
	s.cancel()
	s.mu.Lock()
	for _, stop := range s.stops {
		close(stop)
	}
	s.mu.Unlock()
	logger.Info("Scheduler stopped")
}
