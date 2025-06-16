package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"go.lumeweb.com/configmanager/config"
	"go.lumeweb.com/configmanager/internal/etcd"
	"go.uber.org/zap"
	"strings"
	"sync"
)

var _ Client = (*EtcdSyncClient)(nil)

// EtcdSyncClient implements SyncClient for etcd
type EtcdSyncClient struct {
	manager  etcd.EtcdManager
	prefix   string
	logger   *zap.Logger
	configNS string       // Namespace for sync client configuration
	mu       sync.RWMutex // Protects manager field
}

// EtcdSyncClientOption configures an EtcdSyncClient
type EtcdSyncClientOption func(*EtcdSyncClient)

// WithEtcdManager sets a custom etcd manager
func WithEtcdManager(manager etcd.EtcdManager) EtcdSyncClientOption {
	return func(e *EtcdSyncClient) {
		e.manager = manager
	}
}

// NewEtcdSyncClient creates a new EtcdSyncClient
func NewEtcdSyncClient(
	manager etcd.EtcdManager,
	prefix string,
	logger *zap.Logger,
	opts ...EtcdSyncClientOption,
) *EtcdSyncClient {
	client := &EtcdSyncClient{
		manager: manager,
		prefix:  prefix,
		logger:  logger,
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

// Configure implements sync.Client interface
func (e *EtcdSyncClient) Configure(manager configManager, namespace string, opts ...SyncOption) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.configNS = namespace

	// Get etcd config from manager
	var etcdConfig config.EtcdConfig
	if _, err := manager.Get(namespace, &etcdConfig); err != nil {
		return fmt.Errorf("failed to get etcd config from manager: %w", err)
	}

	// Close and clear existing manager if it exists
	if e.manager != nil {
		if err := e.manager.Close(); err != nil {
			e.logger.Warn("failed to close existing etcd manager", zap.Error(err))
			// Don't clear manager reference if closing failed
			// This allows for potential retry or manual cleanup
		} else {
			e.manager = nil
		}
	}

	// Apply sync options first - allows options to set their own manager
	for _, opt := range opts {
		if err := opt(e); err != nil {
			return fmt.Errorf("failed to apply sync option: %w", err)
		}
	}

	// Only create new manager if none was set by options
	if e.manager == nil {
		newManager, err := etcd.NewEtcdManager(&etcdConfig, e.logger)
		if err != nil {
			return fmt.Errorf("failed to create etcd manager: %w", err)
		}
		e.manager = newManager
	}
	e.logger.Info("EtcdSyncClient configured successfully",
		zap.String("namespace", namespace),
		zap.Any("config", etcdConfig))
	return nil
}

// WithEtcdLogger sets the logger for the etcd client
func WithEtcdLogger(logger *zap.Logger) EtcdSyncClientOption {
	return func(e *EtcdSyncClient) {
		e.logger = logger
	}
}

// WithEtcdPrefix sets the key prefix for the etcd client
func WithEtcdPrefix(prefix string) EtcdSyncClientOption {
	return func(e *EtcdSyncClient) {
		e.prefix = prefix
	}
}

// Start implements SyncClient interface
func (e *EtcdSyncClient) Start(ctx context.Context) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.manager == nil {
		return fmt.Errorf("etcd manager is nil")
	}

	// Verify connection by getting status
	client := e.manager.Client()
	if client == nil {
		return fmt.Errorf("etcd client is nil")
	}
	endpoints := client.Endpoints()
	if len(endpoints) == 0 {
		return fmt.Errorf("no etcd endpoints configured")
	}
	_, err := client.Status(ctx, endpoints[0])
	if err != nil {
		return fmt.Errorf("failed to connect to etcd: %w", err)
	}
	return nil
}

// Stop implements SyncClient interface
func (e *EtcdSyncClient) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.manager == nil {
		return nil
	}
	return e.manager.Close()
}

func (e *EtcdSyncClient) fullKey(key string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.prefix == "" {
		return key
	}
	return fmt.Sprintf("%s/%s", e.prefix, key)
}

// Push implements Client interface by pushing a key-value pair to etcd
func (e *EtcdSyncClient) Push(ctx context.Context, key string, value any, callback PushCallback) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.manager == nil {
		return fmt.Errorf("etcd manager is nil")
	}

	fullKey := e.fullKey(key)
	jsonValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	_, err = e.manager.KV().Put(ctx, fullKey, string(jsonValue))
	if err != nil {
		return fmt.Errorf("failed to put key %s to etcd: %w", fullKey, err)
	}

	if callback != nil {
		callback(key, value)
	}

	return nil
}

// Watch implements Client interface by watching a key in etcd for changes
func (e *EtcdSyncClient) Watch(ctx context.Context, key string, onChange func(key string, value any)) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.manager == nil {
		return fmt.Errorf("etcd manager is nil")
	}

	fullKey := e.fullKey(key)
	rch := e.manager.Watcher().Watch(ctx, fullKey)

	go func() {
		for {
			select {
			case resp, ok := <-rch:
				if !ok {
					e.logger.Debug("etcd watch channel closed")
					return
				}

				for _, ev := range resp.Events {
					if ev.Kv == nil {
						continue
					}

					var value any
					if err := json.Unmarshal(ev.Kv.Value, &value); err != nil {
						e.logger.Error("failed to unmarshal etcd watch value",
							zap.String("key", string(ev.Kv.Key)),
							zap.Error(err))
						continue
					}

					// Strip prefix from key before calling onChange
					key := strings.TrimPrefix(string(ev.Kv.Key), e.prefix)
					key = strings.TrimPrefix(key, "/")
					onChange(key, value)
				}

			case <-ctx.Done():
				e.logger.Debug("etcd watch exiting due to context cancellation")
				return
			}
		}
	}()

	return nil
}
