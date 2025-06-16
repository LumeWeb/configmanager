package source

import (
	"context"
	"fmt"
	"github.com/knadh/koanf/maps"
	"github.com/knadh/koanf/v2"
	ireflect "go.lumeweb.com/configmanager/internal/reflect"
	"reflect"
)

type ConfigDefaults interface {
	Defaults() map[string]any
}

// DefaultConfigSource loads default configuration values.
type DefaultConfigSource struct {
	defaults map[string]any
	manager  manager
}

type manager interface {
	// Struct registration
	RegisterStruct(key string, cfg any) error
	GetRegisteredStructs() map[string]reflect.Type
}

// NewDefaultConfigSource creates a new DefaultConfigSource.
// The defaults map can contain nested values using dot notation keys (e.g. "database.host").
func NewDefaultConfigSource(manager manager, defaults map[string]any) *DefaultConfigSource {
	// Flatten nested maps into dot notation keys
	flatDefaults := make(map[string]any)
	for key, value := range defaults {
		if m, ok := value.(map[string]any); ok {
			for k, v := range flattenMap(m, key) {
				flatDefaults[k] = v
			}
		} else {
			flatDefaults[key] = value
		}
	}
	return &DefaultConfigSource{
		defaults: flatDefaults,
		manager:  manager,
	}
}

// flattenMap converts nested maps into dot notation keys using koanf's Flatten
func flattenMap(m map[string]any, prefix string) map[string]any {
	flattened, _ := maps.Flatten(m, nil, ".")
	if prefix == "" {
		return flattened
	}

	// Add prefix to all keys
	prefixed := make(map[string]any, len(flattened))
	for k, v := range flattened {
		prefixed[prefix+"."+k] = v
	}
	return prefixed
}

// Load loads the default configuration values into the Koanf instance.
func (d *DefaultConfigSource) Load(ctx context.Context, k *koanf.Koanf) error {
	// First load defaults from registered structs that implement ConfigDefaults
	for key, typ := range d.manager.GetRegisteredStructs() {
		if ireflect.ImplementsConfigDefaults(typ) {
			// Create new instance of the struct
			instance := reflect.New(typ).Interface()

			// Get defaults from the struct
			if defaults, ok := instance.(ConfigDefaults); ok {
				for defKey, defValue := range defaults.Defaults() {
					fullKey := key + k.Delim() + defKey
					if !k.Exists(fullKey) {
						if err := k.Set(fullKey, defValue); err != nil {
							return fmt.Errorf("failed to set default value for key %s: %w", fullKey, err)
						}
					}
				}
			}
		}
	}

	// Then load static defaults
	for key, value := range d.defaults {
		// Only set the value if it doesn't already exist
		if !k.Exists(key) {
			if err := k.Set(key, value); err != nil {
				return fmt.Errorf("failed to set default value for key %s: %w", key, err)
			}
		}
	}
	return nil
}

// Watch does not support watching default values, so it returns nil.
func (d *DefaultConfigSource) Watch(ctx context.Context, k *koanf.Koanf, onChange WatchOnChangeCallback) error {
	// Default values cannot be watched, so return nil.
	return nil
}
