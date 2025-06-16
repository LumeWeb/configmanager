package source

import (
	"context"
	"sync"

	"github.com/knadh/koanf/v2"
)

// MemoryConfigSource is an in-memory configuration source that can be used for testing
// or temporary configuration storage.
type MemoryConfigSource struct {
	data  map[string]any
	mutex sync.RWMutex
}

// NewMemoryConfigSource creates a new MemoryConfigSource with optional initial data.
func NewMemoryConfigSource(initialData map[string]any) *MemoryConfigSource {
	data := make(map[string]any)
	if initialData != nil {
		for k, v := range initialData {
			data[k] = v
		}
	}
	return &MemoryConfigSource{
		data: data,
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

// Watch does nothing for memory source as it doesn't support change notifications.
func (m *MemoryConfigSource) Watch(ctx context.Context, k *koanf.Koanf, cb WatchOnChangeCallback) error {
	// Memory source doesn't support watching for changes
	return nil
}

// Set sets a value in the memory source.
func (m *MemoryConfigSource) Set(key string, value any) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.data[key] = value
}

// Delete removes a key from the memory source.
func (m *MemoryConfigSource) Delete(key string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.data, key)
}

// Clear removes all data from the memory source.
func (m *MemoryConfigSource) Clear() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.data = make(map[string]any)
}

// Persist implements PersistableConfigSource but does nothing since memory is ephemeral.
func (m *MemoryConfigSource) Persist(ctx context.Context, k *koanf.Koanf, keyPrefix ...string) error {
	// Memory source doesn't persist changes
	return nil
}
