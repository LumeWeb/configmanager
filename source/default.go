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
	if err := d.loadStructDefaults(ctx, cm); err != nil {
		return err
	}

	// Then load static defaults
	return d.loadStaticDefaults(ctx, cm)
}

// loadStructDefaults processes all registered structs that implement ConfigDefaults
func (d *DefaultConfigSource) loadStructDefaults(ctx context.Context, cm configManager) error {
	for key, typ := range d.manager.GetRegisteredStructs() {
		if !ireflect.ImplementsConfigDefaults(typ) {
			continue
		}

		instance := reflect.New(typ).Interface()
		defaults, ok := instance.(ConfigDefaults)
		if !ok {
			continue
		}

		if err := d.processStructDefaults(ctx, cm, key, typ, defaults.Defaults()); err != nil {
			return err
		}
	}
	return nil
}

// processStructDefaults recursively processes struct fields and their defaults
func (d *DefaultConfigSource) processStructDefaults(ctx context.Context, cm configManager, prefix string, typ reflect.Type, defaults map[string]any) error {
	for defKey, defValue := range defaults {
		fieldName, fieldType, found := d.findMatchingField(typ, defKey)
		if !found {
			continue
		}

		fullKey := prefix + "." + fieldName

		// Handle nested structs recursively
		if fieldType.Kind() == reflect.Struct {
			nestedDefaults, ok := defValue.(map[string]any)
			if !ok {
				continue
			}
			if err := d.processStructDefaults(ctx, cm, fullKey, fieldType, nestedDefaults); err != nil {
				return err
			}
			continue
		}

		// Only set if key doesn't exist
		if !cm.Exists(fullKey) {
			if err := cm.Set(ctx, fullKey, defValue); err != nil {
				return fmt.Errorf("failed to set default value for key %s: %w", fullKey, err)
			}
		}
	}
	return nil
}

// findMatchingField finds a struct field matching the given name
func (d *DefaultConfigSource) findMatchingField(typ reflect.Type, fieldName string) (string, reflect.Type, bool) {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		// Skip unexported fields
		if field.PkgPath != "" {
			continue
		}

		// Check field name match (case sensitive)
		if field.Name == fieldName {
			// Use tag if present, otherwise use field name
			tagName := field.Tag.Get(d.tagName)
			if tagName != "" {
				return tagName, field.Type, true
			}
			return field.Name, field.Type, true
		}
	}
	return "", nil, false
}

// loadStaticDefaults loads the static default values
func (d *DefaultConfigSource) loadStaticDefaults(ctx context.Context, cm configManager) error {
	for key, value := range d.defaults {
		// Only set if key doesn't exist
		if !cm.Exists(key) {
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
