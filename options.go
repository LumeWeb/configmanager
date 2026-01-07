package configmanager

import (
	"go.lumeweb.com/configmanager/source"
	"go.uber.org/zap"
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

// WithFlags is a ConfigOption that sets flags for configuration keys
func WithFlags(flags map[string][]string) ConfigOption {
	return func(cm *ConfigManagerDefault) error {
		for key, flagList := range flags {
			cm.flagManager.SetFlags(key, flagList)
		}
		return nil
	}
}

// WithDescriptions is a ConfigOption that sets descriptions for configuration keys
func WithDescriptions(descriptions map[string]string) ConfigOption {
	return func(cm *ConfigManagerDefault) error {
		cm.descriptionManager.SetDescriptions(descriptions)
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
