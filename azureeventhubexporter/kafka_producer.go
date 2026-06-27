// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azureeventhubexporter // import "github.com/ssijbabu/azureeventhubexporter"

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/IBM/sarama"
)

// syncProducer is the subset of sarama.SyncProducer used by kafkaSenderImpl,
// defined as an interface to allow test injection without a live broker.
type syncProducer interface {
	SendMessage(*sarama.ProducerMessage) (int32, int64, error)
	Close() error
}

// kafkaSenderImpl sends telemetry via the Azure Event Hubs Kafka-compatible endpoint.
type kafkaSenderImpl struct {
	producer syncProducer
	topic    string
}

func (k *kafkaSenderImpl) send(_ context.Context, partitionKey string, body []byte) error {
	msg := &sarama.ProducerMessage{
		Topic: k.topic,
		Value: sarama.ByteEncoder(body),
	}
	if partitionKey != "" {
		msg.Key = sarama.StringEncoder(partitionKey)
	}
	if _, _, err := k.producer.SendMessage(msg); err != nil {
		return fmt.Errorf("failed to send Kafka message: %w", err)
	}
	return nil
}

func (k *kafkaSenderImpl) close() error {
	return k.producer.Close()
}

// newKafkaSenderWithSASKey creates a kafkaSenderImpl authenticated via SASL/PLAIN,
// using "$ConnectionString" as the username and the connection string assembled from
// event_hub fields as the password — the standard Azure Event Hubs Kafka auth pattern.
func newKafkaSenderWithSASKey(eventHubCfg EventHubConfig) (*kafkaSenderImpl, error) {
	cfg := newBaseKafkaConfig()
	cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
	cfg.Net.SASL.User = "$ConnectionString"
	cfg.Net.SASL.Password = eventHubCfg.buildConnectionString()

	producer, err := sarama.NewSyncProducer([]string{eventHubCfg.Namespace + ":9093"}, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}
	return &kafkaSenderImpl{producer: producer, topic: eventHubCfg.Name}, nil
}

// newKafkaSenderFromCredential creates a kafkaSenderImpl authenticated via SASL/OAUTHBEARER,
// fetching tokens from the supplied Azure credential on each handshake.
func newKafkaSenderFromCredential(credential azcore.TokenCredential, eventHubCfg EventHubConfig) (*kafkaSenderImpl, error) {
	cfg := newBaseKafkaConfig()
	cfg.Net.SASL.Mechanism = sarama.SASLTypeOAuth
	cfg.Net.SASL.TokenProvider = &azureTokenProvider{credential: credential}

	producer, err := sarama.NewSyncProducer([]string{eventHubCfg.Namespace + ":9093"}, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}
	return &kafkaSenderImpl{producer: producer, topic: eventHubCfg.Name}, nil
}

// newBaseKafkaConfig returns a sarama config pre-configured for the Azure Event Hubs
// Kafka endpoint: TLS on, SASL on, version pinned to 1.0.0.0 (maximum supported by Event Hubs).
func newBaseKafkaConfig() *sarama.Config {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V1_0_0_0
	cfg.Net.TLS.Enable = true
	cfg.Net.SASL.Enable = true
	cfg.Net.SASL.Handshake = true
	cfg.Producer.Return.Successes = true
	return cfg
}

// azureTokenProvider fetches Azure AD OAuth tokens for SASL/OAUTHBEARER auth.
// sarama calls Token() on each SASL handshake; the azcore credential handles caching.
type azureTokenProvider struct {
	credential azcore.TokenCredential
}

func (p *azureTokenProvider) Token() (*sarama.AccessToken, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	token, err := p.credential.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://eventhubs.azure.net/.default"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to obtain Azure token for Kafka auth: %w", err)
	}
	return &sarama.AccessToken{Token: token.Token}, nil
}
