package source

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/samber/lo"
	"strings"
	"sync"

	"github.com/knadh/koanf/v2"
	clientv3 "go.etcd.io/etcd/client/v3"
	_ "go.etcd.io/etcd/client/v3/mock/mockserver"
	config "go.lumeweb.com/configmanager/config"
	"go.lumeweb.com/configmanager/internal/etcd"
	"go.uber.org/zap"
)

type EtcdConfigSourceOption func(*EtcdConfigSource)

// WithEtcdSourceEtcdManager injects a custom EtcdManager
func WithEtcdSourceEtcdManager(manager etcd.EtcdManager) EtcdConfigSourceOption {
	return func(e *EtcdConfigSource) {
		e.manager = manager
	}
}

// WithEtcdSourceLogger sets a custom logger
func WithEtcdSourceLogger(logger *zap.Logger) EtcdConfigSourceOption {
	return func(e *EtcdConfigSource) {
		e.logger = logger
	}
}

// EtcdConfigSource loads configuration from etcd.
type EtcdConfigSource struct {
	manager     etcd.EtcdManager
	prefix      string
	logger      *zap.Logger
	watchMu     sync.Mutex
	watchCancel context.CancelFunc
}

// Persist writes configuration changes back to etcd.
func (e *EtcdConfigSource) Persist(ctx context.Context, k *koanf.Koanf, keyPrefix ...string) error {
	e.logger.Debug("Persisting configuration to etcd",
		zap.Strings("keyPrefix", keyPrefix))

	// If no keys specified, persist everything
	keys := k.Keys()
	if len(keyPrefix) > 0 {
		keys = keyPrefix
		e.logger.Debug("Persisting prefixed keys",
			zap.Strings("keys", keys))
	} else {
		e.logger.Debug("Persisting all keys",
			zap.Strings("all_keys", keys))
	}

	for _, key := range keys {
		value := k.Get(key)
		if value == nil {
			e.logger.Debug("Skipping nil value",
				zap.String("key", key))
			continue
		}

		fullKey := e.fullKey(key)
		jsonValue, err := json.Marshal(value)
		if err != nil {
			e.logger.Error("Failed to marshal value",
				zap.String("key", key),
				zap.Error(err))
			return fmt.Errorf("failed to marshal value for key %s: %w", key, err)
		}

		e.logger.Debug("Persisting key to etcd",
			zap.String("key", fullKey),
			zap.Any("value", value))

		_, err = e.manager.KV().Put(ctx, fullKey, string(jsonValue))
		if err != nil {
			e.logger.Error("Failed to persist key to etcd",
				zap.String("key", fullKey),
				zap.Error(err))
			return fmt.Errorf("failed to persist key %s to etcd: %w", key, err)
		}

		e.logger.Debug("Successfully persisted key",
			zap.String("key", fullKey))
	}

	e.logger.Debug("Finished persisting configuration")
	return nil
}

// NewEtcdConfigSource creates a new EtcdConfigSource.
func NewEtcdConfigSource(config *config.EtcdConfig, opts ...EtcdConfigSourceOption) (*EtcdConfigSource, error) {
	e := &EtcdConfigSource{
		prefix: config.Prefix,
		logger: zap.NewNop(), // Default no-op logger
	}

	// Apply options
	for _, opt := range opts {
		opt(e)
	}

	// Create manager if not provided
	if e.manager == nil {
		manager, err := etcd.NewEtcdManager(config, e.logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create etcd manager: %w", err)
		}
		e.manager = manager
	}

	return e, nil
}

// Load loads the configuration from etcd into the Koanf instance.
func (e *EtcdConfigSource) Load(ctx context.Context, k *koanf.Koanf) error {
	e.logger.Debug("Loading configuration from etcd",
		zap.String("prefix", e.prefix))

	resp, err := e.manager.KV().Get(ctx, e.prefix, clientv3.WithPrefix())
	if err != nil {
		e.logger.Error("Failed to get etcd keys",
			zap.String("prefix", e.prefix),
			zap.Error(err))
		return fmt.Errorf("failed to get etcd keys: %w", err)
	}

	e.logger.Debug("Retrieved keys from etcd",
		zap.Int("count", len(resp.Kvs)))

	// Log all retrieved keys/values for debugging
	for _, kv := range resp.Kvs {
		e.logger.Debug("Retrieved etcd key-value",
			zap.String("key", string(kv.Key)),
			zap.ByteString("value", kv.Value),
			zap.Int64("version", kv.Version))
	}

	configMap := make(map[string]any)
	for _, kv := range resp.Kvs {
		key := strings.TrimPrefix(string(kv.Key), e.prefix)
		key = strings.TrimPrefix(key, "/")
		key = strings.ReplaceAll(key, "/", k.Delim())

		e.logger.Debug("Processing etcd key",
			zap.String("raw_key", string(kv.Key)),
			zap.String("processed_key", key))

		var value any
		if err := json.Unmarshal(kv.Value, &value); err != nil {
			e.logger.Warn("Failed to unmarshal etcd value",
				zap.String("key", key),
				zap.ByteString("raw_value", kv.Value),
				zap.Error(err))
			continue
		}

		configMap[key] = value
		e.logger.Debug("Added config value",
			zap.String("key", key),
			zap.Any("value", value))
	}

	e.logger.Debug("Final config map before loading into koanf",
		zap.Any("config_map", configMap))

	err = k.Load(newEtcdMapProvider(configMap), nil)
	if err != nil {
		e.logger.Error("Failed to load config map into koanf",
			zap.Error(err))
		return err
	}

	e.logger.Debug("Successfully loaded config from etcd",
		zap.Strings("keys", k.Keys()))
	return nil
}

// Watch watches for changes in etcd and triggers the onChange function when a change occurs.
func (e *EtcdConfigSource) Watch(ctx context.Context, k *koanf.Koanf, onChange WatchOnChangeCallback) error {
	e.watchMu.Lock()
	defer e.watchMu.Unlock()

	e.logger.Debug("Starting etcd watch",
		zap.String("prefix", e.prefix))

	// Cancel any existing watch
	if e.watchCancel != nil {
		e.logger.Debug("Canceling previous etcd watch")
		e.watchCancel()
	}

	watchCtx, cancel := context.WithCancel(ctx)
	e.watchCancel = cancel

	rch := e.manager.Watcher().Watch(watchCtx, e.prefix, clientv3.WithPrefix())
	e.logger.Debug("etcd watch started successfully")
	go func() {
		for resp := range rch {
			if resp.Canceled {
				e.logger.Debug("etcd watch canceled")
				return
			}

			changedKeys := lo.Map(resp.Events, func(ev *clientv3.Event, _ int) string {
				key := strings.TrimPrefix(string(ev.Kv.Key), e.prefix)
				return strings.TrimPrefix(key, "/")
			})

			if len(changedKeys) > 0 {
				e.logger.Debug("Detected etcd changes",
					zap.Strings("changed_keys", changedKeys))

				// Reload the config when changes are detected
				if err := e.Load(ctx, k); err != nil {
					e.logger.Error("Failed to reload config from etcd",
						zap.Error(err))
					continue
				}

				e.logger.Debug("Notifying watchers of changes",
					zap.Strings("changed_keys", changedKeys))
				onChange(changedKeys, nil)
			}
		}
	}()

	return nil
}

// Stop stops the etcd watcher if it's running.
func (e *EtcdConfigSource) Stop() error {
	e.watchMu.Lock()
	defer e.watchMu.Unlock()

	if e.watchCancel != nil {
		e.watchCancel()
		e.watchCancel = nil
	}
	return nil
}

// fullKey combines the etcd prefix with the configuration key
func (e *EtcdConfigSource) fullKey(key string) string {
	if e.prefix == "" {
		return key
	}
	return e.prefix + "/" + key
}

// etcdMapProvider implements koanf.Provider for a map[string]any specifically for etcd
type etcdMapProvider struct {
	data map[string]any
}

func newEtcdMapProvider(data map[string]any) *etcdMapProvider {
	return &etcdMapProvider{data: data}
}

func (m *etcdMapProvider) Read() (map[string]any, error) {
	return m.data, nil
}

func (m *etcdMapProvider) ReadBytes() ([]byte, error) {
	return nil, fmt.Errorf("etcdMapProvider does not support reading bytes")
}
