// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolRecoversPanickingTask(t *testing.T) {
	p := NewPool(1)
	defer p.Shutdown()

	var ran atomic.Bool
	done := make(chan struct{})
	if err := p.SubmitFunc("boom", func(context.Context) error { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	if err := p.SubmitFunc("after", func(context.Context) error {
		ran.Store(true)
		close(done)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pool stopped processing after a panicking task")
	}
	if !ran.Load() {
		t.Fatal("follow-up task did not run")
	}
}

func TestRunTaskConvertsPanicToError(t *testing.T) {
	err := RunTask(context.Background(), Task{Name: "boom", Fn: func(context.Context) error { panic("bad") }})
	if err == nil {
		t.Fatal("RunTask() error = nil, want panic error")
	}
}

func TestPoolSubmitAfterShutdownReturnsError(t *testing.T) {
	p := NewPool(2)
	p.Shutdown()
	p.Shutdown() // idempotent

	if err := p.SubmitFunc("late", func(context.Context) error { return nil }); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("SubmitFunc() after shutdown = %v, want ErrPoolClosed", err)
	}
}

func TestPoolShutdownDrainsQueuedTasks(t *testing.T) {
	p := NewPool(1)
	var count atomic.Int32
	for i := 0; i < 5; i++ {
		_ = p.SubmitFunc("count", func(context.Context) error {
			count.Add(1)
			return nil
		})
	}
	if err := p.ShutdownContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := count.Load(); got != 5 {
		t.Fatalf("drained tasks = %d, want 5", got)
	}
}

func TestPoolShutdownDeadlineCancelsRunningTask(t *testing.T) {
	p := NewPool(1)
	started := make(chan struct{})
	_ = p.SubmitFunc("slow", func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := p.ShutdownContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ShutdownContext() = %v, want deadline exceeded", err)
	}
}

// TestPoolConcurrentSubmitAndShutdown guards against "send on closed channel"
// when producers race shutdown. Run with -race.
func TestPoolConcurrentSubmitAndShutdown(t *testing.T) {
	p := NewPool(2)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = p.SubmitFunc("noop", func(context.Context) error { return nil })
			}
		}()
	}
	time.Sleep(time.Millisecond)
	p.Shutdown()
	wg.Wait()
}

func TestSchedulerStopIsIdempotent(t *testing.T) {
	s := NewScheduler(NewPool(1))
	s.AddJob("noop", time.Hour, func(context.Context) error { return nil })
	s.Start()
	s.Stop()
	s.Stop()
	if h := s.AddJob("late", time.Hour, func(context.Context) error { return nil }); h != (JobHandle{}) {
		t.Fatal("AddJob() on a stopped scheduler returned a live handle")
	}
}

func TestSchedulerRunsJobAddedAfterStart(t *testing.T) {
	pool := NewPool(1)
	defer pool.Shutdown()
	s := NewScheduler(pool)
	s.Start()
	defer s.Stop()

	fired := make(chan struct{}, 1)
	s.AddJob("late", 5*time.Millisecond, func(context.Context) error {
		select {
		case fired <- struct{}{}:
		default:
		}
		return nil
	})
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("job added after Start never ran")
	}
}

func TestSchedulerRemoveJobStopsIt(t *testing.T) {
	pool := NewPool(1)
	defer pool.Shutdown()
	s := NewScheduler(pool)
	s.Start()
	defer s.Stop()

	var runs atomic.Int32
	handle := s.AddJob("removable", 5*time.Millisecond, func(context.Context) error {
		runs.Add(1)
		return nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	s.RemoveJob(handle)
	s.RemoveJob(handle) // unknown handle is ignored
	time.Sleep(20 * time.Millisecond)
	settled := runs.Load()
	time.Sleep(30 * time.Millisecond)
	if after := runs.Load(); after != settled {
		t.Fatalf("job kept running after RemoveJob: %d → %d", settled, after)
	}
}
