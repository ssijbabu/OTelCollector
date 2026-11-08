// Package aksclusterreceiver collects the state of Azure Kubernetes Service clusters
// (version, power state, node pools) from the Azure Resource Manager API.
package aksclusterreceiver

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
)

const typeStr = "aks"

var componentType = component.MustNewType(typeStr)

// NewFactory creates a factory for the aks receiver.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		componentType,
		createDefaultConfig,
		receiver.WithMetrics(createMetricsReceiver, component.StabilityLevelDevelopment),
		receiver.WithLogs(createLogsReceiver, component.StabilityLevelDevelopment),
	)
}

func createMetricsReceiver(_ context.Context, set receiver.Settings, cfg component.Config, next consumer.Metrics) (receiver.Metrics, error) {
	r := newReceiver(set, cfg.(*Config))
	r.metrics = next
	return r, nil
}

func createLogsReceiver(_ context.Context, set receiver.Settings, cfg component.Config, next consumer.Logs) (receiver.Logs, error) {
	r := newReceiver(set, cfg.(*Config))
	r.logs = next
	return r, nil
}
