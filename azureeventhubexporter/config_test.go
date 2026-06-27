// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package azureeventhubexporter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/component"
)

func TestConfigValidate(t *testing.T) {
	authID := component.MustNewID("azure_auth")

	// Base valid SAS-key config, used as a foundation for table entries.
	validEventHub := EventHubConfig{
		Namespace:           "test.servicebus.windows.net",
		Name:                "hub",
		SharedAccessKeyName: "key",
		SharedAccessKey:     "dGVzdA==",
	}

	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name:    "missing namespace",
			cfg:     &Config{},
			wantErr: "event_hub.namespace is required",
		},
		{
			name:    "missing name",
			cfg:     &Config{EventHub: EventHubConfig{Namespace: "test.servicebus.windows.net"}},
			wantErr: "event_hub.name is required",
		},
		{
			name: "missing shared_access_key_name",
			cfg: &Config{EventHub: EventHubConfig{
				Namespace: "test.servicebus.windows.net",
				Name:      "hub",
			}},
			wantErr: "event_hub.shared_access_key_name is required",
		},
		{
			name: "missing shared_access_key",
			cfg: &Config{EventHub: EventHubConfig{
				Namespace:           "test.servicebus.windows.net",
				Name:                "hub",
				SharedAccessKeyName: "key",
			}},
			wantErr: "event_hub.shared_access_key is required",
		},
		{
			name: "valid sas key config",
			cfg:  &Config{EventHub: validEventHub},
		},
		{
			name: "auth without event_hub.namespace",
			cfg: &Config{
				Auth:     &authID,
				EventHub: EventHubConfig{Name: "hub"},
			},
			wantErr: "event_hub.namespace is required",
		},
		{
			name: "auth without event_hub.name",
			cfg: &Config{
				Auth:     &authID,
				EventHub: EventHubConfig{Namespace: "test.servicebus.windows.net"},
			},
			wantErr: "event_hub.name is required",
		},
		{
			name: "valid auth config",
			cfg: &Config{
				Auth: &authID,
				EventHub: EventHubConfig{
					Namespace: "test.servicebus.windows.net",
					Name:      "hub",
				},
			},
		},
		{
			name: "partition_logs_by_resource_attributes and partition_logs_by_trace_id are mutually exclusive",
			cfg: &Config{
				EventHub:                          validEventHub,
				PartitionLogsByResourceAttributes: true,
				PartitionLogsByTraceID:            true,
			},
			wantErr: errLogsPartitionExclusive.Error(),
		},
		{
			name: "partition_logs_by_resource_attributes alone is valid",
			cfg: &Config{
				EventHub:                          validEventHub,
				PartitionLogsByResourceAttributes: true,
			},
		},
		{
			name: "partition_logs_by_trace_id alone is valid",
			cfg: &Config{
				EventHub:               validEventHub,
				PartitionLogsByTraceID: true,
			},
		},
		{
			name: "partition_traces_by_id is valid",
			cfg: &Config{
				EventHub:            validEventHub,
				PartitionTracesByID: true,
			},
		},
		{
			name: "partition_metrics_by_resource_attributes is valid",
			cfg: &Config{
				EventHub:                             validEventHub,
				PartitionMetricsByResourceAttributes: true,
			},
		},
		{
			name: "protocol amqp is valid",
			cfg:  &Config{EventHub: validEventHub, Protocol: ProtocolAMQP},
		},
		{
			name: "protocol kafka is valid",
			cfg:  &Config{EventHub: validEventHub, Protocol: ProtocolKafka},
		},
		{
			name:    "unsupported protocol",
			cfg:     &Config{EventHub: validEventHub, Protocol: "grpc"},
			wantErr: "unsupported protocol",
		},
		{
			name: "kafka with auth requires event_hub.name",
			cfg: &Config{
				Auth:     &authID,
				Protocol: ProtocolKafka,
				EventHub: EventHubConfig{Namespace: "test.servicebus.windows.net"},
			},
			wantErr: "event_hub.name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
