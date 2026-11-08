package azureactivitylogreceiver

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
	defaultAPIVersion = "2015-04-01"
	// Azure keeps activity log events for 90 days.
	maxRetention = 90 * 24 * time.Hour
)

// Config defines the configuration of the azure_activity_log receiver.
type Config struct {
	// SubscriptionID is the subscription whose activity log is read. Required.
	SubscriptionID string `mapstructure:"subscription_id"`
	// Auth is the ID of the azure_auth extension that provides Azure tokens. Required.
	Auth *component.ID `mapstructure:"auth"`

	// At most one of the following narrows the events (an Azure API restriction).
	ResourceGroup    string `mapstructure:"resource_group"`
	ResourceID       string `mapstructure:"resource_id"`
	ResourceProvider string `mapstructure:"resource_provider"`

	// CollectionInterval is how often the activity log is polled.
	CollectionInterval time.Duration `mapstructure:"collection_interval"`
	// MaxIngestionDelay is how far back each poll re-reads, to catch events that
	// Azure makes available late. Events already emitted are skipped.
	MaxIngestionDelay time.Duration `mapstructure:"max_ingestion_delay"`
	// InitialLookback is how far back the first poll after startup reads.
	InitialLookback time.Duration `mapstructure:"initial_lookback"`
	// InitialDelay is the delay before the first poll.
	InitialDelay time.Duration `mapstructure:"initial_delay"`
	// Timeout bounds each poll, including paging.
	Timeout time.Duration `mapstructure:"timeout"`

	// Endpoint is the Azure Resource Manager endpoint. Change it for sovereign clouds.
	Endpoint string `mapstructure:"endpoint"`
	// APIVersion is the Microsoft.Insights activity log API version.
	APIVersion string `mapstructure:"api_version"`
}

func createDefaultConfig() component.Config {
	return &Config{
		CollectionInterval: time.Minute,
		MaxIngestionDelay:  15 * time.Minute,
		InitialLookback:    15 * time.Minute,
		InitialDelay:       time.Second,
		Timeout:            time.Minute,
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
	n := 0
	for _, v := range []string{c.ResourceGroup, c.ResourceID, c.ResourceProvider} {
		if v != "" {
			n++
		}
	}
	if n > 1 {
		errs = append(errs, errors.New("set at most one of resource_group, resource_id and resource_provider"))
	}
	if c.CollectionInterval < 30*time.Second {
		errs = append(errs, errors.New("collection_interval must be at least 30s (Azure Resource Manager is rate limited)"))
	}
	if c.MaxIngestionDelay < 0 || c.MaxIngestionDelay > 24*time.Hour {
		errs = append(errs, errors.New("max_ingestion_delay must be between 0 and 24h"))
	}
	if c.InitialLookback < 0 || c.InitialLookback > maxRetention {
		errs = append(errs, errors.New("initial_lookback must be between 0 and 90 days (2160h)"))
	}
	if c.InitialDelay < 0 {
		errs = append(errs, errors.New("initial_delay must not be negative"))
	}
	if c.Timeout <= 0 {
		errs = append(errs, errors.New("timeout must be positive"))
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

// odataQuote quotes a string literal for an OData $filter.
func odataQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// filter returns the OData $filter for events in [from, to].
func (c *Config) filter(from, to time.Time) string {
	f := fmt.Sprintf("eventTimestamp ge %s and eventTimestamp le %s",
		odataQuote(from.UTC().Format(time.RFC3339Nano)), odataQuote(to.UTC().Format(time.RFC3339Nano)))
	switch {
	case c.ResourceGroup != "":
		f += " and resourceGroupName eq " + odataQuote(c.ResourceGroup)
	case c.ResourceID != "":
		f += " and resourceUri eq " + odataQuote(c.ResourceID)
	case c.ResourceProvider != "":
		f += " and resourceProvider eq " + odataQuote(c.ResourceProvider)
	}
	return f
}

// requestURL returns the first URL to call for events in [from, to].
func (c *Config) requestURL(from, to time.Time) string {
	q := strings.ReplaceAll(url.QueryEscape(c.filter(from, to)), "+", "%20")
	return strings.TrimRight(c.Endpoint, "/") +
		"/subscriptions/" + url.PathEscape(c.SubscriptionID) +
		"/providers/Microsoft.Insights/eventtypes/management/values" +
		"?api-version=" + url.QueryEscape(c.APIVersion) + "&$filter=" + q
}
