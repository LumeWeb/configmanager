package configmanager

import (
	"context"
	"fmt"
	"github.com/Oudwins/zog"
	_ "github.com/Oudwins/zog"
	"github.com/go-viper/mapstructure/v2"
	kkoanf "github.com/knadh/koanf/v2"
	"github.com/samber/lo"
	"github.com/spf13/cast"
	ireflect "go.lumeweb.com/configmanager/internal/reflect"
	"go.lumeweb.com/configmanager/source"
	csync "go.lumeweb.com/configmanager/sync"
	"go.lumeweb.com/event/v2"
	"go.uber.org/zap"
	"reflect"
	"strings"
	"sync"
	"time"
)

const (
	schemaValidationRootPath = "$root"
	keySeparator             = "."
)

// copy creates a throwaway copy of the ConfigManagerDefault with a new Koanf instance.
func (cm *ConfigManagerDefault) copy() *ConfigManagerDefault {
	cm.validationLock.RLock()
	defer cm.validationLock.RUnlock()
	cm.configStructLock.RLock()
	defer cm.configStructLock.RUnlock()

	return &ConfigManagerDefault{
		koanf:             kkoanf.New(cm.Delim()),
		sources:           cm.sources,
		logger:            cm.logger,
		events:            cm.events,
		flagManager:       cm.flagManager,
		configStructs:     cm.configStructs,
		configFile:        cm.configFile,
		configDir:         cm.configDir,
		tagName:           cm.tagName,
		registry:          cm.registry,
		syncConfigNS:      cm.syncConfigNS,
		validationEnabled: cm.validationEnabled,
		delimiter:         cm.delimiter,
	}
}

// ConfigManagerDefault is the central point of interaction for accessing and managing configuration.
type ConfigManagerDefault struct {
	syncMgr           csync.Manager
	koanf             *kkoanf.Koanf
	sources           []source.ConfigSource
	logger            *zap.Logger
	validationEnabled bool
	validationLock    sync.RWMutex
	events            event.EventManager[csync.ConfigEvent]
	flagManager       FlagManager
	configStructs     map[string]reflect.Type
	configStructLock  sync.RWMutex
	configFile        string
	configDir         string
	tagName           string
	registry          ConfigRegistry
	syncConfigNS      string // Namespace for sync client configuration
	delimiter         string // Custom delimiter for nested keys
}

// Ensure ConfigManagerDefault implements Manager interface
var _ Manager = (*ConfigManagerDefault)(nil)

// Subscribe registers a callback to be notified when configuration changes matching the key pattern occur.
// The pattern can contain wildcards:
// - "*" matches any single path segment
// - "**" matches any remaining path segments
// Returns an unsubscribe function.
func (cm *ConfigManagerDefault) Subscribe(pattern string, callback SubscriptionCallback) func() {
	cm.logger.Debug("Adding subscription",
		zap.String("pattern", pattern),
		zap.String("callback", fmt.Sprintf("%p", callback)))

	// Create a simple event listener that calls the callback
	listener := event.NewListenerFunc(func(e event.Event[csync.ConfigEvent]) error {
		cm.logger.Debug("Subscription callback triggered",
			zap.String("pattern", pattern),
			zap.String("event_key", e.Name()))

		configEvent := e.Data()
		callback(pattern,
			configEvent.Get("key").(string),
			configEvent.Get("value"))
		return nil
	})

	// Add the listener with the pattern as the event name
	cm.events.AddListener(pattern, listener)

	// Return an unsubscribe function that removes the listener
	return func() {
		cm.logger.Debug("Removing subscription",
			zap.String("pattern", pattern))
		cm.events.RemoveListener(pattern, listener)
	}
}

// reloadConfig reloads configuration from a source
func (cm *ConfigManagerDefault) reloadConfig(ctx context.Context, source source.ConfigSource) error {
	if err := source.Load(ctx, cm); err != nil {
		return fmt.Errorf("failed to reload config: %w", err)
	}
	return nil
}

func (cm *ConfigManagerDefault) handleConfigChanges(src source.ConfigSource, changedKeys []string) {
	if len(changedKeys) == 0 {
		return
	}

	namespace, _ := cm.registry.GetNamespace(src)

	if len(changedKeys) == 1 && changedKeys[0] == source.WatchAllChanges {
		// Reload entire source
		if err := cm.loadSource(src); err != nil {
			cm.logger.Error("failed to reload configuration from source",
				zap.String("source", fmt.Sprintf("%T", src)),
				zap.Error(err))
		}
		return
	}

	// Apply namespace to changed keys
	var prefixedKeys []string
	if namespace != "" {
		prefixedKeys = make([]string, len(changedKeys))
		for i, key := range changedKeys {
			prefixedKeys[i] = namespace + cm.Delim() + key
		}
	} else {
		prefixedKeys = changedKeys
	}

	// Track old values and prepare updates
	updates := make(map[string]any)
	oldValues := make(map[string]any)

	for _, key := range prefixedKeys {
		// Get current value before reloading
		if cm.Exists(key) {
			oldValues[key] = cm.koanf.Get(key)
		}

		// Reload just this key from source
		val, err := cm.getKeyFromSource(context.Background(), src, key)
		if err != nil {
			cm.logger.Error("failed to get new value for key",
				zap.String("config_key", key),
				zap.String("source", fmt.Sprintf("%T", src)),
				zap.Error(err))
			continue
		}
		updates[key] = val
	}

	if len(updates) > 0 {
		if err := cm.BulkSetAtomic(context.Background(), updates); err != nil {
			cm.logger.Error("failed to apply config updates",
				zap.String("source", fmt.Sprintf("%T", src)),
				zap.Error(err))
			return
		}

		// Notify subscribers for each changed key
		for key, newValue := range updates {
			cm.notifySubscribers(key, oldValues[key], newValue)
		}
	}
}

func (cm *ConfigManagerDefault) reloadKey(ctx context.Context, source source.ConfigSource, key string) error {
	// Get the new value for the key from the source
	newValue, err := cm.getKeyFromSource(ctx, source, key)
	if err != nil {
		return fmt.Errorf("failed to get new value for key %s from source: %w", key, err)
	}

	// Get the old value before updating
	var oldValue any
	if cm.Exists(key) {
		oldValue, _, _ = cm.Get(key)
	}

	// Update the value in Koanf
	if err := cm.Set(ctx, key, newValue); err != nil {
		return fmt.Errorf("failed to set new value for key %s in koanf: %w", key, err)
	}

	// Notify with both old and new values
	cm.notifySubscribers(key, oldValue, newValue)

	return nil
}

func (cm *ConfigManagerDefault) getKeyFromSource(ctx context.Context, source source.ConfigSource, key string) (any, error) {

	// Load only the specific key from the source
	if err := source.Load(ctx, cm); err != nil {
		return nil, fmt.Errorf("failed to load config from source: %w", err)
	}

	// Get the value of the key
	if !cm.Exists(key) {
		return nil, fmt.Errorf("key %s not found in source", key)
	}
	return cm.koanf.Get(key), nil
}

func (cm *ConfigManagerDefault) notifySubscribers(key string, oldValue any, newValue any) {
	cm.logger.Debug("Config changed",
		zap.String("key", key),
		zap.Any("new_value", newValue),
		zap.Any("old_value", oldValue))

	// Fire one event for the full key path
	// The event manager will handle pattern matching via matchNodePath
	evt := csync.NewConfigEvent(key, newValue, oldValue, key)
	if err := cm.events.FireEvent(evt); err != nil {
		cm.logger.Error("Failed to notify configuration change",
			zap.String("config_key", key),
			zap.Error(err))
	}
}

// WithDelimiter sets the delimiter used for nested keys in the configuration manager.
func WithDelimiter(delimiter string) ConfigOption {
	return func(cm *ConfigManagerDefault) error {
		cm.delimiter = delimiter
		cm.koanf = kkoanf.New(delimiter)
		return nil
	}
}

// EnableValidation enables validation for all configuration changes
func (cm *ConfigManagerDefault) EnableValidation() {
	cm.validationLock.Lock()
	defer cm.validationLock.Unlock()
	cm.validationEnabled = true
}

// DisableValidation disables validation for configuration changes
func (cm *ConfigManagerDefault) DisableValidation() {
	cm.validationLock.Lock()
	defer cm.validationLock.Unlock()
	cm.validationEnabled = false
}

// ValidationEnabled returns whether validation is currently enabled
func (cm *ConfigManagerDefault) ValidationEnabled() bool {
	cm.validationLock.RLock()
	defer cm.validationLock.RUnlock()
	return cm.validationEnabled
}

// NewConfigManager creates a new ConfigManagerDefault.
// Sources can be:
// - string paths (automatically wrapped as file sources)
// - source.ConfigSource implementations
// - []source.ConfigSource slice
func NewConfigManager(sources any, opts ...ConfigOption) (*ConfigManagerDefault, error) {
	// Create with default delimiter first
	k := kkoanf.New(".")
	eventMgr := event.NewManager[csync.ConfigEvent]("", event.UsePathMode)

	// Convert sources to ConfigSource interfaces
	var configSources []source.ConfigSource

	switch v := sources.(type) {
	case []any:
		for _, s := range v {
			switch sv := s.(type) {
			case string:
				// Treat strings as file paths
				configSources = append(configSources, source.NewFileSource(sv))
			case source.ConfigSource:
				configSources = append(configSources, sv)
			default:
				return nil, fmt.Errorf("invalid source type: %T", s)
			}
		}
	case []source.ConfigSource:
		configSources = v
	case string:
		configSources = append(configSources, source.NewFileSource(v))
	case source.ConfigSource:
		configSources = append(configSources, v)
	default:
		return nil, fmt.Errorf("invalid sources type: %T, expected []any, []source.ConfigSource, string or source.ConfigSource", sources)
	}

	cm := &ConfigManagerDefault{
		koanf:             k,
		sources:           configSources,
		logger:            zap.NewNop(), // Default no-op logger
		events:            eventMgr,
		flagManager:       NewFlagManager(),
		configStructs:     make(map[string]reflect.Type),
		tagName:           "config", // Default tag name
		registry:          NewDefaultConfigRegistry(),
		syncConfigNS:      "sync.config", // Default sync config namespace
		validationEnabled: true,          // Validation enabled by default
	}

	for _, opt := range opts {
		if err := opt(cm); err != nil {
			return nil, fmt.Errorf("failed to apply config option: %w", err)
		}
	}

	// Set default config file and dir if not provided
	if cm.configFile == "" {
		cm.configFile = "config.yaml"
	}
	if cm.configDir == "" {
		cm.configDir = "."
	}

	return cm, nil
}

// SetupSync configures the sync manager with options and starts it
func (cm *ConfigManagerDefault) SetupSync(opts ...ConfigOption) error {
	for _, opt := range opts {
		if err := opt(cm); err != nil {
			return fmt.Errorf("failed to apply config option: %w", err)
		}
	}

	if cm.syncMgr == nil {
		return fmt.Errorf("sync manager is nil")
	}

	// Configure sync manager
	if err := cm.syncMgr.Configure(cm, cm.syncConfigNS); err != nil {
		return fmt.Errorf("failed to configure sync manager: %w", err)
	}

	// Start sync manager
	if err := cm.syncMgr.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start sync manager: %w", err)
	}

	return nil
}

func (cm *ConfigManagerDefault) loadSource(src source.ConfigSource) error {
	var namespace string
	if ns, ok := cm.registry.GetNamespace(src); ok {
		namespace = ns
	}

	// Create throwaway copy
	throwawayCM := cm.copy()

	// Load into throwaway copy
	if err := src.Load(context.Background(), throwawayCM); err != nil {
		return fmt.Errorf("failed to load from source: %w", err)
	}

	// Prepare updates with namespace
	updates := make(map[string]any)
	sourceData := throwawayCM.All()

	if namespace != "" {
		for key, value := range sourceData {
			fullKey := namespace + cm.Delim() + key
			updates[fullKey] = value
		}
	} else {
		updates = sourceData
	}

	// Apply updates atomically
	if err := cm.BulkSetAtomic(context.Background(), updates); err != nil {
		return fmt.Errorf("failed to apply updates: %w", err)
	}

	return nil
}

// Load loads the initial configuration from the configured sources.
func (cm *ConfigManagerDefault) Load() error {
	// Disable validation during initial load to avoid validation errors
	// from partially loaded configurations
	cm.DisableValidation()
	defer cm.EnableValidation()

	for _, src := range cm.sources {
		if err := cm.loadSource(src); err != nil {
			return fmt.Errorf("failed to load config from source %T: %w", src, err)
		}

		// Start watching if supported
		if err := src.Watch(context.Background(), cm, func(changedKeys []string, err error) {
			cm.handleConfigChanges(src, changedKeys)
		}); err != nil {
			cm.logger.Warn("failed to start config watcher",
				zap.String("source", fmt.Sprintf("%T", src)),
				zap.Error(err))
		}
	}

	// Now that all sources are loaded, validate all registered structs
	if err := cm.ValidateRegisteredStructs(); err != nil {
		return fmt.Errorf("configuration validation failed after load: %w", err)
	}

	return nil
}

// GetString returns the string value for the given key.
func (cm *ConfigManagerDefault) GetString(key string) (string, error) {
	val, _, err := cm.Get(key)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", val), nil
}

// GetInt returns the int64 value for the given key.
func (cm *ConfigManagerDefault) GetInt(key string) (int64, error) {
	val, _, err := cm.Get(key)
	if err != nil {
		return 0, err
	}
	return cast.ToInt64E(val)
}

// GetBool returns the bool value for the given key.
func (cm *ConfigManagerDefault) GetBool(key string) (bool, error) {
	val, _, err := cm.Get(key)
	if err != nil {
		return false, err
	}
	return cast.ToBoolE(val)
}

// GetDuration returns the time.Duration value for the given key.
func (cm *ConfigManagerDefault) GetDuration(key string) (time.Duration, error) {
	val, _, err := cm.Get(key)
	if err != nil {
		return 0, err
	}
	return cast.ToDurationE(val)
}

// GetStringSlice returns the []string value for the given key.
func (cm *ConfigManagerDefault) GetStringSlice(key string) ([]string, error) {
	val, _, err := cm.Get(key)
	if err != nil {
		return nil, err
	}
	return cast.ToStringSliceE(val)
}

// IsSet checks if a configuration key exists and has a non-zero value.
func (cm *ConfigManagerDefault) IsSet(ctx context.Context, key string) bool {
	if !cm.Exists(key) {
		return false
	}
	val, _, _ := cm.Get(key)
	return !reflect.ValueOf(val).IsZero()
}

// Get returns the configuration value for the given key.
// Returns:
// - raw any: direct config value from koanf
// - decoded any: populated struct if target provided, new struct instance if registered, otherwise same as raw
// - error: if any occurred
func (cm *ConfigManagerDefault) Get(key string, target ...any) (any, any, error) {
	// Find the most specific namespace for this key
	nsSource, namespacedKey := cm.registry.FindMostSpecificNamespace(key, cm.Delim())

	var fullKey string
	if nsSource.Namespace != "" {
		if namespacedKey == "" {
			fullKey = nsSource.Namespace
		} else {
			fullKey = nsSource.Namespace + cm.Delim() + namespacedKey
		}
	} else {
		fullKey = key
	}

	// Get raw value first
	if !cm.koanf.Exists(fullKey) {
		return nil, nil, fmt.Errorf("configuration key '%s' not found", fullKey)
	}
	raw := cm.koanf.Get(fullKey)

	// Handle struct decoding cases
	switch {
	case len(target) > 0:
		// Decode into provided target
		decoded, err := cm.getIntoStruct(fullKey, target[0])
		return raw, decoded, err
	case cm.hasConfigStruct(fullKey):
		// Decode into new struct instance
		decoded, err := cm.getIntoStruct(fullKey, nil)
		return raw, decoded, err
	default:
		// No struct decoding needed
		return raw, raw, nil
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

// getIntoStruct decodes configuration into a registered struct type (used by RegisterStruct)
func (cm *ConfigManagerDefault) getIntoStruct(key string, target any) (any, error) {
	if !cm.hasConfigStruct(key) {
		return nil, fmt.Errorf("no struct registered for key '%s'", key)
	}

	cm.configStructLock.RLock()
	structType := cm.configStructs[key]
	cm.configStructLock.RUnlock()

	// Create new instance if target is nil
	var cfg any
	if target == nil {
		// Always create pointer to registered value type
		cfg = reflect.New(structType).Interface()
	} else {
		targetType := reflect.TypeOf(target)
		// Dereference pointer target to match registered value type
		if targetType.Kind() == reflect.Ptr {
			targetType = targetType.Elem()
			if targetType != structType {
				return nil, fmt.Errorf("target type %v does not match registered type %v",
					targetType, structType)
			}
			cfg = target
		} else {
			// For value target, must match registered type exactly
			if targetType != structType {
				return nil, fmt.Errorf("target type %v does not match registered type %v",
					targetType, structType)
			}
			// Return error for value targets since we can't set into them
			return nil, fmt.Errorf("target must be a pointer to decode into, got %T", target)
		}
	}

	hooks := []mapstructure.DecodeHookFunc{
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		mapstructure.RecursiveStructToMapHookFunc(),
		mapstructure.TextUnmarshallerHookFunc(),
		mapstructure.StringToBasicTypeHookFunc(),
		// Initialize nil pointer structs before decoding
		func(f reflect.Type, t reflect.Type, data any) (any, error) {
			if t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct {
				if reflect.ValueOf(data).IsNil() {
					return reflect.New(t.Elem()).Interface(), nil
				}
			}
			return data, nil
		},
		// Convert int to duration in seconds
		func(f reflect.Kind, t reflect.Kind, data any) (any, error) {
			if f == reflect.Int && t == reflect.Int64 {
				// Check if target type is time.Duration (either direct or struct field)
				if structType.String() == "time.Duration" {
					// Convert integer seconds to nanoseconds
					return time.Duration(data.(int)) * time.Second, nil
				}
			}
			// Also handle direct int to duration conversion
			if f == reflect.Int && t == reflect.TypeOf(time.Duration(0)).Kind() {
				return time.Duration(data.(int)) * time.Second, nil
			}
			return data, nil
		},
		// Convert bool to "true"/"false" strings
		func(f reflect.Kind, t reflect.Kind, data any) (any, error) {
			if f == reflect.Bool && t == reflect.String {
				if data.(bool) {
					return "true", nil
				}
				return "false", nil
			}
			return data, nil
		},
	}

	// Get all config data under the key prefix
	data := cm.koanf.Cut(key).Raw()
	if len(data) == 0 {
		return nil, fmt.Errorf("no data found for key %s", key)
	}

	// If we already have the correct type, return it directly
	if reflect.TypeOf(data) == reflect.PointerTo(structType) {
		return data, nil
	}

	// Otherwise unmarshal into the target struct
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook:       mapstructure.ComposeDecodeHookFunc(hooks...),
		TagName:          cm.tagName,
		WeaklyTypedInput: true,
		Result:           cfg,
		Squash:           true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create decoder: %w", err)
	}

	if err := decoder.Decode(data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config into struct: %w", err)
	}

	// Ensure we return a pointer to the struct
	if reflect.TypeOf(cfg).Kind() != reflect.Ptr {
		ptr := reflect.New(reflect.TypeOf(cfg))
		ptr.Elem().Set(reflect.ValueOf(cfg))
		return ptr.Interface(), nil
	}

	return cfg, nil
}

// All retrieves all configuration settings.
func (cm *ConfigManagerDefault) All() map[string]any {
	return cm.koanf.All()
}

// Exists checks if a configuration key exists.
func (cm *ConfigManagerDefault) Exists(key string) bool {
	return cm.koanf.Exists(key)
}

// SetAtomic sets multiple configuration values atomically.
func (cm *ConfigManagerDefault) SetAtomic(ctx context.Context, updates map[string]any) error {
	return cm.bulkSetAtomicInternal(ctx, updates, true)
}

// BulkSetAtomic sets multiple configuration values atomically. All updates are applied first,
// then validation is performed on all affected structs. If validation fails, all changes
// are rolled back. This is different from SetAtomic which validates each change individually.
//
// The method ensures either all updates succeed or none are applied (atomicity).
// Returns an error if validation fails or if any update fails to apply.
func (cm *ConfigManagerDefault) BulkSetAtomic(ctx context.Context, updates map[string]any) error {
	return cm.bulkSetAtomicInternal(ctx, updates, true)
}

// bulkSetAtomicInternal is the internal implementation for atomic bulk updates
func (cm *ConfigManagerDefault) bulkSetAtomicInternal(ctx context.Context, updates map[string]any, validate bool) error {
	// Track old values and affected structs
	oldValues := make(map[string]any)
	for key := range updates {
		raw, _, err := cm.Get(key)
		if err != nil {
			oldValues[key] = nil
		} else {
			oldValues[key] = raw
		}
	}

	// Apply all updates first without validation
	for key, value := range updates {
		if err := cm.setInternal(ctx, key, value, false); err != nil {
			return fmt.Errorf("failed to set config value for key %s: %w", key, err)
		}
	}

	// Validate affected structs if needed
	if validate && cm.ValidationEnabled() {
		if err := cm.validateStructUpdates(updates); err != nil {
			// Revert all changes if validation fails
			for key, oldValue := range oldValues {
				_ = cm.setInternal(ctx, key, oldValue, false)
			}
			return err
		}
	}

	// Deduplicate notifications by only notifying for keys that actually changed
	finalUpdates := make(map[string]any)
	finalOldValues := make(map[string]any)

	for key, newVal := range updates {
		oldVal := oldValues[key]
		if !reflect.DeepEqual(oldVal, newVal) {
			finalUpdates[key] = newVal
			finalOldValues[key] = oldVal
		}
	}

	if len(finalUpdates) > 0 {
		cm.notifyUpdates(finalUpdates, finalOldValues)
	}

	// Sync changes if needed
	for key, value := range updates {
		if cm.shouldSyncKey(key) {
			err := cm.syncMgr.Push(ctx, key, value, func(sKey string, sValue any) {
				cm.notifySubscribers(sKey, oldValues[sKey], sValue)
			})
			if err != nil {
				cm.logger.Error("failed to push config to sync manager after atomic set",
					zap.String("key", key),
					zap.Error(err))
			}
		}
	}

	return nil
}

// Set sets the configuration value for the given key.
func (cm *ConfigManagerDefault) Set(ctx context.Context, key string, value any) error {
	return cm.setInternal(ctx, key, value, true)
}

// BulkSet sets multiple configuration values without individual validation.
// Validation will be done once after all values are set.
func (cm *ConfigManagerDefault) BulkSet(ctx context.Context, updates map[string]any) error {
	// Track old values
	oldValues := make(map[string]any)
	for key := range updates {
		if val, _, err := cm.Get(key); err == nil {
			oldValues[key] = val
		}
	}

	// Apply all updates first
	for key, value := range updates {
		if err := cm.setInternal(ctx, key, value, false); err != nil {
			return err
		}
	}

	// Validate affected structs if validation is enabled
	if cm.ValidationEnabled() {
		if err := cm.validateStructUpdates(updates); err != nil {
			// Revert all changes if validation fails
			for key, oldValue := range oldValues {
				_ = cm.koanf.Set(key, oldValue)
			}
			return err
		}
	}

	// Notify changes
	cm.notifyUpdates(updates, oldValues)

	return nil
}

// setInternal is the internal implementation of setting a value with optional validation
func (cm *ConfigManagerDefault) setInternal(ctx context.Context, key string, value any, validate bool) error {
	// Get the old value before updating
	oldValue, _, _ := cm.Get(key)

	// Set the new value in koanf
	if err := cm.koanf.Set(key, value); err != nil {
		return fmt.Errorf("failed to set config value in koanf: %w", err)
	}

	// Find the nearest struct that needs updating
	structKey := cm.findNearestStructKey(key)

	// If synchronization is enabled and key should be synced, push the change
	if cm.syncMgr != nil && cm.shouldSyncKey(key) {
		// Push to sync manager and fire event on success
		err := cm.syncMgr.Push(ctx, key, value, func(sKey string, sValue any) {
			if structKey == "" {
				// No struct association - simple key/value update
				cm.notifySubscribers(sKey, oldValue, sValue)
			} else if validate {
				// Update and validate the associated struct
				newStruct, err := cm.getIntoStruct(structKey, nil)
				if err != nil {
					cm.logger.Error("failed to decode struct for key",
						zap.String("key", structKey),
						zap.Error(err))
					// Revert the change if validation fails
					_ = cm.koanf.Set(key, oldValue)
					return
				}

				// Validate the entire struct
				if err := cm.validateValue(structKey, newStruct); err != nil {
					// Revert the change if validation fails
					_ = cm.koanf.Set(key, oldValue)
					cm.logger.Error("validation failed for struct",
						zap.String("key", structKey),
						zap.Error(err))
					return
				}

				// Notify with the updated struct
				cm.notifySubscribers(structKey, oldValue, newStruct)
			}
		})
		if err != nil {
			cm.logger.Error("failed to push config to sync manager after local set",
				zap.String("key", key),
				zap.Error(err))
			return fmt.Errorf("failed to push config to sync manager: %w", err)
		}
	} else if validate && cm.ValidationEnabled() {
		if structKey == "" {
			// No struct association - simple key/value update
			cm.notifySubscribers(key, oldValue, value)
		} else {
			// Update and validate the associated struct
			newStruct, err := cm.getIntoStruct(structKey, nil)
			if err != nil {
				return fmt.Errorf("failed to decode struct for key %s: %w", structKey, err)
			}

			// Validate the entire struct
			if err := cm.validateValue(structKey, newStruct); err != nil {
				// Revert the change if validation fails
				_ = cm.koanf.Set(key, oldValue)
				return fmt.Errorf("validation failed for struct %s: %w", structKey, err)
			}

			// Notify with the updated struct
			cm.notifySubscribers(structKey, oldValue, newStruct)
		}
	} else if validate {
		// When validation is disabled but validate=true, we still notify subscribers
		// to maintain consistency in change notifications
		if structKey == "" {
			cm.notifySubscribers(key, oldValue, value)
		} else {
			newStruct, err := cm.getIntoStruct(structKey, nil)
			if err != nil {
				return fmt.Errorf("failed to decode struct for key %s: %w", structKey, err)
			}
			cm.notifySubscribers(structKey, oldValue, newStruct)
		}
	}

	return nil
}

// getFilteredKeys returns keys filtered by prefixes if provided, otherwise all keys
func (cm *ConfigManagerDefault) getFilteredKeys(keyPrefix ...string) []string {
	if len(keyPrefix) == 0 {
		return cm.Keys()
	}

	allKeys := cm.Keys()
	return lo.FlatMap(keyPrefix, func(prefix string, _ int) []string {
		return lo.Filter(allKeys, func(key string, _ int) bool {
			return strings.HasPrefix(key, prefix+keySeparator)
		})
	})
}

// Validate validates the configuration. If keyPrefix is provided, only validates
// keys starting with that prefix. If no prefix is provided, validates the entire config at root level.
// Returns an error if any requested keys don't exist.
func (cm *ConfigManagerDefault) Validate(keyPrefix ...string) error {
	var errs []error

	if len(keyPrefix) > 0 {
		// First check if all requested prefixes have matching keys
		keys := cm.getFilteredKeys(keyPrefix...)
		if len(keys) == 0 {
			if len(keyPrefix) == 1 {
				return fmt.Errorf("configuration key '%s' not found", keyPrefix[0])
			}
			return fmt.Errorf("no configuration keys found matching prefixes: %v", keyPrefix)
		}

		// Validate specific keys when prefixes are provided
		for _, key := range keys {
			if err := cm.validateConfig(key); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", key, err))
			}
		}
	} else {
		// Validate entire config at root level when no prefixes are provided
		if err := cm.validateConfig(""); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration validation failed: %v", errs)
	}
	return nil
}

func (cm *ConfigManagerDefault) validateConfig(key string) error {
	if key == "" {
		// For root validation, use the entire config
		return cm.validateValue(key, cm.All())
	}

	// First check if the key exists in the configuration
	if !cm.Exists(key) {
		return fmt.Errorf("configuration key '%s' not found", key)
	}

	// Check if we have a registered struct for this key
	if cm.hasConfigStruct(key) {
		cfg, _, err := cm.Get(key)
		if err != nil {
			return fmt.Errorf("failed to get config for validation: %w", err)
		}
		return cm.validateValue(key, cfg)
	}

	// Try to find the nearest parent key with a registered struct
	parentKey := cm.findNearestStructKey(key)
	if parentKey == "" {
		// No parent struct, just validate the raw value
		raw, _, err := cm.Get(key)
		if err != nil {
			return err
		}
		return cm.validateValue(key, raw)
	}

	// Get the parent struct (decoded into its registered type) and validate it
	_, parentStruct, err := cm.Get(parentKey)
	if err != nil {
		return fmt.Errorf("failed to get parent config for validation: %w", err)
	}

	// Validate the parent struct which will include our key
	return cm.validateValue(parentKey, parentStruct)
}

func (cm *ConfigManagerDefault) validateValue(key string, val any) error {
	// First check for advanced Validator implementation
	if validator, ok := val.(Validator); ok {
		if err := validator.Validate(); err != nil {
			cm.logger.Error("Configuration validation error",
				zap.String("config_key", key),
				zap.Error(err))
			return fmt.Errorf("%s: %w", key, err)
		}
	}

	// Validate using zog schema if available
	if provider, ok := val.(ConfigSchemaProvider); ok {
		cm.logger.Debug("Found ConfigSchemaProvider implementation", zap.String("key", key))
		if schema := provider.Schema(); schema != nil {
			if err := cm.schemaValidate(key, val, schema); err != nil {
				cm.logger.Error("Schema validation failed",
					zap.String("key", key),
					zap.Error(err))
				return err
			}
		}
	}

	cm.logger.Debug("Validation passed", zap.String("key", key))
	return nil
}
func (cm *ConfigManagerDefault) schemaValidate(key string, value any, schema zog.ZogSchema) error {
	var issues zog.ZogIssueMap

	switch v := schema.(type) {
	case *zog.StructSchema:
		// Struct schemas return issue maps
		issues = v.Validate(value)
	case *zog.PointerSchema:
		// Pointer schemas return issue maps
		issues = v.Validate(value)
	default:
		err := fmt.Errorf("unsupported schema type for validation: %T", schema)
		cm.logger.Error("Configuration schema validation error",
			zap.String("config_key", key),
			zap.Error(err))
		return err
	}

	if len(issues) > 0 {
		// Convert issues to sanitized error messages
		sanitized := zog.Issues.SanitizeMap(issues)
		var errs []error
		for path, messages := range sanitized {
			fullPath := key
			if path != schemaValidationRootPath {
				fullPath = fmt.Sprintf("%s.%s", key, path)
			}
			for _, msg := range messages {
				errs = append(errs, fmt.Errorf("%s: %s", fullPath, msg))
			}
		}
		// Collect the issues for reuse
		zog.Issues.CollectMap(issues)
		return fmt.Errorf("configuration validation failed for %s: %v", key, errs)
	}
	return nil
}

// Persist persists the configuration by delegating to ConfigSource implementations.
func (cm *ConfigManagerDefault) Persist(keyPrefix ...string) error {
	var accumulatedError error
	for _, _source := range cm.sources {
		// Check if the source is persistable
		if ps, ok := _source.(source.PersistableConfigSource); ok {
			keys := cm.getFilteredKeys(keyPrefix...)

			persistKeys := lo.Reject(keys, func(key string, _ int) bool {
				return cm.isVolatile(key)
			})

			if len(persistKeys) > 0 {
				if err := ps.Persist(cm, persistKeys...); err != nil {
					cm.logger.Error("failed to persist config for a source", zap.Error(err))
					if accumulatedError == nil {
						accumulatedError = fmt.Errorf("error persisting source: %w", err)
					} else {
						accumulatedError = fmt.Errorf("%v; error persisting another source: %w", accumulatedError, err)
					}
				}
			}
		}
	}
	return accumulatedError
}

// Shutdown shuts down the ConfigManagerDefault.
func (cm *ConfigManagerDefault) Shutdown() error {
	// Stop all config source watchers
	errs := lo.FilterMap(cm.sources, func(_source source.ConfigSource, _ int) (error, bool) {
		if stoppable, ok := _source.(source.StoppableConfigSource); ok {
			if err := stoppable.Stop(); err != nil {
				cm.logger.Error("failed to stop config watcher",
					zap.String("source", fmt.Sprintf("%T", _source)),
					zap.Error(err))
				return fmt.Errorf("failed to stop config watcher: %w", err), true
			}
		}
		return nil, false
	})

	// Stop sync manager if configured
	if cm.syncMgr != nil {
		if err := cm.syncMgr.Stop(); err != nil {
			cm.logger.Error("failed to stop sync manager during shutdown", zap.Error(err))
			errs = append(errs, fmt.Errorf("failed to stop sync manager: %w", err))
		}
	}

	return lo.Ternary(len(errs) > 0,
		fmt.Errorf("shutdown encountered errors: %v", errs),
		nil)
}

// isVolatile checks if a configuration key is marked as volatile.
// Volatile keys are not persisted to durable storage.
func (cm *ConfigManagerDefault) isVolatile(key string) bool {
	return cm.flagManager.HasFlag(key, "volatile")
}

// hasConfigStruct checks if a key has a registered config struct
func (cm *ConfigManagerDefault) hasConfigStruct(key string) bool {
	cm.configStructLock.RLock()
	defer cm.configStructLock.RUnlock()
	_, ok := cm.configStructs[key]
	return ok
}

// implementsInterface checks if a registered struct implements an interface
func (cm *ConfigManagerDefault) implementsInterface(key string, iface reflect.Type) bool {
	cm.configStructLock.RLock()
	defer cm.configStructLock.RUnlock()

	structType, ok := cm.configStructs[key]
	if !ok {
		return false
	}

	return ireflect.ImplementsInterface(structType, iface)
}

// implementsValidator checks if a registered struct implements Validator
func (cm *ConfigManagerDefault) implementsValidator(key string) bool {
	cm.configStructLock.RLock()
	defer cm.configStructLock.RUnlock()

	structType, ok := cm.configStructs[key]
	if !ok {
		return false
	}

	return ireflect.ImplementsValidator(structType)
}

// implementsConfigSchemaProvider checks if a registered struct implements ConfigSchemaProvider
func (cm *ConfigManagerDefault) implementsConfigSchemaProvider(key string) bool {
	cm.configStructLock.RLock()
	defer cm.configStructLock.RUnlock()

	structType, ok := cm.configStructs[key]
	if !ok {
		return false
	}

	return ireflect.ImplementsConfigSchemaProvider(structType)
}

// implementsConfigDefaults checks if a registered struct implements ConfigDefaults
func (cm *ConfigManagerDefault) implementsConfigDefaults(key string) bool {
	cm.configStructLock.RLock()
	defer cm.configStructLock.RUnlock()

	structType, ok := cm.configStructs[key]
	if !ok {
		return false
	}

	return ireflect.ImplementsConfigDefaults(structType)
}

// FlagManager returns the flag manager instance
func (cm *ConfigManagerDefault) FlagManager() FlagManager {
	return cm.flagManager
}

// GetRegisteredStructs returns a copy of the registered config structs
func (cm *ConfigManagerDefault) GetRegisteredStructs() map[string]reflect.Type {
	cm.configStructLock.RLock()
	defer cm.configStructLock.RUnlock()

	copy := make(map[string]reflect.Type, len(cm.configStructs))
	for k, v := range cm.configStructs {
		copy[k] = v
	}
	return copy
}

// findNearestStructKey finds the nearest parent key that has a registered struct
func (cm *ConfigManagerDefault) findNearestStructKey(key string) string {
	parts := strings.Split(key, keySeparator)
	for i := len(parts); i > 0; i-- {
		potentialKey := strings.Join(parts[:i], keySeparator)
		if cm.hasConfigStruct(potentialKey) {
			return potentialKey
		}
	}
	return ""
}

// Keys returns all configuration keys
func (cm *ConfigManagerDefault) Keys() []string {
	return cm.koanf.Keys()
}

// ListKeys returns all configuration keys matching the given prefix
func (cm *ConfigManagerDefault) ListKeys(prefix string) []string {
	return cm.getFilteredKeys(prefix)
}

// Delete removes a configuration key and notifies watchers
func (cm *ConfigManagerDefault) Delete(key string) {
	// Get the old value before deleting
	var oldValue any
	if cm.Exists(key) {
		oldValue = cm.koanf.Get(key)
	}

	cm.koanf.Delete(key)

	// Notify watchers of the deletion
	cm.notifySubscribers(key, oldValue, nil)
}

// Delim returns the delimiter used for nested keys
func (cm *ConfigManagerDefault) Root(target any) (any, error) {
	if !cm.hasConfigStruct("") {
		return nil, fmt.Errorf("no root configuration struct registered - use RegisterStruct(\"\", yourStruct{})")
	}
	return cm.getIntoStruct("", target)
}

func (cm *ConfigManagerDefault) Delim() string {
	if cm.delimiter != "" {
		return cm.delimiter
	}
	return cm.koanf.Delim()
}

// ConfigFile returns the path to the configuration file
func (cm *ConfigManagerDefault) ConfigFile() string {
	return cm.configFile
}

// ConfigDir returns the path to the configuration directory
func (cm *ConfigManagerDefault) ConfigDir() string {
	return cm.configDir
}

// shouldSyncKey checks if a configuration key should be synchronized.
// Only non-volatile keys that are marked for sync should be synchronized.
func (cm *ConfigManagerDefault) shouldSyncKey(key string) bool {
	return !cm.isVolatile(key) && cm.flagManager.HasFlag(key, "sync")
}

// RegisterNamespace associates a ConfigSource with a namespace
func (cm *ConfigManagerDefault) RegisterNamespace(namespace string, src source.ConfigSource) {
	cm.registry.Register(namespace, src)
}

// RegisteredNamespaces returns a map of all registered namespaces and their associated sources.
func (cm *ConfigManagerDefault) RegisteredNamespaces() map[string]source.ConfigSource {
	namespaces := make(map[string]source.ConfigSource)
	for _, ns := range cm.registry.ListNamespaces() {
		if src, ok := cm.registry.GetSource(ns); ok {
			namespaces[ns] = src
		}
	}
	return namespaces
}

// RegisterSource adds a new configuration source at runtime without loading/watching it
func (cm *ConfigManagerDefault) RegisterSource(src source.ConfigSource) {
	// Check if source is already registered
	if !lo.ContainsBy(cm.sources, func(s source.ConfigSource) bool {
		return s == src
	}) {
		cm.sources = append(cm.sources, src)
	}
}

// LoadSource loads and optionally watches a source, registering it first if needed
func (cm *ConfigManagerDefault) LoadSource(src source.ConfigSource, load bool, watch bool) error {
	cm.RegisterSource(src)

	// Load the source if requested
	if load {
		if err := src.Load(context.Background(), cm); err != nil {
			return fmt.Errorf("failed to load source: %w", err)
		}
	}

	// Start watching if requested and supported
	if watch {
		if err := src.Watch(context.Background(), cm, func(changedKeys []string, err error) {
			cm.handleConfigChanges(src, changedKeys)
		}); err != nil {
			cm.logger.Warn("failed to start watching source",
				zap.String("source", fmt.Sprintf("%T", src)),
				zap.Error(err))
			return fmt.Errorf("failed to start watching source: %w", err)
		}
	}

	return nil
}

// UnregisterNamespace removes a namespace from the registry and stops watching its source
func (cm *ConfigManagerDefault) UnregisterNamespace(namespace string) error {
	// Get the source before unregistering
	src, ok := cm.registry.GetSource(namespace)
	if !ok {
		return fmt.Errorf("namespace %s not found", namespace)
	}

	// Stop watching if the source supports it
	if stoppable, ok := src.(source.StoppableConfigSource); ok {
		if err := stoppable.Stop(); err != nil {
			cm.logger.Error("failed to stop config source watcher",
				zap.String("namespace", namespace),
				zap.Error(err))
			return fmt.Errorf("failed to stop source watcher: %w", err)
		}
	}

	// Remove from registry
	cm.registry.Unregister(namespace)

	// Delete all keys under this namespace from the main koanf
	cm.Delete(namespace)

	return nil
}

// LoadNamespaces loads all registered namespaces
func (cm *ConfigManagerDefault) LoadNamespaces() error {
	for _, namespace := range cm.registry.ListNamespaces() {
		if err := cm.LoadNamespace(namespace); err != nil {
			return fmt.Errorf("failed to load namespace %s: %w", namespace, err)
		}
	}
	return nil
}

// LoadNamespace loads configuration for a specific namespace
func (cm *ConfigManagerDefault) LoadNamespace(namespace string) error {
	src, ok := cm.registry.GetSource(namespace)
	if !ok {
		return fmt.Errorf("namespace %s not registered", namespace)
	}

	// Create throwaway config manager to load source data
	throwawayCM := cm.copy()

	// Load into throwaway copy
	if err := src.Load(context.Background(), throwawayCM); err != nil {
		return fmt.Errorf("failed to load from source: %w", err)
	}

	// Apply updates with namespace prefix and track changed keys
	allData := throwawayCM.All()
	updates := lo.Reduce(lo.Keys(allData), func(agg map[string]any, key string, _ int) map[string]any {
		fullKey := namespace + cm.Delim() + key
		agg[fullKey] = allData[key]
		return agg
	}, make(map[string]any))

	// Apply updates atomically
	if err := cm.BulkSetAtomic(context.Background(), updates); err != nil {
		return fmt.Errorf("failed to apply namespace updates: %w", err)
	}

	// Register the source for watching
	if err := src.Watch(context.Background(), cm, func(changedKeys []string, err error) {
		cm.handleConfigChanges(src, changedKeys)
	}); err != nil {
		cm.logger.Warn("failed to start namespace watcher",
			zap.String("namespace", namespace),
			zap.Error(err))
	}

	return nil
}

// ValidateRegisteredStructs validates all registered configuration structs
func (cm *ConfigManagerDefault) ValidateRegisteredStructs() error {
	var errs []error

	cm.configStructLock.RLock()
	defer cm.configStructLock.RUnlock()

	for structKey := range cm.configStructs {
		if !cm.Exists(structKey) {
			continue
		}

		newStruct, err := cm.getIntoStruct(structKey, nil)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to decode struct %s: %w", structKey, err))
			continue
		}

		if err := cm.validateValue(structKey, newStruct); err != nil {
			errs = append(errs, fmt.Errorf("validation failed for struct %s: %w", structKey, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("validation errors in registered structs: %v", errs)
	}
	return nil
}

// validateStructUpdates validates all structs affected by the updates
func (cm *ConfigManagerDefault) validateStructUpdates(updates map[string]any) error {
	structKeys := make(map[string]struct{}, len(updates)) // Pre-allocate with expected size
	for key := range updates {
		if structKey := cm.findNearestStructKey(key); structKey != "" {
			structKeys[structKey] = struct{}{}
		}
	}

	for structKey := range structKeys {
		newStruct, err := cm.getIntoStruct(structKey, nil)
		if err != nil {
			return fmt.Errorf("failed to decode struct for key %s: %w", structKey, err)
		}
		if err := cm.validateValue(structKey, newStruct); err != nil {
			return fmt.Errorf("validation failed for struct %s: %w", structKey, err)
		}
	}
	return nil
}

// notifyUpdates handles change notifications for updated keys
func (cm *ConfigManagerDefault) notifyUpdates(updates map[string]any, oldValues map[string]any) {
	structKeys := make(map[string]struct{}, len(updates)) // Pre-allocate with expected size
	for key := range updates {
		if structKey := cm.findNearestStructKey(key); structKey != "" {
			structKeys[structKey] = struct{}{}
		} else {
			cm.notifySubscribers(key, oldValues[key], updates[key])
		}
	}

	for structKey := range structKeys {
		newStruct, err := cm.getIntoStruct(structKey, nil)
		if err == nil {
			cm.notifySubscribers(structKey, oldValues[structKey], newStruct)
		}
	}
}
