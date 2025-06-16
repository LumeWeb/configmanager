package etcd

import (
	"context"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.lumeweb.com/configmanager/config"
	"go.uber.org/zap"
	"time"
)

// EtcdClient defines the interface for etcd client operations we need
type EtcdClient interface {
	Endpoints() []string
	Status(ctx context.Context, endpoint string) (*clientv3.StatusResponse, error)
	Close() error
	KV() clientv3.KV
	Watcher() clientv3.Watcher
}

// etcdClientWrapper wraps the real clientv3.Client to implement EtcdClient
type etcdClientWrapper struct {
	*clientv3.Client
}

func (w *etcdClientWrapper) Endpoints() []string {
	return w.Client.Endpoints()
}

func (w *etcdClientWrapper) KV() clientv3.KV {
	return clientv3.NewKV(w.Client)
}

func (w *etcdClientWrapper) Watcher() clientv3.Watcher {
	return clientv3.NewWatcher(w.Client)
}

// EtcdManager defines the interface for etcd manager operations
type EtcdManager interface {
	Client() EtcdClient
	KV() clientv3.KV
	Watcher() clientv3.Watcher
	Close() error
}

// etcdManager implements EtcdManager
type etcdManager struct {
	client     EtcdClient
	kv         clientv3.KV
	watcher    clientv3.Watcher
	logger     *zap.Logger
	ctx        context.Context
	cancelFunc context.CancelFunc
}

type EtcdManagerOption func(*etcdManager)

// WithClient sets a custom etcd client while ensuring it uses the provided config
func WithClient(client EtcdClient) EtcdManagerOption {
	return func(m *etcdManager) {
		m.client = client
	}
}

func NewEtcdManager(config *config.EtcdConfig, logger *zap.Logger, opts ...EtcdManagerOption) (EtcdManager, error) {
	ctx, cancel := context.WithCancel(context.Background())

	mgr := &etcdManager{
		logger:     logger,
		ctx:        ctx,
		cancelFunc: cancel,
	}

	// Apply options
	for _, opt := range opts {
		opt(mgr)
	}

	// Initialize client if not set by options
	if mgr.client == nil {
		rawClient, err := clientv3.New(clientv3.Config{
			Endpoints:   config.Endpoints,
			DialTimeout: time.Duration(config.DialTimeout) * time.Second,
			Username:    config.Username,
			Password:    config.Password,
		})
		if err != nil {
			cancel()
			return nil, err
		}
		mgr.client = &etcdClientWrapper{rawClient}
	}

	return mgr, nil
}

func (m *etcdManager) Client() EtcdClient {
	return m.client
}

func (m *etcdManager) KV() clientv3.KV {
	return m.client.KV()
}

func (m *etcdManager) Watcher() clientv3.Watcher {
	return m.client.Watcher()
}

func (m *etcdManager) Close() error {
	m.cancelFunc()
	if m.client != nil {
		return m.client.Close()
	}
	return nil
}
