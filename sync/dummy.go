package sync

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"sync"
)

// DummySyncClient is an in-memory implementation of Client for testing
type DummySyncClient struct {
	data           map[string]any
	watchCallbacks map[string]func(key string, value any)
	mu             sync.RWMutex
	logger         *zap.Logger
	manager        configManager // Reference to config manager
	configNS       string        // Namespace for config
}

// NewDummySyncClient creates a new DummySyncClient
func NewDummySyncClient() *DummySyncClient {
	return &DummySyncClient{
		data:           make(map[string]any),
		watchCallbacks: make(map[string]func(key string, value any)),
		logger:         zap.NewNop(), // Default to no-op logger
	}
}

// Configure implements sync.Client interface
func (d *DummySyncClient) Configure(manager configManager, namespace string, opts ...SyncOption) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.manager = manager
	d.configNS = namespace
	// Apply sync options
	for _, opt := range opts {
		if err := opt(d); err != nil {
			return fmt.Errorf("failed to apply sync option: %w", err)
		}
	}

	d.logger.Info("Dummy sync client configured",
		zap.String("namespace", namespace))
	return nil
}

// WithLogger sets the logger for the dummy client
func (d *DummySyncClient) WithLogger(logger *zap.Logger) *DummySyncClient {
	d.logger = logger
	return d
}

// Start initializes the dummy client (no-op)
func (d *DummySyncClient) Start(ctx context.Context) error {
	d.logger.Debug("DummySyncClient started")
	return nil
}

// Stop cleans up the dummy client (no-op)
func (d *DummySyncClient) Stop() error {
	d.logger.Debug("DummySyncClient stopped")
	return nil
}

// Push stores a value in memory, calls the callback, and simulates a change notification
func (d *DummySyncClient) Push(ctx context.Context, key string, value any, callback PushCallback) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	oldValue := d.data[key]
	d.data[key] = value

	d.logger.Debug("Pushing value",
		zap.String("key", key),
		zap.Any("value", value),
		zap.Any("old_value", oldValue))

	// Call the push callback if provided
	if callback != nil {
		d.logger.Debug("Calling push callback",
			zap.String("key", key))
		callback(key, value)
	}

	// Simulate change notification
	if oldValue != value {
		d.logger.Debug("Value changed, triggering watch callbacks",
			zap.String("key", key))

		// Call all matching watch callbacks
		for watchKey, onChange := range d.watchCallbacks {
			if watchKey == key {
				d.logger.Debug("Calling watch callback",
					zap.String("key", key))
				onChange(key, value) // Run synchronously for tests
			}
		}
	} else {
		d.logger.Debug("Value unchanged, skipping watch callbacks",
			zap.String("key", key))
	}

	return nil
}

// Watch simulates watching a key by storing the callback
func (d *DummySyncClient) Watch(ctx context.Context, key string, onChange func(key string, value any)) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.watchCallbacks[key] = onChange

	d.logger.Debug("Registered watch callback",
		zap.String("key", key),
		zap.Int("total_watches", len(d.watchCallbacks)))

	return nil
}

// Get retrieves a value from the dummy store (for testing)
func (d *DummySyncClient) Get(key string) any {
	d.mu.RLock()
	defer d.mu.RUnlock()

	value := d.data[key]
	d.logger.Debug("Getting value",
		zap.String("key", key),
		zap.Any("value", value))

	return value
}

// Set sets a value in the dummy store (for testing)
func (d *DummySyncClient) Set(key string, value any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.data[key] = value

	d.logger.Debug("Setting value",
		zap.String("key", key),
		zap.Any("value", value))
}

// Reset clears all data from the dummy store
func (d *DummySyncClient) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.data = make(map[string]any)

	d.logger.Debug("Reset all data",
		zap.Int("watch_callbacks_cleared", len(d.watchCallbacks)))
	d.watchCallbacks = make(map[string]func(key string, value any))
}
