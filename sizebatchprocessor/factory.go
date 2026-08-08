package sizebatchprocessor

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
)

const (
	typeStr         = "sizebatch"
	stability       = component.StabilityLevelDevelopment
	defaultMaxBytes = 1 << 20        // 1 MiB
	defaultTimeout  = 5 * time.Second
)

var processorType = component.MustNewType(typeStr)

// NewFactory returns the factory for the sizebatch processor.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		processorType,
		createDefaultConfig,
		processor.WithTraces(createTracesProcessor, stability),
		processor.WithMetrics(createMetricsProcessor, stability),
		processor.WithLogs(createLogsProcessor, stability),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		MaxSizeBytes: defaultMaxBytes,
		Timeout:      defaultTimeout,
	}
}

func createTracesProcessor(ctx context.Context, set processor.Settings, cfg component.Config, next consumer.Traces) (processor.Traces, error) {
	b := newTracesBatcher(cfg.(*Config), next, set.Logger)
	// ProcessTracesFunc must return (ptrace.Traces, error). We buffer the incoming
	// batch and return ErrSkipProcessingData so the helper does not forward to next;
	// the batcher calls next itself when flushing.
	fn := func(fCtx context.Context, td ptrace.Traces) (ptrace.Traces, error) {
		if err := b.consume(fCtx, td); err != nil {
			return ptrace.Traces{}, err
		}
		return ptrace.Traces{}, processorhelper.ErrSkipProcessingData
	}
	return processorhelper.NewTraces(ctx, set, cfg, next, fn,
		processorhelper.WithStart(b.start),
		processorhelper.WithShutdown(b.shutdown),
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}),
	)
}

func createMetricsProcessor(ctx context.Context, set processor.Settings, cfg component.Config, next consumer.Metrics) (processor.Metrics, error) {
	b := newMetricsBatcher(cfg.(*Config), next, set.Logger)
	fn := func(fCtx context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
		if err := b.consume(fCtx, md); err != nil {
			return pmetric.Metrics{}, err
		}
		return pmetric.Metrics{}, processorhelper.ErrSkipProcessingData
	}
	return processorhelper.NewMetrics(ctx, set, cfg, next, fn,
		processorhelper.WithStart(b.start),
		processorhelper.WithShutdown(b.shutdown),
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}),
	)
}

func createLogsProcessor(ctx context.Context, set processor.Settings, cfg component.Config, next consumer.Logs) (processor.Logs, error) {
	b := newLogsBatcher(cfg.(*Config), next, set.Logger)
	fn := func(fCtx context.Context, ld plog.Logs) (plog.Logs, error) {
		if err := b.consume(fCtx, ld); err != nil {
			return plog.Logs{}, err
		}
		return plog.Logs{}, processorhelper.ErrSkipProcessingData
	}
	return processorhelper.NewLogs(ctx, set, cfg, next, fn,
		processorhelper.WithStart(b.start),
		processorhelper.WithShutdown(b.shutdown),
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}),
	)
}
