package sizebatchprocessor

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

// batcher[T] buffers items of type T (ptrace.Traces, pmetric.Metrics, plog.Logs)
// and flushes them as a single merged batch when the accumulated proto-serialized
// size exceeds maxSizeBytes or when the timeout fires.
//
// Timer correctness: each logical flush window is identified by a monotonically
// increasing timerGen. The AfterFunc closure captures the gen value at the time
// the timer was armed. timedFlush checks that the batcher's current timerGen
// still matches before acting, ensuring stale timer goroutines (that fired just
// as consume performed an explicit overflow flush and re-armed the timer) never
// touch the new buffer.
type batcher[T any] struct {
	maxSizeBytes int
	timeout      time.Duration

	mu       sync.Mutex
	buf      []T
	curSize  int
	timer    *time.Timer // guarded by mu; nil when no timer is armed
	timerGen uint64      // incremented each time the timer is (re-)armed

	flushFn func(context.Context, []T) error
	sizeFn  func(T) int
	logger  *zap.Logger
}

func newBatcher[T any](
	cfg *Config,
	flushFn func(context.Context, []T) error,
	sizeFn func(T) int,
	logger *zap.Logger,
) *batcher[T] {
	return &batcher[T]{
		maxSizeBytes: cfg.MaxSizeBytes,
		timeout:      cfg.Timeout,
		flushFn:      flushFn,
		sizeFn:       sizeFn,
		logger:       logger,
	}
}

// armTimer arms a new AfterFunc timer. Must be called with mu held.
func (b *batcher[T]) armTimer() {
	b.timerGen++
	gen := b.timerGen
	b.timer = time.AfterFunc(b.timeout, func() { b.timedFlush(gen) })
}

// disarmTimer stops the current timer. Must be called with mu held.
func (b *batcher[T]) disarmTimer() {
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
}

func (b *batcher[T]) start(_ context.Context, _ component.Host) error {
	return nil // timer is armed lazily on first item
}

func (b *batcher[T]) shutdown(ctx context.Context) error {
	b.mu.Lock()
	b.disarmTimer()
	toFlush := b.buf
	b.buf = nil
	b.curSize = 0
	b.mu.Unlock()

	if len(toFlush) == 0 {
		return nil
	}
	return b.flushFn(ctx, toFlush)
}

// consume is the processorhelper ConsumeFunc. It is safe for concurrent use.
func (b *batcher[T]) consume(ctx context.Context, item T) error {
	incoming := b.sizeFn(item)

	b.mu.Lock()

	// If the buffer is non-empty and adding this item would exceed the limit,
	// flush the current buffer first, then start a fresh one.
	if len(b.buf) > 0 && b.curSize+incoming > b.maxSizeBytes {
		toFlush := b.buf
		b.buf = nil
		b.curSize = 0
		b.disarmTimer()
		b.mu.Unlock()

		if err := b.flushFn(ctx, toFlush); err != nil {
			return err
		}

		b.mu.Lock()
	}

	wasEmpty := len(b.buf) == 0
	b.buf = append(b.buf, item)
	b.curSize += incoming

	// A single oversized item bypasses buffering entirely.
	if incoming >= b.maxSizeBytes {
		toFlush := b.buf
		b.buf = nil
		b.curSize = 0
		b.disarmTimer()
		b.mu.Unlock()
		return b.flushFn(ctx, toFlush)
	}

	if wasEmpty {
		b.armTimer()
	}
	b.mu.Unlock()

	return nil
}

// timedFlush is called by AfterFunc. It only acts if myGen matches the current
// timerGen, preventing stale goroutines from flushing a newly-started buffer.
func (b *batcher[T]) timedFlush(myGen uint64) {
	b.mu.Lock()
	if b.timerGen != myGen || len(b.buf) == 0 {
		b.mu.Unlock()
		return
	}
	toFlush := b.buf
	b.buf = nil
	b.curSize = 0
	b.timer = nil
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := b.flushFn(ctx, toFlush); err != nil {
		b.logger.Error("size-batch timed flush failed", zap.Error(err))
	}
}

// ---------------------------------------------------------------------------
// Per-signal constructors
// ---------------------------------------------------------------------------

func newTracesBatcher(cfg *Config, next consumer.Traces, logger *zap.Logger) *batcher[ptrace.Traces] {
	m := &ptrace.JSONMarshaler{}
	return newBatcher(
		cfg,
		func(ctx context.Context, batches []ptrace.Traces) error {
			merged := ptrace.NewTraces()
			for i := range batches {
				batches[i].ResourceSpans().MoveAndAppendTo(merged.ResourceSpans())
			}
			return next.ConsumeTraces(ctx, merged)
		},
		func(td ptrace.Traces) int {
			b, err := m.MarshalTraces(td)
			if err != nil {
				return 0
			}
			return len(b)
		},
		logger,
	)
}

func newMetricsBatcher(cfg *Config, next consumer.Metrics, logger *zap.Logger) *batcher[pmetric.Metrics] {
	m := &pmetric.JSONMarshaler{}
	return newBatcher(
		cfg,
		func(ctx context.Context, batches []pmetric.Metrics) error {
			merged := pmetric.NewMetrics()
			for i := range batches {
				batches[i].ResourceMetrics().MoveAndAppendTo(merged.ResourceMetrics())
			}
			return next.ConsumeMetrics(ctx, merged)
		},
		func(md pmetric.Metrics) int {
			b, err := m.MarshalMetrics(md)
			if err != nil {
				return 0
			}
			return len(b)
		},
		logger,
	)
}

func newLogsBatcher(cfg *Config, next consumer.Logs, logger *zap.Logger) *batcher[plog.Logs] {
	m := &plog.JSONMarshaler{}
	return newBatcher(
		cfg,
		func(ctx context.Context, batches []plog.Logs) error {
			merged := plog.NewLogs()
			for i := range batches {
				batches[i].ResourceLogs().MoveAndAppendTo(merged.ResourceLogs())
			}
			return next.ConsumeLogs(ctx, merged)
		},
		func(ld plog.Logs) int {
			b, err := m.MarshalLogs(ld)
			if err != nil {
				return 0
			}
			return len(b)
		},
		logger,
	)
}
