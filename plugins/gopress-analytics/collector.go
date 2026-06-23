package gopressanalytics

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go-press/pkg/logger"
)

const collectorFlushInterval = 5 * time.Second

type batchWriter interface {
	RecordBatch(ctx context.Context, events []Event) error
}

type collector struct {
	writer        batchWriter
	queue         chan Event
	maxBatchSize  int
	flushInterval time.Duration
	stop          chan struct{}
	done          chan struct{}
	once          sync.Once
	stopped       atomic.Bool
	dropped       atomic.Uint64
}

func newCollector(writer batchWriter, queueSize int) *collector {
	return newCollectorWithInterval(writer, queueSize, collectorFlushInterval)
}

func newCollectorWithInterval(writer batchWriter, queueSize int, flushInterval time.Duration) *collector {
	if queueSize < 100 {
		queueSize = 100
	}
	if flushInterval <= 0 {
		flushInterval = collectorFlushInterval
	}
	c := &collector{
		writer:        writer,
		queue:         make(chan Event, queueSize),
		maxBatchSize:  queueSize,
		flushInterval: flushInterval,
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
	go c.run()
	return c
}

func (c *collector) publish(event Event) bool {
	if c.stopped.Load() {
		return false
	}
	select {
	case <-c.stop:
		return false
	case c.queue <- event:
		return true
	default:
		c.dropped.Add(1)
		return false
	}
}

func (c *collector) stopAndFlush() {
	c.once.Do(func() {
		c.stopped.Store(true)
		close(c.stop)
	})
	<-c.done
}

func (c *collector) run() {
	defer close(c.done)
	ticker := time.NewTicker(c.flushInterval)
	defer ticker.Stop()

	batch := make([]Event, 0, c.maxBatchSize)
	appendEvent := func(event Event) {
		if len(batch) >= c.maxBatchSize {
			c.dropped.Add(1)
			return
		}
		batch = append(batch, event)
	}
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := c.writer.RecordBatch(ctx, batch)
		cancel()
		if err != nil {
			logger.Error("gopress-analytics: batch write failed", "events", len(batch), "error", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case event := <-c.queue:
			appendEvent(event)
		case <-ticker.C:
			flush()
		case <-c.stop:
			for {
				select {
				case event := <-c.queue:
					appendEvent(event)
				default:
					flush()
					return
				}
			}
		}
	}
}
