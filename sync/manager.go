package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.lumeweb.com/event/v2"
	"go.uber.org/zap"
)

// ManagerDefault handles configuration synchronization
type ManagerDefault struct {
	client        Client
	eventMgr      event.EventManager[ConfigEvent]
	logger        *zap.Logger
	watchedKeys   map[string]struct{}
	watchMu       sync.RWMutex
	localChanges  *sync.Map // key -> timestamp
	timeout       time.Duration
	cleanupCtx    context.Context
	cleanupCancel context.CancelFunc
}

// NewManager creates a new sync Manager with enhanced functionality
func NewManager(client Client, eventMgr event.EventManager[ConfigEvent], logger *zap.Logger) *ManagerDefault {
	if logger == nil {
		logger = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	mgr := &ManagerDefault{
		client:        client,
		eventMgr:      eventMgr,
		logger:        logger,
		watchedKeys:   make(map[string]struct{}),
		localChanges:  new(sync.Map),
		timeout:       5 * time.Second, // default timeout
		cleanupCtx:    ctx,
		cleanupCancel: cancel,
	}
	go mgr.cleanupStaleChanges()
	return mgr
}

// Configure implements Manager interface by delegating to the client
func (m *ManagerDefault) Configure(manager configManager, namespace string, opts ...SyncOption) error {
	if m.client == nil {
		return fmt.Errorf("sync client is nil")
	}
	return m.client.Configure(manager, namespace, opts...)
}

// Start begins synchronization
func (m *ManagerDefault) Start(ctx context.Context) error {
	if err := m.client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sync client: %w", err)
	}
	return nil
}

// Stop ends synchronization and cleans up resources
func (m *ManagerDefault) Stop() error {
	// Cancel the cleanup goroutine first
	if m.cleanupCancel != nil {
		m.cleanupCancel()
	}

	// Stop the client
	if err := m.client.Stop(); err != nil {
		return fmt.Errorf("failed to stop sync client: %w", err)
	}
	return nil
}

// Push pushes a configuration change
func (m *ManagerDefault) Push(ctx context.Context, key string, value any, callback PushCallback) error {
	m.watchMu.RLock()
	_, isWatched := m.watchedKeys[key]
	m.watchMu.RUnlock()

	if !isWatched {
		if err := m.watchKey(ctx, key); err != nil {
			return fmt.Errorf("failed to watch key before push: %w", err)
		}
	}

	// Mark this key as locally changed
	m.localChanges.Store(key, time.Now())

	// Create timeout context for push operation
	pushCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	// Push to external store
	err := m.client.Push(pushCtx, key, value, func(sKey string, sValue any) {
		// On success, remove from local changes
		m.localChanges.Delete(sKey)
		if callback != nil {
			callback(sKey, sValue)
		}
	})

	if err != nil {
		m.localChanges.Delete(key)
		return fmt.Errorf("failed to push config change: %w", err)
	}
	return nil
}

// watchKey starts watching a key for changes
func (m *ManagerDefault) watchKey(ctx context.Context, key string) error {
	m.watchMu.Lock()
	defer m.watchMu.Unlock()

	if _, exists := m.watchedKeys[key]; exists {
		return nil
	}

	err := m.client.Watch(ctx, key, func(changedKey string, newValue any) {
		// Skip if this is a local change
		if _, exists := m.localChanges.Load(changedKey); exists {
			m.logger.Debug("Skipping local change",
				zap.String("key", changedKey))
			return
		}

		// Handle external change
		m.logger.Debug("Detected external config change",
			zap.String("key", changedKey),
			zap.Any("value", newValue))

		if m.eventMgr != nil {
			evt := ConfigEvent{
				key:   changedKey,
				value: newValue,
			}

			m.logger.Debug("Firing config change event",
				zap.String("key", changedKey))

			if err := m.eventMgr.FireEvent(&evt); err != nil {
				m.logger.Error("Failed to fire config change event",
					zap.String("key", changedKey),
					zap.Error(err))
			} else {
				m.logger.Debug("Successfully fired config change event",
					zap.String("key", changedKey))
			}
		}
	})

	if err != nil {
		return fmt.Errorf("failed to watch key %s: %w", key, err)
	}

	m.watchedKeys[key] = struct{}{}
	return nil
}

// cleanupStaleChanges periodically checks for and removes stale local changes
func (m *ManagerDefault) cleanupStaleChanges() {
	ticker := time.NewTicker(100 * time.Millisecond) // Faster tick for tests
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			var toDelete []string

			// First collect all stale keys
			m.localChanges.Range(func(key, value any) bool {
				if ts, ok := value.(time.Time); ok && now.Sub(ts) > m.timeout {
					toDelete = append(toDelete, key.(string))
				}
				return true
			})

			// Then delete them
			for _, key := range toDelete {
				if ts, ok := m.localChanges.Load(key); ok {
					m.localChanges.Delete(key)
					if timestamp, ok := ts.(time.Time); ok {
						m.logger.Debug("Cleaned up stale local change",
							zap.String("key", key),
							zap.Duration("age", now.Sub(timestamp)))
					}
				}
			}
		case <-m.cleanupCtx.Done():
			m.logger.Debug("Stopping cleanup goroutine")
			return
		}
	}
}
