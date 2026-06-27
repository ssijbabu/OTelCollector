// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azureeventhubexporter

import (
	"context"
	"errors"
	"testing"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.uber.org/zap"
)

// fakeSyncProducer implements syncProducer for testing without a live Kafka broker.
type fakeSyncProducer struct {
	messages []*sarama.ProducerMessage
	sendErr  error
	closed   bool
}

func (f *fakeSyncProducer) SendMessage(msg *sarama.ProducerMessage) (int32, int64, error) {
	f.messages = append(f.messages, msg)
	return 0, 0, f.sendErr
}

func (f *fakeSyncProducer) Close() error {
	f.closed = true
	return nil
}

// newTestKafkaExporter builds an exporter wired to a fake Kafka producer,
// bypassing the start() path so tests run without a live broker.
func newTestKafkaExporter(cfg *Config, fake *fakeSyncProducer) *azureEventHubExporter {
	e := &azureEventHubExporter{
		config: cfg,
		logger: zap.NewNop(),
		kafkaSender: &kafkaSenderImpl{
			producer: fake,
			topic:    "test-hub",
		},
	}
	e.doSend = e.kafkaSend
	return e
}

// --- kafkaSenderImpl unit tests ---

func TestKafkaSend_NoPartitionKey(t *testing.T) {
	fake := &fakeSyncProducer{}
	exp := newTestKafkaExporter(&Config{Protocol: ProtocolKafka}, fake)

	require.NoError(t, exp.kafkaSend(context.Background(), "", []byte("payload")))
	require.Len(t, fake.messages, 1)
	assert.Equal(t, "test-hub", fake.messages[0].Topic)
	assert.Nil(t, fake.messages[0].Key, "key must be nil when partitionKey is empty")
}

func TestKafkaSend_WithPartitionKey(t *testing.T) {
	fake := &fakeSyncProducer{}
	exp := newTestKafkaExporter(&Config{Protocol: ProtocolKafka}, fake)

	require.NoError(t, exp.kafkaSend(context.Background(), "trace-abc", []byte("payload")))
	require.Len(t, fake.messages, 1)
	raw, err := fake.messages[0].Key.Encode()
	require.NoError(t, err)
	assert.Equal(t, "trace-abc", string(raw))
}

func TestKafkaSend_Error(t *testing.T) {
	fake := &fakeSyncProducer{sendErr: errors.New("broker unavailable")}
	exp := newTestKafkaExporter(&Config{Protocol: ProtocolKafka}, fake)

	err := exp.kafkaSend(context.Background(), "", []byte("payload"))
	assert.ErrorContains(t, err, "broker unavailable")
}

func TestKafkaShutdown_ClosesCalled(t *testing.T) {
	fake := &fakeSyncProducer{}
	exp := newTestKafkaExporter(&Config{Protocol: ProtocolKafka}, fake)

	require.NoError(t, exp.shutdown(context.Background()))
	assert.True(t, fake.closed)
}

func TestKafkaShutdown_NilSender(t *testing.T) {
	exp := &azureEventHubExporter{config: &Config{Protocol: ProtocolKafka}, logger: zap.NewNop()}
	assert.NoError(t, exp.shutdown(context.Background()))
}

// --- Consume* tests routed through the Kafka sender ---

func TestKafkaConsumeLogs_NoPartitioning(t *testing.T) {
	var sent []sentEvent
	exp := newTestKafkaExporter(&Config{Protocol: ProtocolKafka}, &fakeSyncProducer{})
	exp.doSend = captureSender(&sent)

	require.NoError(t, exp.ConsumeLogs(context.Background(), makeLogs(2)))
	require.Len(t, sent, 1)
	assert.Equal(t, "", sent[0].partitionKey)
	assert.NotEmpty(t, sent[0].body)
}

func TestKafkaConsumeMetrics_NoPartitioning(t *testing.T) {
	var sent []sentEvent
	exp := newTestKafkaExporter(&Config{Protocol: ProtocolKafka}, &fakeSyncProducer{})
	exp.doSend = captureSender(&sent)

	require.NoError(t, exp.ConsumeMetrics(context.Background(), makeMetrics(3)))
	require.Len(t, sent, 1)
}

func TestKafkaConsumeTraces_NoPartitioning(t *testing.T) {
	var sent []sentEvent
	exp := newTestKafkaExporter(&Config{Protocol: ProtocolKafka}, &fakeSyncProducer{})
	exp.doSend = captureSender(&sent)

	require.NoError(t, exp.ConsumeTraces(context.Background(), makeTraces(2)))
	require.Len(t, sent, 1)
}

// --- start() error-path tests for Kafka (no live broker required) ---

func TestStartKafka_AuthExtensionNotFound(t *testing.T) {
	id := component.MustNewID("azure_auth")
	exp := newTestExporter(&Config{
		Protocol: ProtocolKafka,
		Auth:     &id,
		EventHub: EventHubConfig{Name: "hub", Namespace: "ns.servicebus.windows.net"},
	})
	err := exp.start(context.Background(), componenttest.NewNopHost())
	assert.ErrorContains(t, err, "failed to resolve auth extension")
}

func TestStartKafka_AuthExtensionWrongType(t *testing.T) {
	id := component.MustNewID("azure_auth")
	exp := newTestExporter(&Config{
		Protocol: ProtocolKafka,
		Auth:     &id,
		EventHub: EventHubConfig{Name: "hub", Namespace: "ns.servicebus.windows.net"},
	})
	host := newHostWithExtension(id, &notACredential{})
	err := exp.start(context.Background(), host)
	assert.ErrorContains(t, err, "does not implement azcore.TokenCredential")
}
