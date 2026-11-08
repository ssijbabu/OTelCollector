package azureactivitylogreceiver

import (
	"net/url"
	"path/filepath"
	"strings"
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
	sub, err := cm.Sub("azure_activity_log")
	require.NoError(t, err)

	cfg := createDefaultConfig().(*Config)
	require.NoError(t, sub.Unmarshal(cfg))
	require.NoError(t, cfg.Validate())

	authID := component.MustNewID("azure_auth")
	assert.Equal(t, &Config{
		SubscriptionID:     "00000000-0000-0000-0000-000000000000",
		Auth:               &authID,
		ResourceGroup:      "example-rg",
		CollectionInterval: 2 * time.Minute,
		MaxIngestionDelay:  20 * time.Minute,
		InitialLookback:    24 * time.Hour,
		InitialDelay:       time.Second,
		Timeout:            time.Minute,
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
		{"two filters", func(c *Config) { c.ResourceGroup = "rg"; c.ResourceProvider = "Microsoft.Compute" }, "at most one of"},
		{"interval too short", func(c *Config) { c.CollectionInterval = 10 * time.Second }, "at least 30s"},
		{"delay too long", func(c *Config) { c.MaxIngestionDelay = 48 * time.Hour }, "max_ingestion_delay"},
		{"lookback beyond retention", func(c *Config) { c.InitialLookback = 91 * 24 * time.Hour }, "initial_lookback"},
		{"bad endpoint", func(c *Config) { c.Endpoint = "management.azure.com" }, "absolute URL"},
		{"no timeout", func(c *Config) { c.Timeout = 0 }, "timeout must be positive"},
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
	from := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)

	u, err := url.Parse(cfg.requestURL(from, to))
	require.NoError(t, err)
	assert.Equal(t, "/subscriptions/sub/providers/Microsoft.Insights/eventtypes/management/values", u.Path)
	assert.Equal(t, "2015-04-01", u.Query().Get("api-version"))
	assert.Equal(t, "eventTimestamp ge '2026-11-07T10:00:00Z' and eventTimestamp le '2026-11-07T10:01:00Z'", u.Query().Get("$filter"))
	assert.NotContains(t, u.RawQuery, "+", "spaces must be encoded as %20")

	cfg.ResourceGroup = "rg"
	assert.True(t, strings.HasSuffix(cfg.filter(from, to), " and resourceGroupName eq 'rg'"))
	cfg.ResourceGroup, cfg.ResourceID = "", "/subscriptions/sub/resourceGroups/rg/providers/x/y/o'brien"
	assert.True(t, strings.HasSuffix(cfg.filter(from, to), " and resourceUri eq '/subscriptions/sub/resourceGroups/rg/providers/x/y/o''brien'"))
	cfg.ResourceID, cfg.ResourceProvider = "", "Microsoft.ContainerService"
	assert.True(t, strings.HasSuffix(cfg.filter(from, to), " and resourceProvider eq 'Microsoft.ContainerService'"))

	cfg.Endpoint = "https://management.usgovcloudapi.net/"
	assert.Equal(t, "https://management.usgovcloudapi.net/.default", cfg.scope())
}
