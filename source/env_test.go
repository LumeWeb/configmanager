package source

import (
	"context"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
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

func TestEnvConfigSource_ArrayParsing_AutoStrategy(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		envVars     map[string]string
		expectedKey string
		expectedVal any
		setupFunc   func()
		cleanupFunc func()
	}{
		{
			name: "JSON array in env var",
			envVars: map[string]string{
				"APP_HOSTS": `["node1","node2","node3"]`,
			},
			expectedKey: "hosts",
			expectedVal: []string{"node1", "node2", "node3"},
		},
		{
			name: "comma-delimited array",
			envVars: map[string]string{
				"APP_TAGS": "web,api,database",
			},
			expectedKey: "tags",
			expectedVal: []string{"web", "api", "database"},
		},
		{
			name: "empty JSON array",
			envVars: map[string]string{
				"APP_EMPTY": `[]`,
			},
			expectedKey: "empty",
			expectedVal: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			mgr := newMockManager(".")
			source := NewEnvConfigSource("APP_", "_", WithEnvSourceArrayStrategy(ArrayStrategyAuto, ""))

			err := source.Load(ctx, mgr)
			assert.NoError(t, err)

			val, _, err := mgr.Get(tt.expectedKey)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedVal, val)
		})
	}
}

func TestEnvConfigSource_ArrayParsing_IndexStrategy(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		envVars     map[string]string
		expectedKey string
		expectedVal []string
	}{
		{
			name: "index-based host list",
			envVars: map[string]string{
				"APP_HOSTS_0": "node1",
				"APP_HOSTS_1": "node2",
				"APP_HOSTS_2": "node3",
			},
			expectedKey: "hosts",
			expectedVal: []string{"node1", "node2", "node3"},
		},
		{
			name: "single index-based value",
			envVars: map[string]string{
				"APP_NAME_0": "myapp",
			},
			expectedKey: "name",
			expectedVal: []string{"myapp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			mgr := newMockManager(".")
			source := NewEnvConfigSource("APP_", "_", WithEnvSourceArrayStrategy(ArrayStrategyIndex, ""))

			err := source.Load(ctx, mgr)
			assert.NoError(t, err)

			val, _, err := mgr.Get(tt.expectedKey)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedVal, val)
		})
	}
}

func TestEnvConfigSource_ArrayParsing_DelimitedStrategy(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		envVars     map[string]string
		delimiter   string
		expectedKey string
		expectedVal []string
	}{
		{
			name: "comma-delimited",
			envVars: map[string]string{
				"APP_TAGS": "web,api,database",
			},
			delimiter:   ",",
			expectedKey: "tags",
			expectedVal: []string{"web", "api", "database"},
		},
		{
			name: "space-delimited",
			envVars: map[string]string{
				"APP_REGIONS": "us-east us-west eu-west",
			},
			delimiter:   " ",
			expectedKey: "regions",
			expectedVal: []string{"us-east", "us-west", "eu-west"},
		},
		{
			name: "pipe-delimited",
			envVars: map[string]string{
				"APP_PATHS": "/var|/tmp|/opt",
			},
			delimiter:   "|",
			expectedKey: "paths",
			expectedVal: []string{"/var", "/tmp", "/opt"},
		},
		{
			name: "semicolon-delimited",
			envVars: map[string]string{
				"APP_ORIGINS": "http://a.com;https://b.com",
			},
			delimiter:   ";",
			expectedKey: "origins",
			expectedVal: []string{"http://a.com", "https://b.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			mgr := newMockManager(".")
			source := NewEnvConfigSource("APP_", "_",
				WithEnvSourceArrayStrategy(ArrayStrategyDelimited, tt.delimiter))

			err := source.Load(ctx, mgr)
			assert.NoError(t, err)

			val, _, err := mgr.Get(tt.expectedKey)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedVal, val)
		})
	}
}

func TestEnvConfigSource_ArrayParsing_JSONStrategy(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		envVars     map[string]string
		expectedKey string
		expectedVal []string
	}{
		{
			name: "simple JSON array",
			envVars: map[string]string{
				"APP_HOSTS": `["host1","host2"]`,
			},
			expectedKey: "hosts",
			expectedVal: []string{"host1", "host2"},
		},
		{
			name: "JSON array with spaces",
			envVars: map[string]string{
				"APP_LIST": `["a", "b", "c"]`,
			},
			expectedKey: "list",
			expectedVal: []string{"a", "b", "c"},
		},
		{
			name: "empty JSON array",
			envVars: map[string]string{
				"APP_EMPTY": `[]`,
			},
			expectedKey: "empty",
			expectedVal: []string{},
		},
		{
			name: "non-JSON should not parse",
			envVars: map[string]string{
				"APP_VALUE": "just,a,string",
			},
			expectedKey: "value",
			expectedVal: nil, // Should remain as string
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			mgr := newMockManager(".")
			source := NewEnvConfigSource("APP_", "_",
				WithEnvSourceArrayStrategy(ArrayStrategyJSON, ""))

			err := source.Load(ctx, mgr)
			assert.NoError(t, err)

			if tt.expectedVal != nil {
				val, _, err := mgr.Get(tt.expectedKey)
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedVal, val)
			} else {
				// Should not be parsed as array
				val, _, err := mgr.Get(tt.expectedKey)
				assert.NoError(t, err)
				assert.IsType(t, "", val) // Should be string
			}
		})
	}
}

func TestEnvConfigSource_ArrayParsing_EdgeCases(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		envVars       map[string]string
		strategy      ArrayStrategy
		delimiter     string
		expectedKey   string
		expectedVal   any
		shouldConvert bool
	}{
		{
			name: "single value should not be array",
			envVars: map[string]string{
				"APP_HOST": "localhost",
			},
			strategy:      ArrayStrategyAuto,
			delimiter:     ",",
			expectedKey:   "host",
			expectedVal:   "localhost",
			shouldConvert: false,
		},
		{
			name: "empty string should not be array",
			envVars: map[string]string{
				"APP_HOST": "",
			},
			strategy:      ArrayStrategyAuto,
			delimiter:     ",",
			expectedKey:   "host",
			expectedVal:   "",
			shouldConvert: false,
		},
		{
			name: "gap in index-based parsing should fail silently",
			envVars: map[string]string{
				"APP_HOSTS_0": "node1",
				"APP_HOSTS_2": "node3", // Gap at index 1
			},
			strategy:      ArrayStrategyAuto,
			delimiter:     ",",
			expectedKey:   "", // Array shouldn't be created due to gap
			expectedVal:   nil,
			shouldConvert: false,
		},
		{
			name: "URL with colon should not parse as array",
			envVars: map[string]string{
				"APP_URL": "https://localhost:8080",
			},
			strategy:      ArrayStrategyAuto,
			delimiter:     ",",
			expectedKey:   "url",
			expectedVal:   "https://localhost:8080",
			shouldConvert: false,
		},
		{
			name: "IP address with colons should not parse",
			envVars: map[string]string{
				"APP_IP": "2001:db8::1",
			},
			strategy:      ArrayStrategyAuto,
			delimiter:     ",",
			expectedKey:   "ip",
			expectedVal:   "2001:db8::1",
			shouldConvert: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			mgr := newMockManager(".")
			source := NewEnvConfigSource("APP_", "_",
				WithEnvSourceArrayStrategy(tt.strategy, tt.delimiter))

			err := source.Load(ctx, mgr)
			assert.NoError(t, err)

			if tt.expectedKey == "" {
				// Expected no value (e.g., gap in indices)
				return
			}

			if tt.shouldConvert {
				if arr, ok := tt.expectedVal.([]string); ok {
					val, _, err := mgr.Get(tt.expectedKey)
					assert.NoError(t, err)
					assert.Equal(t, arr, val)
				}
			} else {
				val, _, err := mgr.Get(tt.expectedKey)
				assert.NoError(t, err)
				assert.IsType(t, "", val, "Should remain as string")
				assert.Equal(t, tt.expectedVal, val)
			}
		})
	}
}

func TestEnvConfigSource_WithEnvEnvironFunc(t *testing.T) {
	ctx := context.Background()

	// Custom environment for testing
	customEnv := []string{
		"APP_HOST=localhost",
		"APP_PORT=8080",
	}

	mgr := newMockManager(".")
	source := NewEnvConfigSource("APP_", "_",
		WithEnvEnvironFunc(func() []string {
			return customEnv
		}))

	err := source.Load(ctx, mgr)
	assert.NoError(t, err)

	host, _, err := mgr.Get("host")
	assert.NoError(t, err)
	assert.Equal(t, "localhost", host)
}

func TestEnvConfigSource_WithEnvTransformFunc(t *testing.T) {
	ctx := context.Background()

	os.Setenv("APP_HOST", "localhost")
	defer os.Unsetenv("APP_HOST")

	mgr := newMockManager(",")
	source := NewEnvConfigSource("APP_", "_",
		WithEnvTransformFunc(func(k, v string) (string, any) {
			// Custom transform that adds "custom_" prefix to all values
			k = strings.ToLower(strings.TrimPrefix(k, "APP_"))
			return k, "custom_" + v
		}))

	err := source.Load(ctx, mgr)
	assert.NoError(t, err)

	host, _, err := mgr.Get("host")
	assert.NoError(t, err)
	assert.Equal(t, "custom_localhost", host)
}

func TestEnvConfigSource_CombinedParsing(t *testing.T) {
	ctx := context.Background()

	// Test that regular env vars and index-based arrays work together
	envVars := map[string]string{
		"APP_NAME":       "myapp",
		"APP_PORT":       "8080",
		"APP_HOSTS_0":    "node1",
		"APP_HOSTS_1":    "node2",
		"APP_HOSTS_2":    "node3",
		"APP_FEATURES_0": "auth",
		"APP_FEATURES_1": "logging",
	}

	for k, v := range envVars {
		os.Setenv(k, v)
	}
	defer func() {
		for k := range envVars {
			os.Unsetenv(k)
		}
	}()

	mgr := newMockManager(".")
	source := NewEnvConfigSource("APP_", "_",
		WithEnvSourceArrayStrategy(ArrayStrategyAuto, ""))

	err := source.Load(ctx, mgr)
	assert.NoError(t, err)

	// Check regular values
	name, _, err := mgr.Get("name")
	assert.NoError(t, err)
	assert.Equal(t, "myapp", name)

	port, _, err := mgr.Get("port")
	assert.NoError(t, err)
	assert.Equal(t, "8080", port)

	// Check arrays
	hosts, _, err := mgr.Get("hosts")
	assert.NoError(t, err)
	assert.Equal(t, []string{"node1", "node2", "node3"}, hosts)

	features, _, err := mgr.Get("features")
	assert.NoError(t, err)
	assert.Equal(t, []string{"auth", "logging"}, features)
}

func TestEnvConfigSource_ArrayParsing_ComplexJson(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		input       string
		isParsed    bool
	}{
		{
			name:     "Malformed JSON should not parse as array",
			input:    `["unclosed}`,
			isParsed: false,
		},
		{
			name:     "JSON without quotes should not parse",
			input:    `[a,b,c]`,
			isParsed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("APP_TEST", tt.input)
			defer os.Unsetenv("APP_TEST")

			mgr := newMockManager(".")
			source := NewEnvConfigSource("APP_", "_",
				WithEnvSourceArrayStrategy(ArrayStrategyJSON, ""))

			err := source.Load(ctx, mgr)
			assert.NoError(t, err)

			val, _, err := mgr.Get("test")
			assert.NoError(t, err)

			if !tt.isParsed {
				// When JSON parsing fails, value remains as original string
				assert.IsType(t, "", val)
			} else {
				// Should have parsed as array
				assert.IsType(t, []string{}, val)
			}
		})
	}
}

