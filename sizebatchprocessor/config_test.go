package sizebatchprocessor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid defaults",
			cfg:     Config{MaxSizeBytes: defaultMaxBytes, Timeout: defaultTimeout},
			wantErr: false,
		},
		{
			name:    "zero max_size_bytes",
			cfg:     Config{MaxSizeBytes: 0, Timeout: time.Second},
			wantErr: true,
		},
		{
			name:    "negative max_size_bytes",
			cfg:     Config{MaxSizeBytes: -1, Timeout: time.Second},
			wantErr: true,
		},
		{
			name:    "zero timeout",
			cfg:     Config{MaxSizeBytes: 1024, Timeout: 0},
			wantErr: true,
		},
		{
			name:    "negative timeout",
			cfg:     Config{MaxSizeBytes: 1024, Timeout: -time.Second},
			wantErr: true,
		},
		{
			name:    "valid custom",
			cfg:     Config{MaxSizeBytes: 512, Timeout: 100 * time.Millisecond},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCreateDefaultConfig(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	assert.Equal(t, defaultMaxBytes, cfg.MaxSizeBytes)
	assert.Equal(t, defaultTimeout, cfg.Timeout)
	assert.NoError(t, cfg.Validate())
}
