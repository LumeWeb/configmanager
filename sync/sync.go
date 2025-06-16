package sync

import (
	"context"
	"fmt"
)

// Client defines the interface for configuration synchronization clients
type Client interface {
	// Start initializes the synchronization client
	Start(ctx context.Context) error
	// Stop shuts down the synchronization client
	Stop() error
	// Push sends a configuration change to the synchronization backend
	Push(ctx context.Context, key string, value any, callback PushCallback) error
	// Watch monitors a key for changes from the synchronization backend
	Watch(ctx context.Context, key string, onChange func(key string, value any)) error
	// Configure configures the synchronization client using the config manager and options
	Configure(manager configManager, namespace string, opts ...SyncOption) error
}

// Manager handles the synchronization of configuration data with a distributed store.
type Manager interface {
	// Start starts the synchronization process.
	Start(ctx context.Context) error
	// Stop stops the synchronization process.
	Stop() error
	// Push pushes a configuration change to the distributed store.
	Push(ctx context.Context, key string, value any, callback PushCallback) error
	// Configure configures the synchronization manager using the config manager
	Configure(manager configManager, namespace string, opts ...SyncOption) error
}
type configManager interface {
	Get(key string, target ...any) (any, error)
}

type CManager = configManager

// SyncOption configures a sync client
type SyncOption func(Client) error

// WithSyncEtcdClientOption converts an EtcdSyncClientOption to a SyncOption
func WithSyncEtcdClientOption(opt EtcdSyncClientOption) SyncOption {
	return func(c Client) error {
		if etcdClient, ok := c.(*EtcdSyncClient); ok {
			opt(etcdClient)
			return nil
		}
		return fmt.Errorf("option is only valid for EtcdSyncClient")
	}
}
