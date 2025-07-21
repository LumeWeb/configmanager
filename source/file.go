package source

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	yyaml "gopkg.in/yaml.v3"
)

type fileSource struct {
	provider         *file.File
	path             string
	prevState        map[string]any
	prevLock         sync.Mutex
	changedThreshold float64 // Percentage (0-1) of keys that must change to trigger full reload
	logger           *zap.Logger
	initialLoad      bool // Track if this is the first load
}

type FileSourceOption func(*fileSource)

func NewFileSource(path string, opts ...FileSourceOption) ConfigSource {
	f := &fileSource{
		provider:         file.Provider(path),
		path:             path,
		changedThreshold: 0.5,          // Default 50% threshold
		logger:           zap.NewNop(), // Default no-op logger
	}

	for _, opt := range opts {
		opt(f)
	}
	return f
}

// WithChangedThreshold sets the percentage (0-1) of keys that must change to trigger full reload
func WithChangedThreshold(threshold float64) FileSourceOption {
	return func(f *fileSource) {
		if threshold >= 0 && threshold <= 1 {
			f.changedThreshold = threshold
		}
	}
}

func WithFileSourceLogger(logger *zap.Logger) FileSourceOption {
	return func(f *fileSource) {
		f.logger = logger
	}
}

func (f *fileSource) Load(ctx context.Context, cm configManager) error {
	f.prevLock.Lock()
	defer f.prevLock.Unlock()

	// Initialize previous state if needed
	if f.prevState == nil {
		f.prevState = make(map[string]any)
	}

	// Create temporary koanf to load file
	tmpKoanf := koanf.New(".")
	if err := tmpKoanf.Load(f.provider, yaml.Parser()); err != nil {
		return err
	}

	// Store the new state
	newState := tmpKoanf.All()

	// Use BulkSetAtomic for atomic loading of all values
	if err := cm.BulkSetAtomic(ctx, newState); err != nil {
		return err
	}

	// Compare with previous state if this isn't the first load
	if f.initialLoad {
		changedKeys := f.detectChangedKeys(f.prevState, newState)
		if len(changedKeys) > 0 {
			f.logger.Debug("Detected configuration changes",
				zap.Strings("changed_keys", changedKeys))
		}
	}

	// Update previous state
	f.prevState = newState

	// Mark as loaded after first successful load
	if !f.initialLoad {
		f.initialLoad = true
	}

	return nil
}

func (f *fileSource) Watch(ctx context.Context, cm configManager, cb WatchOnChangeCallback) error {
	return f.provider.Watch(func(event any, err error) {
		if err != nil {
			// if err is not nil, it means the file was removed
			cb(AllChanges, err)
			return
		}

		// Create temporary koanf to load new values
		tmpKoanf := koanf.New(".")

		// Let koanf handle the reading and parsing
		if err := tmpKoanf.Load(f.provider, yaml.Parser()); err != nil {
			cb(nil, err)
			return
		}

		// Compare with previous state
		f.prevLock.Lock()
		changedKeys := f.detectChangedKeys(f.prevState, tmpKoanf.All())
		f.prevState = tmpKoanf.All()
		f.prevLock.Unlock()

		if len(changedKeys) == 0 {
			cb(nil, nil)
			return
		}

		// Update config manager with changed values
		for _, key := range changedKeys {
			if tmpKoanf.Exists(key) {
				if err := cm.Set(ctx, key, tmpKoanf.Get(key)); err != nil {
					cb(nil, err)
					return
				}
			} else {
				// Handle deleted keys by setting nil
				if err := cm.Set(ctx, key, nil); err != nil {
					cb(nil, err)
					return
				}
			}
		}

		cb(changedKeys, nil)
	})
}

func (f *fileSource) detectChangedKeys(oldState, newState map[string]any) []string {
	var changed []string
	// Create a set of all unique keys
	allKeys := make(map[string]struct{})
	for key := range oldState {
		allKeys[key] = struct{}{}
	}
	for key := range newState {
		allKeys[key] = struct{}{}
	}
	totalKeys := len(allKeys)
	if totalKeys == 0 {
		return nil
	}

	// Check for new or modified keys
	for key, newVal := range newState {
		oldVal, exists := oldState[key]
		if !exists || !reflect.DeepEqual(oldVal, newVal) {
			changed = append(changed, key)
		}
	}

	// Check for deleted keys
	for key := range oldState {
		if _, exists := newState[key]; !exists {
			changed = append(changed, key)
		}
	}

	if len(changed) == 0 {
		return nil
	}

	// If changed keys exceed threshold percentage, return full reload
	changeRatio := float64(len(changed)) / float64(totalKeys)
	if changeRatio >= f.changedThreshold {
		return AllChanges
	}

	return changed
}

func (f *fileSource) Persist(cm configManager, namespace string, keys ...string) error {
	// Store original keys to use for the final persisted data
	originalKeys := keys
	// If namespace is provided, we need to prefix the keys when getting values from manager
	if namespace != "" {
		prefixedKeys := make([]string, len(keys))
		for i, key := range keys {
			prefixedKeys[i] = namespace + cm.Delim() + key
		}
		keys = prefixedKeys
	}
	// Get all config if no keys specified
	var configToPersist map[string]any
	if len(keys) == 0 {
		// For full persist, strip namespace from all keys
		allConfig := cm.All()
		configToPersist = make(map[string]any)
		for key, value := range allConfig {
			if namespace != "" && strings.HasPrefix(key, namespace+cm.Delim()) {
				key = strings.TrimPrefix(key, namespace+cm.Delim())
			}
			configToPersist[key] = value
		}
	} else {
		configToPersist = make(map[string]any)
		allKeys := cm.Keys()

		for i, prefix := range keys {
			for _, key := range allKeys {
				if strings.HasPrefix(key, prefix) {
					if value, _, err := cm.Get(key); err == nil {
						// Use original key without namespace for persistence
						persistKey := originalKeys[i]
						if strings.HasPrefix(key, prefix) {
							suffix := strings.TrimPrefix(key, prefix)
							persistKey += suffix
						}
						configToPersist[persistKey] = value
					}
				}
			}
		}
	}

	// 2. Create a temporary file
	tmpFile, err := os.CreateTemp(filepath.Dir(f.path), ".config_tmp_*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// 3. Write to the temporary file
	enc := yyaml.NewEncoder(tmpFile)
	defer enc.Close()

	// First check if the config contains any unsupported types
	if err := checkForUnsupportedTypes(configToPersist); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("cannot persist config: %w", err)
	}

	err = enc.Encode(configToPersist)
	if err != nil {
		// Close the file before returning error
		_ = tmpFile.Close()
		return fmt.Errorf("cannot persist config: %w", err)
	}

	if err = tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	// 4. Rename the temporary file
	if err := os.Rename(tmpFile.Name(), f.path); err != nil {
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	return nil
}

// checkForUnsupportedTypes recursively checks for types that can't be marshaled to YAML
func checkForUnsupportedTypes(v any) error {
	switch val := v.(type) {
	case map[string]any:
		for _, vv := range val {
			if err := checkForUnsupportedTypes(vv); err != nil {
				return err
			}
		}
	case []any:
		for _, vv := range val {
			if err := checkForUnsupportedTypes(vv); err != nil {
				return err
			}
		}
	case func():
		return fmt.Errorf("unsupported type %T", val)
	default:
		// Use reflection to check for any channel type
		if reflect.TypeOf(v).Kind() == reflect.Chan {
			return fmt.Errorf("unsupported type %T (channel)", v)
		}
	}
	return nil
}
