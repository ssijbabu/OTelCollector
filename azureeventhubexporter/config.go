// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azureeventhubexporter // import "github.com/ssijbabu/azureeventhubexporter"

import (
	"errors"
	"fmt"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

// Protocol selects the wire protocol used to send messages to Azure Event Hubs.
type Protocol string

const (
	// ProtocolAMQP uses the native Azure Event Hubs AMQP protocol (default).
	ProtocolAMQP Protocol = "amqp"
	// ProtocolKafka uses the Azure Event Hubs Kafka-compatible endpoint.
	ProtocolKafka Protocol = "kafka"
)

var errLogsPartitionExclusive = errors.New("partition_logs_by_resource_attributes and partition_logs_by_trace_id cannot both be enabled")

// Config defines the configuration for the Azure Event Hub exporter.
type Config struct {
	// Protocol selects the wire protocol: "amqp" (default) or "kafka".
	// When "kafka", the Kafka endpoint is derived from event_hub.namespace as
	// "<namespace>:9093" with TLS and SASL enabled automatically.
	Protocol Protocol `mapstructure:"protocol"`

	// EventHub identifies the target Event Hub and holds the credentials.
	EventHub EventHubConfig `mapstructure:"event_hub"`

	// Auth is the component ID of an auth extension that implements azcore.TokenCredential
	// (e.g. the azureauthextension). When set, event_hub.shared_access_key* fields are ignored.
	Auth *component.ID `mapstructure:"auth"`

	// PartitionTracesByID sets the partition key of outgoing trace messages to the trace ID,
	// ensuring all spans belonging to the same trace land on the same Event Hub partition.
	PartitionTracesByID bool `mapstructure:"partition_traces_by_id"`

	// PartitionMetricsByResourceAttributes splits the outgoing metrics batch by resource and
	// sets the partition key to a hash of each resource's attributes, so metrics from the
	// same resource are consistently routed to the same partition.
	PartitionMetricsByResourceAttributes bool `mapstructure:"partition_metrics_by_resource_attributes"`

	// PartitionLogsByResourceAttributes splits the outgoing logs batch by resource and sets
	// the partition key to a hash of each resource's attributes.
	// Mutually exclusive with PartitionLogsByTraceID.
	PartitionLogsByResourceAttributes bool `mapstructure:"partition_logs_by_resource_attributes"`

	// PartitionLogsByTraceID sets the partition key of outgoing log messages to the trace ID
	// found on each log record, co-locating logs with their associated traces.
	// Mutually exclusive with PartitionLogsByResourceAttributes.
	PartitionLogsByTraceID bool `mapstructure:"partition_logs_by_trace_id"`

	// TimeoutSettings controls per-request timeouts.
	TimeoutSettings exporterhelper.TimeoutConfig `mapstructure:"timeout"`

	// QueueSettings configures the sending queue / batcher.
	QueueSettings configoptional.Optional[exporterhelper.QueueBatchConfig] `mapstructure:"sending_queue"`

	// BackOffConfig configures retry-on-failure behaviour.
	BackOffConfig configretry.BackOffConfig `mapstructure:"retry_on_failure"`
}

// EventHubConfig identifies the target Event Hub and holds SAS key credentials.
// Namespace and Name are always required. SharedAccessKey* are required when Auth is not set.
type EventHubConfig struct {
	// Namespace is the fully qualified Event Hubs namespace,
	// e.g. "mynamespace.servicebus.windows.net".
	Namespace string `mapstructure:"namespace"`

	// Name is the Event Hub name (Kafka topic).
	Name string `mapstructure:"name"`

	// SharedAccessKeyName is the SAS policy name.
	// Required when Auth is not set.
	SharedAccessKeyName string `mapstructure:"shared_access_key_name"`

	// SharedAccessKey is the SAS key value.
	// Required when Auth is not set.
	SharedAccessKey string `mapstructure:"shared_access_key"`
}

// buildConnectionString assembles an Azure Event Hubs connection string from the
// individual fields. Used internally by the AMQP and Kafka SAS-key paths.
func (c *EventHubConfig) buildConnectionString() string {
	return fmt.Sprintf(
		"Endpoint=sb://%s/;SharedAccessKeyName=%s;SharedAccessKey=%s;EntityPath=%s",
		c.Namespace, c.SharedAccessKeyName, c.SharedAccessKey, c.Name,
	)
}

func (c *Config) Validate() error {
	if c.Protocol != "" && c.Protocol != ProtocolAMQP && c.Protocol != ProtocolKafka {
		return fmt.Errorf("unsupported protocol %q: must be %q or %q", c.Protocol, ProtocolAMQP, ProtocolKafka)
	}
	if c.EventHub.Namespace == "" {
		return errors.New("event_hub.namespace is required")
	}
	if c.EventHub.Name == "" {
		return errors.New("event_hub.name is required")
	}
	if c.Auth == nil {
		if c.EventHub.SharedAccessKeyName == "" {
			return errors.New("event_hub.shared_access_key_name is required when not using an auth extension")
		}
		if c.EventHub.SharedAccessKey == "" {
			return errors.New("event_hub.shared_access_key is required when not using an auth extension")
		}
	}
	if c.PartitionLogsByResourceAttributes && c.PartitionLogsByTraceID {
		return errLogsPartitionExclusive
	}
	return nil
}
