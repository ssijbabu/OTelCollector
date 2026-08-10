// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azureeventhubexporter

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
	"github.com/IBM/sarama"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/batchpersignal"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/pdatautil"
)

// traceIDToHex converts a TraceID to its hex string representation.
// Returns an empty string for a zero (unset) trace ID.
func traceIDToHex(id pcommon.TraceID) string {
	if id.IsEmpty() {
		return ""
	}
	return id.String()
}

// producerClient is the subset of azeventhubs.ProducerClient used by the exporter.
// Defined as an interface to allow test injection.
type producerClient interface {
	NewEventDataBatch(ctx context.Context, options *azeventhubs.EventDataBatchOptions) (*azeventhubs.EventDataBatch, error)
	SendEventDataBatch(ctx context.Context, batch *azeventhubs.EventDataBatch, options *azeventhubs.SendEventDataBatchOptions) error
	Close(ctx context.Context) error
}

type azureEventHubExporter struct {
	config      *Config
	producer    producerClient   // AMQP path; nil when using Kafka
	kafkaSender *kafkaSenderImpl // Kafka path; nil when using AMQP
	logger      *zap.Logger
	// doSend is the function called by Consume* methods to dispatch a single
	// (partitionKey, body) pair. It defaults to e.send (AMQP) and can be
	// replaced in tests to capture output without a live connection.
	doSend func(ctx context.Context, partitionKey string, body []byte) error
	// addEventFn wraps batch.AddEventData so tests can inject ErrEventDataTooLarge
	// without needing a real EventDataBatch (the SDK panics on a nil receiver).
	addEventFn        func(*azeventhubs.EventDataBatch, *azeventhubs.EventData) error
	droppedBatches    metric.Int64Counter
	droppedBytes      metric.Int64Counter
	sentBatches       metric.Int64Counter
	sentBytes         metric.Int64Counter
	sendDuration      metric.Float64Histogram
	partitionsPerBatch metric.Int64Histogram
}

func newExporter(config *Config, set exporter.Settings) *azureEventHubExporter {
	e := &azureEventHubExporter{
		config: config,
		logger: set.Logger,
	}
	e.doSend = e.send
	e.addEventFn = func(b *azeventhubs.EventDataBatch, ed *azeventhubs.EventData) error {
		return b.AddEventData(ed, nil)
	}

	meter := set.TelemetrySettings.MeterProvider.Meter("github.com/ssijbabu/azureeventhubexporter")
	e.droppedBatches, _ = meter.Int64Counter(
		"azureeventhub_exporter_dropped_batches",
		metric.WithDescription("Number of telemetry batches dropped because they exceeded the Event Hub message size limit"),
		metric.WithUnit("{batch}"),
	)
	e.droppedBytes, _ = meter.Int64Counter(
		"azureeventhub_exporter_dropped_bytes",
		metric.WithDescription("Bytes of telemetry dropped because the payload exceeded the Event Hub message size limit"),
		metric.WithUnit("By"),
	)
	e.sentBatches, _ = meter.Int64Counter(
		"azureeventhub_exporter_sent_batches",
		metric.WithDescription("Number of telemetry batches successfully sent to Event Hub"),
		metric.WithUnit("{batch}"),
	)
	e.sentBytes, _ = meter.Int64Counter(
		"azureeventhub_exporter_sent_bytes",
		metric.WithDescription("Bytes of telemetry successfully sent to Event Hub"),
		metric.WithUnit("By"),
	)
	e.sendDuration, _ = meter.Float64Histogram(
		"azureeventhub_exporter_send_duration",
		metric.WithDescription("Wall-clock time of a single successful Event Hub send call"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000),
	)
	e.partitionsPerBatch, _ = meter.Int64Histogram(
		"azureeventhub_exporter_partitions_per_batch",
		metric.WithDescription("Number of Event Hub messages produced per Consume call (one per partition group)"),
		metric.WithUnit("{message}"),
		metric.WithExplicitBucketBoundaries(1, 2, 5, 10, 25, 50, 100, 250, 500),
	)
	return e
}

func (e *azureEventHubExporter) start(_ context.Context, host component.Host) error {
	if e.config.Protocol == ProtocolKafka {
		return e.startKafka(host)
	}
	return e.startAMQP(host)
}

func (e *azureEventHubExporter) startAMQP(host component.Host) error {
	if e.config.Auth != nil {
		ext, ok := host.GetExtensions()[*e.config.Auth]
		if !ok {
			return fmt.Errorf("failed to resolve auth extension %q", *e.config.Auth)
		}
		credential, ok := ext.(azcore.TokenCredential)
		if !ok {
			return fmt.Errorf("extension %q does not implement azcore.TokenCredential", *e.config.Auth)
		}
		producer, err := azeventhubs.NewProducerClient(
			e.config.EventHub.Namespace,
			e.config.EventHub.Name,
			credential,
			nil,
		)
		if err != nil {
			return fmt.Errorf("failed to create Event Hub producer client: %w", err)
		}
		e.producer = producer
		return nil
	}

	// EntityPath is already embedded in the built connection string; pass "" to avoid conflict.
	producer, err := azeventhubs.NewProducerClientFromConnectionString(
		e.config.EventHub.buildConnectionString(),
		"",
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create Event Hub producer client: %w", err)
	}
	e.producer = producer
	return nil
}

func (e *azureEventHubExporter) startKafka(host component.Host) error {
	if e.config.Auth != nil {
		ext, ok := host.GetExtensions()[*e.config.Auth]
		if !ok {
			return fmt.Errorf("failed to resolve auth extension %q", *e.config.Auth)
		}
		credential, ok := ext.(azcore.TokenCredential)
		if !ok {
			return fmt.Errorf("extension %q does not implement azcore.TokenCredential", *e.config.Auth)
		}
		s, err := newKafkaSenderFromCredential(credential, e.config.EventHub)
		if err != nil {
			return err
		}
		e.kafkaSender = s
	} else {
		s, err := newKafkaSenderWithSASKey(e.config.EventHub)
		if err != nil {
			return err
		}
		e.kafkaSender = s
	}
	e.doSend = e.kafkaSend
	return nil
}

func (e *azureEventHubExporter) shutdown(ctx context.Context) error {
	if e.kafkaSender != nil {
		return e.kafkaSender.close()
	}
	if e.producer != nil {
		return e.producer.Close(ctx)
	}
	return nil
}

// kafkaSend forwards a single (partitionKey, body) pair to the Kafka sender.
func (e *azureEventHubExporter) kafkaSend(ctx context.Context, partitionKey string, body []byte) error {
	start := time.Now()
	err := e.kafkaSender.send(ctx, partitionKey, body)
	if errors.Is(err, sarama.ErrMessageSizeTooLarge) {
		e.logger.Warn("dropping telemetry: payload exceeds Event Hub Kafka broker message size limit",
			zap.Int("bytes", len(body)),
		)
		e.droppedBatches.Add(ctx, 1)
		e.droppedBytes.Add(ctx, int64(len(body)))
		return nil
	}
	if err != nil {
		return err
	}
	proto := attribute.String("protocol", "kafka")
	e.sendDuration.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(proto))
	e.sentBatches.Add(ctx, 1, metric.WithAttributes(proto))
	e.sentBytes.Add(ctx, int64(len(body)), metric.WithAttributes(proto))
	return nil
}

// ConsumeLogs splits the batch according to the configured partition strategy and
// sends each partition group as a separate Event Hub message.
func (e *azureEventHubExporter) ConsumeLogs(ctx context.Context, ld plog.Logs) error {
	var n int64
	for partitionKey, partialLogs := range e.partitionLogs(ld) {
		body, err := (&plog.JSONMarshaler{}).MarshalLogs(partialLogs)
		if err != nil {
			return fmt.Errorf("failed to marshal logs: %w", err)
		}
		if err = e.doSend(ctx, partitionKey, body); err != nil {
			return err
		}
		n++
	}
	e.partitionsPerBatch.Record(ctx, n, metric.WithAttributes(attribute.String("signal", "logs")))
	return nil
}

// ConsumeMetrics splits the batch according to the configured partition strategy and
// sends each partition group as a separate Event Hub message.
func (e *azureEventHubExporter) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	var n int64
	for partitionKey, partialMetrics := range e.partitionMetrics(md) {
		body, err := (&pmetric.JSONMarshaler{}).MarshalMetrics(partialMetrics)
		if err != nil {
			return fmt.Errorf("failed to marshal metrics: %w", err)
		}
		if err = e.doSend(ctx, partitionKey, body); err != nil {
			return err
		}
		n++
	}
	e.partitionsPerBatch.Record(ctx, n, metric.WithAttributes(attribute.String("signal", "metrics")))
	return nil
}

// ConsumeTraces splits the batch according to the configured partition strategy and
// sends each partition group as a separate Event Hub message.
func (e *azureEventHubExporter) ConsumeTraces(ctx context.Context, td ptrace.Traces) error {
	var n int64
	for partitionKey, partialTraces := range e.partitionTraces(td) {
		body, err := (&ptrace.JSONMarshaler{}).MarshalTraces(partialTraces)
		if err != nil {
			return fmt.Errorf("failed to marshal traces: %w", err)
		}
		if err = e.doSend(ctx, partitionKey, body); err != nil {
			return err
		}
		n++
	}
	e.partitionsPerBatch.Record(ctx, n, metric.WithAttributes(attribute.String("signal", "traces")))
	return nil
}

// partitionLogs returns an iterator that yields (partitionKey, partial Logs) pairs.
// The partitioning strategy is determined by the config flags.
func (e *azureEventHubExporter) partitionLogs(ld plog.Logs) iter.Seq2[string, plog.Logs] {
	return func(yield func(string, plog.Logs) bool) {
		if e.config.PartitionLogsByResourceAttributes {
			// One message per resource; key = hash of resource attributes.
			newLogs := plog.NewLogs()
			target := newLogs.ResourceLogs().AppendEmpty()
			for _, resourceLogs := range ld.ResourceLogs().All() {
				hash := pdatautil.MapHash(resourceLogs.Resource().Attributes())
				resourceLogs.CopyTo(target)
				if !yield(string(hash[:]), newLogs) {
					return
				}
			}
			return
		}
		if e.config.PartitionLogsByTraceID {
			// One message per trace ID found in log records; key = trace ID hex string.
			for _, l := range batchpersignal.SplitLogs(ld) {
				traceID := l.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).TraceID()
				key := traceIDToHex(traceID)
				if !yield(key, l) {
					return
				}
			}
			return
		}
		// No partitioning: send the whole batch as a single message with no key.
		yield("", ld)
	}
}

// partitionMetrics returns an iterator that yields (partitionKey, partial Metrics) pairs.
func (e *azureEventHubExporter) partitionMetrics(md pmetric.Metrics) iter.Seq2[string, pmetric.Metrics] {
	return func(yield func(string, pmetric.Metrics) bool) {
		if e.config.PartitionMetricsByResourceAttributes {
			// One message per resource; key = hash of resource attributes.
			newMetrics := pmetric.NewMetrics()
			target := newMetrics.ResourceMetrics().AppendEmpty()
			for _, resourceMetrics := range md.ResourceMetrics().All() {
				hash := pdatautil.MapHash(resourceMetrics.Resource().Attributes())
				resourceMetrics.CopyTo(target)
				if !yield(string(hash[:]), newMetrics) {
					return
				}
			}
			return
		}
		yield("", md)
	}
}

// partitionTraces returns an iterator that yields (partitionKey, partial Traces) pairs.
func (e *azureEventHubExporter) partitionTraces(td ptrace.Traces) iter.Seq2[string, ptrace.Traces] {
	return func(yield func(string, ptrace.Traces) bool) {
		if e.config.PartitionTracesByID {
			// One message per trace; key = trace ID hex string.
			// batchpersignal.SplitTraces guarantees exactly one trace ID per returned value.
			for _, t := range batchpersignal.SplitTraces(td) {
				key := traceIDToHex(
					t.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).TraceID(),
				)
				if !yield(key, t) {
					return
				}
			}
			return
		}
		yield("", td)
	}
}

// send creates an Event Hub batch optionally pinned to a partition key and sends it.
// An empty partitionKey means Event Hubs will pick the partition (round-robin).
func (e *azureEventHubExporter) send(ctx context.Context, partitionKey string, body []byte) error {
	start := time.Now()
	var opts *azeventhubs.EventDataBatchOptions
	if partitionKey != "" {
		opts = &azeventhubs.EventDataBatchOptions{PartitionKey: &partitionKey}
	}

	batch, err := e.producer.NewEventDataBatch(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to create event data batch: %w", err)
	}

	if err = e.addEventFn(batch, &azeventhubs.EventData{Body: body}); err != nil {
		if errors.Is(err, azeventhubs.ErrEventDataTooLarge) {
			e.logger.Warn("dropping telemetry: payload exceeds Event Hub message size limit",
				zap.Int("bytes", len(body)),
			)
			e.droppedBatches.Add(ctx, 1)
			e.droppedBytes.Add(ctx, int64(len(body)))
			return nil
		}
		return fmt.Errorf("failed to add event data to batch: %w", err)
	}

	if err = e.producer.SendEventDataBatch(ctx, batch, nil); err != nil {
		return fmt.Errorf("failed to send event data batch: %w", err)
	}
	proto := attribute.String("protocol", "amqp")
	e.sendDuration.Record(ctx, float64(time.Since(start).Milliseconds()), metric.WithAttributes(proto))
	e.sentBatches.Add(ctx, 1, metric.WithAttributes(proto))
	e.sentBytes.Add(ctx, int64(len(body)), metric.WithAttributes(proto))
	return nil
}
