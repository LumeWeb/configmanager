package source

import (
	"context"
	"fmt"
	"strings"

	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/v2"
)

// EnvConfigSource loads configuration from environment variables.
type EnvConfigSource struct {
	prefix    string
	delimiter string
	global    bool // Controls whether this source should be loaded globally
}

// IsGlobal implements GlobalConfigSource
func (e *EnvConfigSource) IsGlobal() bool {
	return e.global
}

// NewEnvConfigSource creates a new EnvConfigSource with optional prefix and delimiter.
// The prefix is prepended to environment variable names (e.g. "APP_").
// The delimiter is used to split nested keys (e.g. "_" for "APP_DB_HOST").
type EnvConfigOption func(*EnvConfigSource)

func WithEnvSourceGlobal() EnvConfigOption {
	return func(e *EnvConfigSource) {
		e.global = true
	}
}

func NewEnvConfigSource(prefix, delimiter string, opts ...EnvConfigOption) *EnvConfigSource {
	e := &EnvConfigSource{
		prefix:    prefix,
		delimiter: delimiter,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Load loads the configuration from environment variables into the config manager.
func (e *EnvConfigSource) Load(ctx context.Context, cm configManager) error {
	if cm == nil {
		return fmt.Errorf("config manager cannot be nil")
	}

	// Create temporary koanf to load env vars
	tmpKoanf := koanf.New(cm.Delim())

	// Create a callback that transforms env var names to config keys
	cb := func(k, v string) (string, any) {
		// Remove prefix if specified
		if e.prefix != "" {
			k = strings.TrimPrefix(k, e.prefix)
		}

		// Convert to lowercase and replace delimiter with koanf's delimiter
		k = strings.ToLower(k)
		if e.delimiter != "" {
			k = strings.ReplaceAll(k, e.delimiter, tmpKoanf.Delim())
		}
		return k, v
	}

	// Use the env provider with our callback
	provider := env.Provider(tmpKoanf.Delim(), env.Opt{
		Prefix:        e.prefix,
		TransformFunc: cb,
	})
	if err := tmpKoanf.Load(provider, nil); err != nil {
		return err
	}

	// Set values through config manager to trigger validation
	for key, value := range tmpKoanf.All() {
		if err := cm.Set(ctx, key, value); err != nil {
			return err
		}
	}

	return nil
}

// Watch watches for changes in the environment variables and triggers the onChange function when a change occurs.
// Environment variables cannot be watched in a cross-platform way, so this is a no-op.
func (e *EnvConfigSource) Watch(_ context.Context, _ configManager, _ WatchOnChangeCallback) error {
	// Environment variables cannot be watched in a cross-platform way
	return nil
}
