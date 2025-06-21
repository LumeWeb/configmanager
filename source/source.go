package source

import (
	"context"
	"fmt"
)

// ConfigSource abstracts the loading of configuration data from different sources.
type ConfigSource interface {
	// Load loads configuration data from a specific source into the config manager.
	Load(ctx context.Context, cm configManager) error
	// Watch watches for changes in the configuration source and triggers the onChange function with the keys that changed.
	// If the source does not support watching, this method should return nil.
	Watch(ctx context.Context, cm configManager, onChange WatchOnChangeCallback) error
}

// WatchAllChanges is a special value that indicates all configuration values may have changed.
// This should be used when the source detects a broad change but can't determine exactly which keys changed.
const WatchAllChanges = "*"

// AllChanges is a predefined slice containing the WatchAllChanges constant for convenience.
var AllChanges = []string{WatchAllChanges}

// WatchOnChangeCallback defines the signature for the callback function used in Watch.
// The callback receives a slice of changed keys:
// - An empty slice []string{} means no changes were detected
// - A slice containing WatchAllChanges (["*"]) means all configuration may have changed
// - A slice of specific keys means those keys changed
type WatchOnChangeCallback func(changedKeys []string, err error)

// StoppableConfigSource is a ConfigSource that can be stopped.
type StoppableConfigSource interface {
	ConfigSource
	// Stop stops any background processes or watchers associated with the source.
	Stop() error
}

// FindSourceByType iterates through a slice of ConfigSource
// and returns the first source that matches the specified type T.
// If no source of the specified type is found, it returns the zero value for T and an error.
func FindSourceByType[T ConfigSource](sources []ConfigSource) (T, error) {
	var zero T // Used to return the zero value of T and for type name in error
	for _, s := range sources {
		if typedSource, ok := s.(T); ok {
			return typedSource, nil
		}
	}
	return zero, fmt.Errorf("no source of type %T found", zero)
}

// PersistableConfigSource is a ConfigSource that can persist configuration changes.
type PersistableConfigSource interface {
	ConfigSource
	// Persist writes configuration changes back to the source.
	Persist(cm configManager, keyPrefix ...string) error
}
type configManager interface {
	Get(string, ...any) (any, any, error)
	Exists(key string) bool
	Set(ctx context.Context, key string, value any) error
	BulkSet(ctx context.Context, updates map[string]any) error
	SetAtomic(ctx context.Context, updates map[string]any) error 
	BulkSetAtomic(ctx context.Context, updates map[string]any) error
	Delete(key string)
	Keys() []string
	Delim() string
	All() map[string]any
}
