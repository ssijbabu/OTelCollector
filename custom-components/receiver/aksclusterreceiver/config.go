package aksclusterreceiver

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
)

const (
	defaultEndpoint   = "https://management.azure.com"
	defaultAPIVersion = "2025-05-01"
)

// Config defines the configuration of the aks receiver.
type Config struct {
	// SubscriptionID is the Azure subscription that holds the AKS clusters. Required.
	SubscriptionID string `mapstructure:"subscription_id"`
	// ResourceGroup limits collection to one resource group. Optional.
	ResourceGroup string `mapstructure:"resource_group"`
	// ClusterName collects a single cluster. Requires ResourceGroup.
	ClusterName string `mapstructure:"cluster_name"`

	// Auth is the ID of the azure_auth extension that provides Azure tokens. Required.
	Auth *component.ID `mapstructure:"auth"`

	// CollectionInterval is how often the Azure Resource Manager API is polled.
	CollectionInterval time.Duration `mapstructure:"collection_interval"`
	// InitialDelay is the delay before the first collection.
	InitialDelay time.Duration `mapstructure:"initial_delay"`
	// Timeout bounds each collection, including paging.
	Timeout time.Duration `mapstructure:"timeout"`

	// Endpoint is the Azure Resource Manager endpoint. Change it for sovereign clouds.
	Endpoint string `mapstructure:"endpoint"`
	// APIVersion is the Microsoft.ContainerService API version.
	APIVersion string `mapstructure:"api_version"`
}

func createDefaultConfig() component.Config {
	return &Config{
		CollectionInterval: 5 * time.Minute,
		InitialDelay:       time.Second,
		Timeout:            30 * time.Second,
		Endpoint:           defaultEndpoint,
		APIVersion:         defaultAPIVersion,
	}
}

// Validate checks the receiver configuration.
func (c *Config) Validate() error {
	var errs []error
	if c.SubscriptionID == "" {
		errs = append(errs, errors.New("subscription_id is required"))
	}
	if c.Auth == nil {
		errs = append(errs, errors.New("auth is required: set it to the ID of an azure_auth extension"))
	}
	if c.ClusterName != "" && c.ResourceGroup == "" {
		errs = append(errs, errors.New("cluster_name requires resource_group"))
	}
	if c.CollectionInterval < time.Minute {
		errs = append(errs, errors.New("collection_interval must be at least 1m (Azure Resource Manager is rate limited)"))
	}
	if c.Timeout <= 0 {
		errs = append(errs, errors.New("timeout must be positive"))
	}
	if c.InitialDelay < 0 {
		errs = append(errs, errors.New("initial_delay must not be negative"))
	}
	if c.APIVersion == "" {
		errs = append(errs, errors.New("api_version is required"))
	}
	if u, err := url.Parse(c.Endpoint); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("endpoint %q must be an absolute URL", c.Endpoint))
	}
	return errors.Join(errs...)
}

// scope is the OAuth scope for the configured Azure Resource Manager endpoint.
func (c *Config) scope() string {
	return strings.TrimRight(c.Endpoint, "/") + "/.default"
}

// requestURL returns the first URL to call for the configured scope.
func (c *Config) requestURL() string {
	p := "/subscriptions/" + url.PathEscape(c.SubscriptionID)
	if c.ResourceGroup != "" {
		p += "/resourceGroups/" + url.PathEscape(c.ResourceGroup)
	}
	p += "/providers/Microsoft.ContainerService/managedClusters"
	if c.ClusterName != "" {
		p += "/" + url.PathEscape(c.ClusterName)
	}
	return strings.TrimRight(c.Endpoint, "/") + p + "?api-version=" + url.QueryEscape(c.APIVersion)
}
