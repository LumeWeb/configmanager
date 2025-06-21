package source

import (
	"context"
	"go.uber.org/zap"
	"reflect"
	"sync"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type fileSourceWrapper struct {
	provider         *file.File
	prevState        map[string]any
	prevLock         sync.Mutex
	changedThreshold float64 // Percentage (0-1) of keys that must change to trigger full reload
	logger           *zap.Logger
	initialLoad      bool // Track if this is the first load
}

type FileSourceOption func(*fileSourceWrapper)

func NewFileSource(path string, opts ...FileSourceOption) ConfigSource {
	f := &fileSourceWrapper{
		provider:         file.Provider(path),
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
	return func(f *fileSourceWrapper) {
		if threshold >= 0 && threshold <= 1 {
			f.changedThreshold = threshold
		}
	}
}

func WithFileSourceLogger(logger *zap.Logger) FileSourceOption {
	return func(f *fileSourceWrapper) {
		f.logger = logger
	}
}

func (f *fileSourceWrapper) Load(ctx context.Context, cm configManager) error {
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

func (f *fileSourceWrapper) Watch(ctx context.Context, cm configManager, cb WatchOnChangeCallback) error {
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

func (f *fileSourceWrapper) detectChangedKeys(oldState, newState map[string]any) []string {
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
