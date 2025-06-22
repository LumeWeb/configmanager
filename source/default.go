package source

import (
	"context"
	"fmt"
	"github.com/knadh/koanf/maps"
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
	tagName  string
}

type manager interface {
	// Struct registration
	RegisterStruct(key string, cfg any) error
	GetRegisteredStructs() map[string]reflect.Type
}

// DefaultConfigOptions holds configuration options for DefaultConfigSource
type DefaultConfigOptions struct {
	defaults map[string]any
	tagName  string
}

// DefaultConfigOption defines the option function type
type DefaultConfigOption func(*DefaultConfigOptions)

// WithDefaults sets the default values map
func WithDefaults(defaults map[string]any) DefaultConfigOption {
	return func(o *DefaultConfigOptions) {
		o.defaults = defaults
	}
}

// WithTagName sets the struct tag name to use (default: "config")
func WithTagName(tagName string) DefaultConfigOption {
	return func(o *DefaultConfigOptions) {
		o.tagName = tagName
	}
}

// NewDefaultConfigSource creates a new DefaultConfigSource.
// The defaults map can contain nested values using dot notation keys (e.g. "database.host").
func NewDefaultConfigSource(manager manager, opts ...DefaultConfigOption) *DefaultConfigSource {
	// Set defaults
	options := DefaultConfigOptions{
		tagName: "config",
	}

	// Apply options
	for _, opt := range opts {
		opt(&options)
	}

	// Flatten nested maps into dot notation keys
	flatDefaults := make(map[string]any)
	for key, value := range options.defaults {
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
		tagName:  options.tagName,
	}
}

// flattenMap converts nested maps into keys using the manager's delimiter
func flattenMap(m map[string]any, prefix string) map[string]any {
	flattened, _ := maps.Flatten(m, nil, ".")
	if prefix == "" {
		return flattened
	}

	// Add prefix to all keys using the manager's delimiter
	prefixed := make(map[string]any, len(flattened))
	for k, v := range flattened {
		prefixed[prefix+"."+k] = v // Still use dot here since maps.Flatten uses dots internally
	}
	return prefixed
}

// Load loads the default configuration values into the config manager.
func (d *DefaultConfigSource) Load(ctx context.Context, cm configManager) error {
	// First load defaults from registered structs that implement ConfigDefaults
	for key, typ := range d.manager.GetRegisteredStructs() {
		if ireflect.ImplementsConfigDefaults(typ) {
			// Create new instance of the struct
			instance := reflect.New(typ).Interface()

			// Get defaults from the struct
			if defaults, ok := instance.(ConfigDefaults); ok {
				for defKey, defValue := range defaults.Defaults() {
					// Find field with matching name (case sensitive)
					var fieldName string
					found := false
					for i := 0; i < typ.NumField(); i++ {
						field := typ.Field(i)
						// Skip unexported fields
						if field.PkgPath != "" {
							continue
						}
						if field.Name == defKey {
							// Use tag if present, otherwise use field name
							if tag := field.Tag.Get(d.tagName); tag != "" {
								fieldName = tag
							} else {
								fieldName = field.Name
							}
							found = true
							break
						}
					}
					if !found {
						continue // Skip if no matching field found
					}
					fullKey := key + "." + fieldName // Use dot since we're working with flattened map keys
					// Only set if key doesn't exist
					if exists, _, _ := cm.Get(fullKey); exists == nil {
						if err := cm.Set(ctx, fullKey, defValue); err != nil {
							return fmt.Errorf("failed to set default value for key %s: %w", fullKey, err)
						}
					}
				}
			}
		}
	}

	// Then load static defaults
	for key, value := range d.defaults {
		// Only set if key doesn't exist
		if exists, _, _ := cm.Get(key); exists == nil {
			if err := cm.Set(ctx, key, value); err != nil {
				return fmt.Errorf("failed to set default value for key %s: %w", key, err)
			}
		}
	}
	return nil
}

// Watch does not support watching default values, so it returns nil.
func (d *DefaultConfigSource) Watch(ctx context.Context, cm configManager, onChange WatchOnChangeCallback) error {
	// Default values cannot be watched, so return nil.
	return nil
}
