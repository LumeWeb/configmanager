package source

import (
	"context"
	"go.uber.org/zap"
	"sync"

	"github.com/knadh/koanf/v2"
)

// MemoryConfigSource is an in-memory configuration source that can be used for testing
// or temporary configuration storage.
type MemoryConfigSource struct {
	data      map[string]any
	mutex     sync.RWMutex
	watchers  []WatchOnChangeCallback
	watchLock sync.Mutex
	logger    *zap.Logger
}

// NewMemoryConfigSource creates a new MemoryConfigSource with optional initial data.
func NewMemoryConfigSource(initialData map[string]any, opts ...MemoryConfigSourceOption) *MemoryConfigSource {
	data := make(map[string]any)
	if initialData != nil {
		for k, v := range initialData {
			data[k] = v
		}
	}

	m := &MemoryConfigSource{
		data:   data,
		logger: zap.NewNop(), // Default no-op logger
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

type MemoryConfigSourceOption func(*MemoryConfigSource)

func WithMemorySourceLogger(logger *zap.Logger) MemoryConfigSourceOption {
	return func(m *MemoryConfigSource) {
		m.logger = logger
	}
}

// Load loads the in-memory configuration into the Koanf instance.
func (m *MemoryConfigSource) Load(ctx context.Context, k *koanf.Koanf) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	for key, value := range m.data {
		if err := k.Set(key, value); err != nil {
			return err
		}
	}
	return nil
}

// Watch registers a callback to be notified when changes occur.
func (m *MemoryConfigSource) Watch(ctx context.Context, k *koanf.Koanf, cb WatchOnChangeCallback) error {
	m.watchLock.Lock()
	defer m.watchLock.Unlock()

	// Wrap the callback to update the koanf instance first
	wrappedCb := func(changedKeys []string, err error) {
		m.mutex.RLock()
		if len(m.data) == 0 {
			// Source was cleared - clear all keys from koanf
			allKeys := k.Keys()
			for _, key := range allKeys {
				k.Delete(key)
			}
		} else {
			// Update koanf with current values
			for key, value := range m.data {
				if err := k.Set(key, value); err != nil {
					m.logger.Error("failed to update koanf value",
						zap.String("key", key),
						zap.Error(err))
				}
			}
		}
		m.mutex.RUnlock()

		// Call original callback
		cb(changedKeys, err)
	}

	m.watchers = append(m.watchers, wrappedCb)
	return nil
}

// Set sets a value in the memory source.
func (m *MemoryConfigSource) Set(key string, value any) {
	m.mutex.Lock()
	m.data[key] = value
	m.mutex.Unlock()

	m.watchLock.Lock()
	watchers := make([]WatchOnChangeCallback, len(m.watchers))
	copy(watchers, m.watchers)
	m.watchLock.Unlock()

	for _, cb := range watchers {
		cb([]string{key}, nil)
	}
}

// Delete removes a key from the memory source.
func (m *MemoryConfigSource) Delete(key string) {
	m.mutex.Lock()
	delete(m.data, key)
	m.mutex.Unlock()

	m.watchLock.Lock()
	watchers := make([]WatchOnChangeCallback, len(m.watchers))
	copy(watchers, m.watchers)
	m.watchLock.Unlock()

	for _, cb := range watchers {
		cb([]string{key}, nil)
	}
}

// Clear removes all data from the memory source.
func (m *MemoryConfigSource) Clear() {
	m.mutex.Lock()
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	m.data = make(map[string]any)
	m.mutex.Unlock()

	m.watchLock.Lock()
	watchers := make([]WatchOnChangeCallback, len(m.watchers))
	copy(watchers, m.watchers)
	m.watchLock.Unlock()

	for _, cb := range watchers {
		cb(keys, nil)
	}
}

// Persist implements PersistableConfigSource but does nothing since memory is ephemeral.
func (m *MemoryConfigSource) Persist(ctx context.Context, k *koanf.Koanf, keyPrefix ...string) error {
	// Memory source doesn't persist changes
	return nil
}
