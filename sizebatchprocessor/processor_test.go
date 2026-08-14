package sizebatchprocessor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processortest"
)

// makeTraces builds a ptrace.Traces with n spans to control approximate size.
func makeTraces(n int) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	ss := rs.ScopeSpans().AppendEmpty()
	for i := 0; i < n; i++ {
		sp := ss.Spans().AppendEmpty()
		sp.SetName("span")
		sp.Attributes().PutStr("key", "value-that-adds-a-bit-of-size-to-each-span")
	}
	return td
}

func makeMetrics(n int) pmetric.Metrics {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()
	for i := 0; i < n; i++ {
		m := sm.Metrics().AppendEmpty()
		m.SetName("metric")
		m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(1.0)
	}
	return md
}

func makeLogs(n int) plog.Logs {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	sl := rl.ScopeLogs().AppendEmpty()
	for i := 0; i < n; i++ {
		lr := sl.LogRecords().AppendEmpty()
		lr.Body().SetStr("log message with some content")
	}
	return ld
}

func processorSettings() processor.Settings {
	return processortest.NewNopSettings(processorType)
}

// measureTraces returns the JSON size of td, matching the sizeFn used by the processor.
func measureTraces(td ptrace.Traces) int {
	m := &ptrace.JSONMarshaler{}
	b, _ := m.MarshalTraces(td)
	return len(b)
}

// ------- Traces -------

func TestTracesFlushOnSize(t *testing.T) {
	sink := &consumertest.TracesSink{}

	// Build two batches, each large enough that together they exceed the limit.
	batch1 := makeTraces(50)
	batch2 := makeTraces(50)
	size1 := measureTraces(batch1)
	size2 := measureTraces(batch2)

	cfg := &Config{
		MaxSizeBytes: size1 + size2 - 1, // limit is between the two sizes summed
		Timeout:      10 * time.Second,
	}

	p, err := NewFactory().CreateTraces(
		context.Background(),
		processorSettings(),
		cfg,
		sink,
	)
	require.NoError(t, err)
	require.NoError(t, p.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	require.NoError(t, p.ConsumeTraces(context.Background(), batch1))
	// batch1 is buffered; no flush yet.
	assert.Empty(t, sink.AllTraces())

	require.NoError(t, p.ConsumeTraces(context.Background(), batch2))
	// Adding batch2 would exceed the limit → batch1 is flushed, batch2 is buffered.
	require.Len(t, sink.AllTraces(), 1)
	assert.Equal(t, 50, sink.AllTraces()[0].SpanCount())

	// Drain the remaining buffer via shutdown.
	require.NoError(t, p.Shutdown(context.Background()))
	require.Len(t, sink.AllTraces(), 2)
	assert.Equal(t, 50, sink.AllTraces()[1].SpanCount())
}

func TestTracesFlushOnTimeout(t *testing.T) {
	sink := &consumertest.TracesSink{}
	cfg := &Config{
		MaxSizeBytes: 10 << 20, // 10 MiB — won't be reached
		Timeout:      50 * time.Millisecond,
	}

	p, err := NewFactory().CreateTraces(context.Background(), processorSettings(), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, p.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	require.NoError(t, p.ConsumeTraces(context.Background(), makeTraces(5)))
	assert.Empty(t, sink.AllTraces())

	// Wait for the timeout flush.
	assert.Eventually(t, func() bool {
		return len(sink.AllTraces()) == 1
	}, 500*time.Millisecond, 10*time.Millisecond)

	assert.Equal(t, 5, sink.AllTraces()[0].SpanCount())
}

func TestTracesOversizedBatchPassthrough(t *testing.T) {
	sink := &consumertest.TracesSink{}

	big := makeTraces(200)
	bigSize := measureTraces(big)

	cfg := &Config{
		MaxSizeBytes: bigSize / 2, // limit is smaller than one batch
		Timeout:      10 * time.Second,
	}

	p, err := NewFactory().CreateTraces(context.Background(), processorSettings(), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, p.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	require.NoError(t, p.ConsumeTraces(context.Background(), big))
	// Single batch exceeds limit → flushed immediately.
	require.Len(t, sink.AllTraces(), 1)
	assert.Equal(t, 200, sink.AllTraces()[0].SpanCount())
}

func TestTracesShutdownFlush(t *testing.T) {
	sink := &consumertest.TracesSink{}
	cfg := &Config{
		MaxSizeBytes: 10 << 20,
		Timeout:      10 * time.Second,
	}

	p, err := NewFactory().CreateTraces(context.Background(), processorSettings(), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, p.Start(context.Background(), componenttest.NewNopHost()))

	require.NoError(t, p.ConsumeTraces(context.Background(), makeTraces(10)))
	assert.Empty(t, sink.AllTraces()) // still buffered

	require.NoError(t, p.Shutdown(context.Background()))
	require.Len(t, sink.AllTraces(), 1)
	assert.Equal(t, 10, sink.AllTraces()[0].SpanCount())
}

// ------- Metrics -------

func TestMetricsFlushOnSize(t *testing.T) {
	sink := &consumertest.MetricsSink{}

	m := &pmetric.JSONMarshaler{}
	b1 := makeMetrics(30)
	s1, _ := m.MarshalMetrics(b1)
	b2 := makeMetrics(30)

	cfg := &Config{
		MaxSizeBytes: len(s1)*2 - 1,
		Timeout:      10 * time.Second,
	}

	p, err := NewFactory().CreateMetrics(context.Background(), processorSettings(), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, p.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	require.NoError(t, p.ConsumeMetrics(context.Background(), b1))
	assert.Empty(t, sink.AllMetrics())

	require.NoError(t, p.ConsumeMetrics(context.Background(), b2))
	require.Len(t, sink.AllMetrics(), 1)
	assert.Equal(t, 30, sink.AllMetrics()[0].DataPointCount())
}

// ------- Logs -------

func TestLogsFlushOnTimeout(t *testing.T) {
	sink := &consumertest.LogsSink{}
	cfg := &Config{
		MaxSizeBytes: 10 << 20,
		Timeout:      50 * time.Millisecond,
	}

	p, err := NewFactory().CreateLogs(context.Background(), processorSettings(), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, p.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	require.NoError(t, p.ConsumeLogs(context.Background(), makeLogs(7)))
	assert.Empty(t, sink.AllLogs())

	assert.Eventually(t, func() bool {
		return len(sink.AllLogs()) == 1
	}, 500*time.Millisecond, 10*time.Millisecond)
	assert.Equal(t, 7, sink.AllLogs()[0].LogRecordCount())
}

// ------- Factory -------

func TestNewFactory(t *testing.T) {
	f := NewFactory()
	assert.Equal(t, processorType, f.Type())
}
