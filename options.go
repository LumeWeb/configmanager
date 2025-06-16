package configmanager

import (
	"fmt"
	"go.lumeweb.com/configmanager/source"
	"go.uber.org/zap"
	"reflect"
)

type ConfigOption func(*ConfigManagerDefault) error

// WithTagName sets the struct tag name to use for configuration mapping
func WithTagName(tagName string) ConfigOption {
	return func(cm *ConfigManagerDefault) error {
		cm.tagName = tagName
		return nil
	}
}

// WithConfigStruct registers a configuration struct type for a key
func WithConfigStruct(key string, cfg any) ConfigOption {
	return func(cm *ConfigManagerDefault) error {
		return cm.RegisterStruct(key, cfg)
	}
}

// RegisterStruct registers a configuration struct type for a key at runtime.
// Returns an error if the key is already registered to a different type.
func (cm *ConfigManagerDefault) RegisterStruct(key string, cfg any) error {
	cm.configStructLock.Lock()
	defer cm.configStructLock.Unlock()

	typ := reflect.TypeOf(cfg)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	// Check if already registered with same type
	if existing, ok := cm.configStructs[key]; ok {
		if existing != typ {
			return fmt.Errorf("config struct for key '%s' already registered with different type (%v vs %v)",
				key, existing, typ)
		}
		return nil // same type, no error
	}

	cm.configStructs[key] = typ
	return nil
}

// WithFlags is a ConfigOption that sets flags for configuration keys
func WithFlags(flags map[string][]string) ConfigOption {
	return func(cm *ConfigManagerDefault) error {
		for key, flagList := range flags {
			cm.flagManager.SetFlags(key, flagList)
		}
		return nil
	}
}

// WithLogger configures the logger for the ConfigManagerDefault
func WithLogger(logger *zap.Logger) ConfigOption {
	return func(cm *ConfigManagerDefault) error {
		cm.logger = logger
		return nil
	}
}

// WithDefaultConfigFile sets the default config file path
func WithDefaultConfigFile(path string) ConfigOption {
	return func(cm *ConfigManagerDefault) error {
		cm.configFile = path
		return nil
	}
}

// WithSyncConfigNamespace sets the namespace for sync client configuration
func WithSyncConfigNamespace(namespace string) ConfigOption {
	return func(cm *ConfigManagerDefault) error {
		cm.syncConfigNS = namespace
		return nil
	}
}

// WithSources sets the configuration sources for the ConfigManagerDefault
func WithSources(sources ...source.ConfigSource) ConfigOption {
	return func(cm *ConfigManagerDefault) error {
		cm.sources = sources
		return nil
	}
}

// UsingSources is a helper that returns the sources as-is, useful for inline usage
func UsingSources(sources ...source.ConfigSource) []source.ConfigSource {
	return sources
}
