package source

import (
	"context"
	"github.com/stretchr/testify/require"
	"os"
	"testing"

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

			mgr := newMockManager(tt.koanfDelim)
			source := NewEnvConfigSource(tt.prefix, tt.delimiter)

			err := source.Load(ctx, mgr)
			assert.NoError(t, err)

			// Verify values were set in the mock manager
			for key, expectedVal := range tt.expected {
				val, _, err := mgr.Get(key)
				assert.NoError(t, err)
				assert.Equal(t, expectedVal, val, "Value for key '%s' should match", key)
			}
		})
	}
}

func TestEnvConfigSource_IsGlobal(t *testing.T) {
	t.Run("default is not global", func(t *testing.T) {
		source := NewEnvConfigSource("APP_", "_")
		assert.False(t, source.IsGlobal(), "default IsGlobal should be false")
	})

	t.Run("with WithEnvSourceGlobal option", func(t *testing.T) {
		source := NewEnvConfigSource("APP_", "_", WithEnvSourceGlobal())
		assert.True(t, source.IsGlobal(), "WithEnvSourceGlobal should set IsGlobal to true")
	})
}

func TestEnvConfigSource_Load_NilManager(t *testing.T) {
	ctx := context.Background()
	source := NewEnvConfigSource("APP_", "_")

	err := source.Load(ctx, nil)
	assert.Error(t, err, "Load should return error when config manager is nil")
	assert.Contains(t, err.Error(), "cannot be nil", "error message should mention nil config manager")
}

func TestEnvConfigSource_Load_EdgeCases(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		prefix     string
		delimiter  string
		envVars    map[string]string
		expected   map[string]any
		koanfDelim string
	}{
		{
			name:      "empty after prefix is ignored",
			prefix:    "APP_",
			delimiter: "_",
			envVars: map[string]string{
				"APP_": "value",
			},
			expected:   map[string]any{},
			koanfDelim: ".",
		},
		{
			name:      "mixed case env vars are converted to lowercase",
			prefix:    "APP_",
			delimiter: "_",
			envVars: map[string]string{
				"APP_DB_HOST":  "localhost",
				"APP_API_KEY":  "secret",
				"APP_UserName": "admin",
			},
			expected: map[string]any{
				"db.host":  "localhost",
				"api.key":  "secret",
				"username": "admin",
			},
			koanfDelim: ".",
		},
		{
			name:      "numeric values are preserved as strings",
			prefix:    "APP_",
			delimiter: "_",
			envVars: map[string]string{
				"APP_PORT":    "8080",
				"APP_TIMEOUT": "30",
				"APP_RATIO":   "0.75",
			},
			expected: map[string]any{
				"port":    "8080",
				"timeout": "30",
				"ratio":   "0.75",
			},
			koanfDelim: ".",
		},
		{
			name:      "deep nesting with multiple delimiters",
			prefix:    "APP_",
			delimiter: "_",
			envVars: map[string]string{
				"APP_DB_CLUSTER_SHARD_HOST": "node1",
				"APP_DB_CLUSTER_SHARD_PORT": "7000",
			},
			expected: map[string]any{
				"db.cluster.shard.host": "node1",
				"db.cluster.shard.port": "7000",
			},
			koanfDelim: ".",
		},
		{
			name:      "boolean values as strings",
			prefix:    "APP_",
			delimiter: "_",
			envVars: map[string]string{
				"APP_ENABLED":  "true",
				"APP_DISABLED": "false",
				"APP_DEBUG":    "1",
			},
			expected: map[string]any{
				"enabled":  "true",
				"disabled": "false",
				"debug":    "1",
			},
			koanfDelim: ".",
		},
		{
			name:      "special characters in values",
			prefix:    "APP_",
			delimiter: "_",
			envVars: map[string]string{
				"APP_URL":      "https://example.com:8443/path?query=1",
				"APP_PASSWORD": "p@ss!w0rd#",
				"APP_PATH":     "/var/log/app",
			},
			expected: map[string]any{
				"url":      "https://example.com:8443/path?query=1",
				"password": "p@ss!w0rd#",
				"path":     "/var/log/app",
			},
			koanfDelim: ".",
		},
		{
			name:      "no delimiter in env var names",
			prefix:    "APP_",
			delimiter: "_",
			envVars: map[string]string{
				"APP_CONFIGFILE": "/etc/config.yaml",
				"APP_LOGLEVEL":   "info",
			},
			expected: map[string]any{
				"configfile": "/etc/config.yaml",
				"loglevel":   "info",
			},
			koanfDelim: ".",
		},
		{
			name:      "empty delimiter keeps original key structure",
			prefix:    "APP_",
			delimiter: "",
			envVars: map[string]string{
				"APP_DBHOST": "localhost",
				"APP_DBPORT": "5432",
			},
			expected: map[string]any{
				"dbhost": "localhost",
				"dbport": "5432",
			},
			koanfDelim: ".",
		},
		{
			name:      "trailing delimiter in env var name",
			prefix:    "APP_",
			delimiter: "_",
			envVars: map[string]string{
				"APP_DB_HOST_": "value1",
				"APP_PORT_":    "8080",
			},
			expected: map[string]any{
				"db.host.": "value1",
				"port.":    "8080",
			},
			koanfDelim: ".",
		},
		{
			name:      "consecutive delimiters",
			prefix:    "APP_",
			delimiter: "_",
			envVars: map[string]string{
				"APP_DB__HOST": "localhost",
				"APP_PORT__80": "8080",
			},
			expected: map[string]any{
				"db..host": "localhost",
				"port..80": "8080",
			},
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

			mgr := newMockManager(tt.koanfDelim)
			source := NewEnvConfigSource(tt.prefix, tt.delimiter)

			err := source.Load(ctx, mgr)
			assert.NoError(t, err)

			// Verify values were set in the mock manager
			for key, expectedVal := range tt.expected {
				val, _, err := mgr.Get(key)
				assert.NoError(t, err)
				assert.Equal(t, expectedVal, val, "Value for key '%s' should match", key)
			}
		})
	}
}

func TestEnvConfigSource_Watch(t *testing.T) {
	ctx := context.Background()
	mgr := newMockManager()
	source := NewEnvConfigSource("TEST_", "_")

	// Watch should be a no-op and not call the callback
	// It should also return nil error
	err := source.Watch(ctx, mgr, func(changedKeys []string, err error) {
		assert.Fail(t, "Watch callback should not be called for EnvConfigSource")
	})
	assert.NoError(t, err, "Watch should return nil and be a no-op")
}
