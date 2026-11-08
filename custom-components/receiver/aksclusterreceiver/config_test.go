package aksclusterreceiver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap/confmaptest"
)

func TestLoadConfig(t *testing.T) {
	cm, err := confmaptest.LoadConf(filepath.Join("testdata", "config.yaml"))
	require.NoError(t, err)
	sub, err := cm.Sub("aks")
	require.NoError(t, err)

	cfg := createDefaultConfig().(*Config)
	require.NoError(t, sub.Unmarshal(cfg))
	require.NoError(t, cfg.Validate())

	authID := component.MustNewID("azure_auth")
	assert.Equal(t, &Config{
		SubscriptionID:     "00000000-0000-0000-0000-000000000000",
		ResourceGroup:      "example-rg",
		ClusterName:        "example-aks",
		Auth:               &authID,
		CollectionInterval: 10 * time.Minute,
		InitialDelay:       time.Second,
		Timeout:            30 * time.Second,
		Endpoint:           defaultEndpoint,
		APIVersion:         defaultAPIVersion,
	}, cfg)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"valid", func(*Config) {}, ""},
		{"no subscription", func(c *Config) { c.SubscriptionID = "" }, "subscription_id is required"},
		{"no auth", func(c *Config) { c.Auth = nil }, "auth is required"},
		{"cluster without rg", func(c *Config) { c.ClusterName = "x" }, "cluster_name requires resource_group"},
		{"interval too short", func(c *Config) { c.CollectionInterval = 10 * time.Second }, "at least 1m"},
		{"bad endpoint", func(c *Config) { c.Endpoint = "management.azure.com" }, "absolute URL"},
		{"no timeout", func(c *Config) { c.Timeout = 0 }, "timeout must be positive"},
		{"no api version", func(c *Config) { c.APIVersion = "" }, "api_version is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createDefaultConfig().(*Config)
			cfg.SubscriptionID = "sub"
			authID := component.MustNewID("azure_auth")
			cfg.Auth = &authID
			tt.mutate(cfg)
			err := cfg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestRequestURL(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.SubscriptionID = "sub"
	assert.Equal(t, "https://management.azure.com/subscriptions/sub/providers/Microsoft.ContainerService/managedClusters?api-version=2025-05-01", cfg.requestURL())

	cfg.ResourceGroup = "rg"
	assert.Equal(t, "https://management.azure.com/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters?api-version=2025-05-01", cfg.requestURL())

	cfg.ClusterName = "c1"
	cfg.Endpoint = "https://management.usgovcloudapi.net/"
	assert.Equal(t, "https://management.usgovcloudapi.net/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/c1?api-version=2025-05-01", cfg.requestURL())
	assert.Equal(t, "https://management.usgovcloudapi.net/.default", cfg.scope())
}
