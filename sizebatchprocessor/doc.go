// Package sizebatchprocessor buffers telemetry until the accumulated payload
// reaches a configurable byte threshold (or a timeout elapses), then flushes
// a single merged batch to the next consumer.
package sizebatchprocessor // import "github.com/ssijbabu/sizebatchprocessor"
