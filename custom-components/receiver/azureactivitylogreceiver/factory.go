// Package azureactivitylogreceiver reads the Azure activity log (control-plane
// events for a subscription) from the Azure Resource Manager API.
package azureactivitylogreceiver

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
)

const typeStr = "azure_activity_log"

var componentType = component.MustNewType(typeStr)

// NewFactory creates a factory for the azure_activity_log receiver.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		componentType,
		createDefaultConfig,
		receiver.WithLogs(createLogsReceiver, component.StabilityLevelDevelopment),
	)
}

func createLogsReceiver(_ context.Context, set receiver.Settings, cfg component.Config, next consumer.Logs) (receiver.Logs, error) {
	return newReceiver(set, cfg.(*Config), next), nil
}
