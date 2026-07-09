package source

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
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
	watcher          *fsnotify.Watcher
	watcherDone      chan struct{}
	debounceTimer    *time.Timer
	watcherLock      sync.Mutex // Protects watcher, watcherDone, debounceTimer
	processWg        sync.WaitGroup // Tracks in-flight processFile invocations
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
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	realPath, err := filepath.EvalSymlinks(f.path)
	if err != nil {
		w.Close()
		return err
	}
	realPath = filepath.Clean(realPath)

	// Watch the parent directory to catch create/remove/rename events.
	fDir := filepath.Dir(f.path)
	if err := w.Add(fDir); err != nil {
		w.Close()
		return err
	}

	f.watcher = w
	f.watcherDone = make(chan struct{})

	go func() {
		defer close(f.watcherDone)
		var (
			lastEvent     string
			lastEventTime time.Time
		)

		processFile := func() {
			defer f.processWg.Done()
			tmpKoanf := koanf.New(".")
			if err := tmpKoanf.Load(f.provider, yaml.Parser()); err != nil {
				cb(nil, err)
				return
			}

			f.prevLock.Lock()
			changedKeys := f.detectChangedKeys(f.prevState, tmpKoanf.All())
			f.prevState = tmpKoanf.All()
			f.prevLock.Unlock()

			if len(changedKeys) == 0 {
				cb(nil, nil)
				return
			}

			for _, key := range changedKeys {
				if tmpKoanf.Exists(key) {
					if err := cm.Set(ctx, key, tmpKoanf.Get(key)); err != nil {
						cb(nil, err)
						return
					}
				} else {
					if err := cm.Set(ctx, key, nil); err != nil {
						cb(nil, err)
						return
					}
				}
			}

			cb(changedKeys, nil)
		}

		for {
			select {
			case event, ok := <-w.Events:
				if !ok {
					return
				}

				// Debounce duplicate events (some platforms fire multiple times).
				if event.String() == lastEvent && time.Since(lastEventTime) < 5*time.Millisecond {
					continue
				}
				lastEvent = event.String()
				lastEventTime = time.Now()

				evFile := filepath.Clean(event.Name)
				if evFile != realPath && evFile != f.path {
					continue
				}

				// File was removed.
				if event.Op&fsnotify.Remove != 0 {
					f.stopDebounce()
					cb(AllChanges, fmt.Errorf("file %s was removed", event.Name))
					return
				}

				// Resolve symlink in case the target changed.
				curPath, err := filepath.EvalSymlinks(f.path)
				if err != nil {
					f.stopDebounce()
					cb(nil, err)
					return
				}
				realPath = filepath.Clean(curPath)

				// Only care about write and create events.
				if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}

				// Debounce: wait 50ms for events to settle before processing.
				// os.WriteFile can trigger multiple fsnotify events; reading
				// during a partial write produces incorrect diff results.
				f.watcherLock.Lock()
				if f.debounceTimer != nil {
					// Stop the previous timer. If Stop returns true the func
					// hasn't fired yet, so undo the Add(1) to keep the
					// WaitGroup balanced. If false, processFile already ran
					// (or is running) and will call Done itself.
					if f.debounceTimer.Stop() {
						f.processWg.Done()
					}
				}
				f.processWg.Add(1)
				f.debounceTimer = time.AfterFunc(50*time.Millisecond, processFile)
				f.watcherLock.Unlock()

			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				f.stopDebounce()
				cb(nil, err)
				return
			}
		}
	}()

	return nil
}

// stopDebounce stops the pending debounce timer and waits for any
// in-flight processFile to complete, ensuring no concurrent cb()
// invocations. Called from the watcher goroutine on Remove, Error,
// and EvalSymlinks-failure paths before invoking cb() directly.
func (f *fileSource) stopDebounce() {
	f.watcherLock.Lock()
	var pending bool
	if f.debounceTimer != nil {
		if f.debounceTimer.Stop() {
			f.processWg.Done()
		} else {
			pending = true // processFile already fired or is running
		}
		f.debounceTimer = nil
	}
	f.watcherLock.Unlock()
	if pending {
		f.processWg.Wait()
	}
}

func (f *fileSource) Stop() error {
	f.watcherLock.Lock()
	if f.watcher == nil {
		f.watcherLock.Unlock()
		return nil
	}
	watcher := f.watcher
	f.watcherLock.Unlock()

	f.stopDebounce()

	err := watcher.Close()
	<-f.watcherDone

	// Re-stop any timer the goroutine may have created between the
	// unlock above and its exit; the goroutine cannot create any more
	// timers once watcherDone is closed.
	f.stopDebounce()

	f.watcherLock.Lock()
	f.watcher = nil
	f.watcherLock.Unlock()

	// Guarantee no processFile outlives Stop(), even if a previously
	// replaced timer's processFile is still running.
	f.processWg.Wait()

	return err
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
	// Create a new koanf instance to collect and organize the config
	persistKoanf := koanf.New(cm.Delim())

	// Get all keys if none specified
	if len(keys) == 0 {
		keys = cm.Keys()
	}

	// Collect all key-value pairs into the koanf instance
	for _, key := range keys {
		// Handle namespaced keys
		fullKey := key
		if namespace != "" {
			fullKey = namespace + cm.Delim() + key
		}

		if value, _, err := cm.Get(fullKey); err == nil {
			err = persistKoanf.Set(fullKey, value)
			if err != nil {
				return err
			}
		}
	}

	// Get the final config to persist by cutting the namespace if needed
	var configToPersist map[string]any
	if namespace != "" {
		configToPersist = persistKoanf.Cut(namespace).Raw()
	} else {
		configToPersist = persistKoanf.Raw()
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp(filepath.Dir(f.path), ".config_tmp_*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write config data directly to temporary file
	enc := yyaml.NewEncoder(tmpFile)
	defer enc.Close()

	if err := checkForUnsupportedTypes(configToPersist); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("cannot persist config: %w", err)
	}

	if err := enc.Encode(configToPersist); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("cannot persist config: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	// Rename temporary file to final destination
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
