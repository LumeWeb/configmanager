package source

import (
	"context"
	"fmt"
	"strings"

	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/v2"
)

// EnvConfigSource loads configuration from environment variables.
type EnvConfigSource struct {
	prefix    string
	delimiter string
}

// NewEnvConfigSource creates a new EnvConfigSource with optional prefix and delimiter.
// The prefix is prepended to environment variable names (e.g. "APP_").
// The delimiter is used to split nested keys (e.g. "_" for "APP_DB_HOST").
func NewEnvConfigSource(prefix, delimiter string) *EnvConfigSource {
	return &EnvConfigSource{
		prefix:    prefix,
		delimiter: delimiter,
	}
}

// Load loads the configuration from environment variables into the Koanf instance.
func (e *EnvConfigSource) Load(ctx context.Context, k *koanf.Koanf) error {
	if k == nil {
		return fmt.Errorf("koanf instance cannot be nil")
	}

	// Create a callback that transforms env var names to config keys
	cb := func(s string) string {
		// Remove prefix if specified
		if e.prefix != "" {
			s = strings.TrimPrefix(s, e.prefix)
		}

		// Convert to lowercase and replace delimiter with koanf's delimiter
		s = strings.ToLower(s)
		if e.delimiter != "" {
			s = strings.ReplaceAll(s, e.delimiter, k.Delim())
		}
		return s
	}

	// Use the env provider with our callback
	provider := env.Provider(e.prefix, k.Delim(), cb)
	return k.Load(provider, nil)
}

// Watch watches for changes in the environment variables and triggers the onChange function when a change occurs.
// Environment variables cannot be watched in a cross-platform way, so this is a no-op.
func (e *EnvConfigSource) Watch(_ context.Context, _ *koanf.Koanf, _ WatchOnChangeCallback) error {
	// Environment variables cannot be watched in a cross-platform way
	return nil
}
