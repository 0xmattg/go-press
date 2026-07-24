package hook

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestActionPriorityOrder(t *testing.T) {
	b := New()
	var order []int
	b.AddAction("x", func(ctx context.Context, args ...interface{}) { order = append(order, 2) }, 20)
	b.AddAction("x", func(ctx context.Context, args ...interface{}) { order = append(order, 1) }, 10)
	b.AddAction("x", func(ctx context.Context, args ...interface{}) { order = append(order, 3) }, 30)
	b.DoAction(context.Background(), "x")
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("callbacks ran out of priority order: %v", order)
	}
}

func TestRemoveActionUnregisters(t *testing.T) {
	b := New()
	var count int
	h := b.AddAction("x", func(ctx context.Context, args ...interface{}) { count++ }, 10)
	b.DoAction(context.Background(), "x")
	b.RemoveAction(h)
	b.DoAction(context.Background(), "x")
	if count != 1 {
		t.Fatalf("count = %d, want 1 (callback should fire once, then be removed)", count)
	}
}

func TestFilterChaining(t *testing.T) {
	b := New()
	b.AddFilter("f", func(v interface{}, args ...interface{}) interface{} { return v.(int) + 1 }, 10)
	b.AddFilter("f", func(v interface{}, args ...interface{}) interface{} { return v.(int) * 2 }, 20)
	got := b.ApplyFilter("f", 3)
	if got.(int) != 8 { // (3+1)*2
		t.Fatalf("ApplyFilter = %v, want 8", got)
	}
}

// TestConcurrentFireAndMutate must be run with -race. It reproduces the plugin
// toggle vs request-fires-hook scenario: DoAction/ApplyFilter iterating while
// Add/Remove churn the callback slices. Copy-on-write registration must keep the
// snapshots the readers hold immutable.
func TestConcurrentFireAndMutate(t *testing.T) {
	b := New()
	var fired int64
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Readers: continuously fire the hook.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					b.DoAction(context.Background(), "evt")
					b.ApplyFilter("flt", 0)
				}
			}
		}()
	}

	// Writers: continuously register and unregister callbacks.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					ha := b.AddAction("evt", func(ctx context.Context, args ...interface{}) {
						atomic.AddInt64(&fired, 1)
					}, i)
					hf := b.AddFilter("flt", func(v interface{}, args ...interface{}) interface{} {
						return v
					}, i)
					b.RemoveAction(ha)
					b.RemoveFilter(hf)
				}
			}
		}()
	}

	// Let the goroutines race for a while, then stop.
	for i := 0; i < 5000; i++ {
		b.DoAction(context.Background(), "evt")
	}
	close(stop)
	wg.Wait()
}
