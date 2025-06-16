package source

import (
	"context"
	"github.com/stretchr/testify/require"
	"os"
	"testing"

	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
)

func TestEnvConfigSource_Load(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		prefix      string
		delimiter   string
		envVars     map[string]string
		expected    map[string]any
		koanfDelim  string
		setupFunc   func(vars map[string]string)
		cleanupFunc func(vars map[string]string)
	}{
		{
			name:      "with prefix and delimiter",
			prefix:    "APP_",
			delimiter: "_",
			envVars: map[string]string{
				"APP_DB_HOST": "localhost",
				"APP_DB_PORT": "5432",
				"APP_FEATURE": "true",
			},
			expected: map[string]any{
				"db.host": "localhost",
				"db.port": "5432",
				"feature": "true",
			},
			koanfDelim: ".",
		},
		{
			name:      "without prefix",
			prefix:    "",
			delimiter: "_",
			envVars: map[string]string{
				"DB_HOST": "localhost",
				"DB_PORT": "5432",
			},
			expected: map[string]any{
				"db.host": "localhost",
				"db.port": "5432",
			},
			koanfDelim: ".",
		},
		{
			name:      "without delimiter",
			prefix:    "APP_",
			delimiter: "", // Koanf's default delimiter will be used by provider if this is empty
			envVars: map[string]string{
				"APP_DBHOST": "localhost", // Assuming koanfDelim is "." this won't be split
				"APP_DBPORT": "5432",
			},
			expected: map[string]any{
				"dbhost": "localhost",
				"dbport": "5432",
			},
			koanfDelim: ".",
		},
		{
			name:      "with different koanf delimiter",
			prefix:    "APP_",
			delimiter: "_", // Env delimiter
			envVars: map[string]string{
				"APP_SERVER_ADDRESS": "127.0.0.1",
				"APP_SERVER_ENABLED": "false",
			},
			expected: map[string]any{
				"server/address": "127.0.0.1",
				"server/enabled": "false",
			},
			koanfDelim: "/", // Koanf delimiter
		},
		{
			name:       "empty env vars",
			prefix:     "APP_",
			delimiter:  "_",
			envVars:    map[string]string{},
			expected:   map[string]any{},
			koanfDelim: ".",
		},
		{
			name:      "env vars not matching prefix",
			prefix:    "MYAPP_",
			delimiter: "_",
			envVars: map[string]string{
				"APP_DB_HOST": "localhost",
			},
			expected:   map[string]any{},
			koanfDelim: ".",
		},
	}

	setup := func(vars map[string]string) {
		for k, v := range vars {
			err := os.Setenv(k, v)
			require.NoError(t, err)
		}
	}
	cleanup := func(vars map[string]string) {
		for k := range vars {
			err := os.Unsetenv(k)
			require.NoError(t, err)
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup(tt.envVars)
			defer cleanup(tt.envVars)

			k := koanf.New(tt.koanfDelim)
			source := NewEnvConfigSource(tt.prefix, tt.delimiter)

			err := source.Load(ctx, k)
			assert.NoError(t, err)

			// Only check the keys we expect - ignore other env vars
			for key, val := range tt.expected {
				assert.Equal(t, val, k.Get(key), "Value for key '%s' should match", key)
			}
		})
	}
}

func TestEnvConfigSource_Watch(t *testing.T) {
	ctx := context.Background()
	k := koanf.New(".")
	source := NewEnvConfigSource("TEST_", "_")

	// Watch should be a no-op and not call the callback
	// It should also return nil error
	err := source.Watch(ctx, k, func(changedKeys []string, err error) {
		assert.Fail(t, "Watch callback should not be called for EnvConfigSource")
	})
	assert.NoError(t, err, "Watch should return nil and be a no-op")
}
