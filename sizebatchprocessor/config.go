package sizebatchprocessor

import (
	"errors"
	"time"
)

// Config defines the configuration for the sizebatch processor.
type Config struct {
	// MaxSizeBytes is the maximum accumulated proto-serialized payload size in
	// bytes before a flush is triggered. A single batch that already exceeds this
	// threshold is passed through immediately after flushing any buffered data.
	// Default: 1 MiB (1048576).
	MaxSizeBytes int `mapstructure:"max_size_bytes"`

	// Timeout is the maximum duration to hold buffered telemetry before flushing,
	// even if MaxSizeBytes has not been reached. Default: 5s.
	Timeout time.Duration `mapstructure:"timeout"`
}

func (c *Config) Validate() error {
	if c.MaxSizeBytes <= 0 {
		return errors.New("max_size_bytes must be greater than 0")
	}
	if c.Timeout <= 0 {
		return errors.New("timeout must be greater than 0")
	}
	return nil
}
