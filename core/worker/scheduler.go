package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/0xmattg/go-press/pkg/logger"
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

// JobHandle identifies a registered job so its owner can remove it, for
// example when a plugin is deactivated.
type JobHandle struct {
	id uint64
}

// Scheduler runs jobs at fixed intervals using simple time.Ticker.
// For production cron-expression support, swap internals with robfig/cron/v3.
//
// Jobs may be added before or after Start: a job added to a running scheduler
// starts ticking immediately. Stop is idempotent and waits for every ticker
// goroutine to exit; jobs already handed to the pool are drained by the pool.
type Scheduler struct {
	mu      sync.Mutex
	jobs    map[uint64]*scheduledEntry
	order   []uint64
	nextID  uint64
	pool    *Pool
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
	stopped bool
	wg      sync.WaitGroup
}

type scheduledEntry struct {
	job  ScheduledJob
	stop chan struct{}
}

// NewScheduler creates a scheduler that dispatches jobs to the given pool.
func NewScheduler(pool *Pool) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		jobs:   make(map[uint64]*scheduledEntry),
		pool:   pool,
		ctx:    ctx,
		cancel: cancel,
	}
}

// AddJob registers a recurring job and returns a handle for RemoveJob.
//
// Jobs added after Start begin running right away. Adding to a stopped
// scheduler records nothing and returns a zero handle.
func (s *Scheduler) AddJob(name string, interval time.Duration, fn func(ctx context.Context) error) JobHandle {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		logger.Warn("Scheduler stopped, job not added", "job", name)
		return JobHandle{}
	}
	if interval <= 0 {
		logger.Error("Scheduled job interval must be positive", "job", name, "interval", interval)
		return JobHandle{}
	}
	s.nextID++
	id := s.nextID
	entry := &scheduledEntry{
		job:  ScheduledJob{Name: name, Interval: interval, Fn: fn},
		stop: make(chan struct{}),
	}
	s.jobs[id] = entry
	s.order = append(s.order, id)
	if s.started {
		s.launchLocked(entry)
	}
	return JobHandle{id: id}
}

// RemoveJob stops and forgets a job. Unknown or zero handles are ignored.
func (s *Scheduler) RemoveJob(handle JobHandle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.jobs[handle.id]
	if !ok {
		return
	}
	delete(s.jobs, handle.id)
	for i, id := range s.order {
		if id == handle.id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	close(entry.stop)
}

// Start begins running all registered jobs on their intervals.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped {
		return
	}
	s.started = true

	for _, id := range s.order {
		s.launchLocked(s.jobs[id])
	}
	logger.Info("Scheduler started", "jobs", len(s.order))
}

func (s *Scheduler) launchLocked(entry *scheduledEntry) {
	s.wg.Add(1)
	go s.runJob(entry.job, entry.stop)
}

func (s *Scheduler) runJob(job ScheduledJob, stop chan struct{}) {
	defer s.wg.Done()
	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			task := Task{Name: "scheduled:" + job.Name, Fn: job.Fn}
			if s.pool == nil {
				if err := RunTask(s.ctx, task); err != nil {
					logger.Error("Scheduled job failed", "job", job.Name, "error", err)
				}
				continue
			}
			if err := s.pool.Submit(task); errors.Is(err, ErrPoolClosed) {
				return
			}
		}
	}
}

// Stop terminates all scheduled jobs and waits for their ticker goroutines to
// exit. It is safe to call more than once.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.cancel()
	s.mu.Unlock()

	s.wg.Wait()
	logger.Info("Scheduler stopped")
}
