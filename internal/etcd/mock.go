package etcd

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// MockKV implements clientv3.KV for testing
type MockKV struct {
	data     map[string][]byte
	mu       sync.RWMutex
	watchers []*MockWatcher
}

func NewMockKV() *MockKV {
	return &MockKV{
		data: make(map[string][]byte),
	}
}

func (m *MockKV) Put(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data[key] = []byte(val)

	// Notify watchers
	for _, w := range m.watchers {
		w.notify(&clientv3.Event{
			Type: mvccpb.PUT,
			Kv: &mvccpb.KeyValue{
				Key:   []byte(key),
				Value: []byte(val),
			},
		})
	}

	return &clientv3.PutResponse{}, nil
}

func (m *MockKV) Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Apply options to determine the range
	op := clientv3.OpGet(key, opts...)
	if op.IsOptsWithPrefix() {
		// Handle prefix query
		var kvs []*mvccpb.KeyValue
		prefix := string(op.KeyBytes())
		for k, v := range m.data {
			if strings.HasPrefix(k, prefix) {
				kvs = append(kvs, &mvccpb.KeyValue{
					Key:   []byte(k),
					Value: v,
				})
			}
		}
		return &clientv3.GetResponse{Kvs: kvs}, nil
	}

	// Handle single key query
	val, exists := m.data[key]
	if !exists {
		return &clientv3.GetResponse{}, nil
	}

	return &clientv3.GetResponse{
		Kvs: []*mvccpb.KeyValue{
			{
				Key:   []byte(key),
				Value: val,
			},
		},
	}, nil
}

func (m *MockKV) Delete(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.DeleteResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	op := clientv3.OpDelete(key, opts...)
	var deletedKeys []string

	if op.IsOptsWithPrefix() {
		// Handle prefix delete
		prefix := string(op.KeyBytes())
		for k := range m.data {
			if strings.HasPrefix(k, prefix) {
				delete(m.data, k)
				deletedKeys = append(deletedKeys, k)
			}
		}
	} else {
		// Handle single key delete
		delete(m.data, key)
		deletedKeys = append(deletedKeys, key)
	}

	// Notify watchers
	for _, key := range deletedKeys {
		for _, w := range m.watchers {
			w.notify(&clientv3.Event{
				Type: mvccpb.DELETE,
				Kv: &mvccpb.KeyValue{
					Key: []byte(key),
				},
			})
		}
	}

	return &clientv3.DeleteResponse{}, nil
}

func (m *MockKV) Do(ctx context.Context, op clientv3.Op) (clientv3.OpResponse, error) {
	// Implement based on operation type
	return clientv3.OpResponse{}, nil
}

func (m *MockKV) Txn(ctx context.Context) clientv3.Txn {
	// Return a mock transaction
	return &MockTxn{kv: m}
}

func (m *MockKV) Compact(ctx context.Context, rev int64, opts ...clientv3.CompactOption) (*clientv3.CompactResponse, error) {
	return &clientv3.CompactResponse{}, nil
}

func (m *MockKV) addWatcher(w *MockWatcher) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchers = append(m.watchers, w)
}

// MockWatcher implements clientv3.Watcher for testing
type MockWatcher struct {
	kv          *MockKV
	events      chan clientv3.WatchResponse
	watchBuffer int
}

type MockWatcherOption func(*MockWatcher)

// WithWatchBuffer sets the buffer size for watch channels
func WithWatchBuffer(size int) MockWatcherOption {
	return func(w *MockWatcher) {
		w.watchBuffer = size
	}
}

func NewMockWatcher(kv *MockKV, opts ...MockWatcherOption) *MockWatcher {
	w := &MockWatcher{
		kv:          kv,
		events:      make(chan clientv3.WatchResponse, 10),
		watchBuffer: 10, // Default matches events channel size
	}
	for _, opt := range opts {
		opt(w)
	}
	kv.addWatcher(w)
	return w
}

func (m *MockWatcher) Watch(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
	ch := make(chan clientv3.WatchResponse, m.watchBuffer)
	op := clientv3.OpGet(key, opts...)
	watchKey := string(op.KeyBytes())

	go func() {
		for {
			select {
			case resp := <-m.events:
				// Filter events based on watch options
				for _, event := range resp.Events {
					eventKey := string(event.Kv.Key)
					if op.IsOptsWithPrefix() {
						if !strings.HasPrefix(eventKey, watchKey) {
							continue
						}
					} else if eventKey != watchKey {
						continue
					}
					ch <- clientv3.WatchResponse{Events: []*clientv3.Event{event}}
				}
			case <-ctx.Done():
				close(ch)
				return
			}
		}
	}()
	return ch
}

func (m *MockWatcher) RequestProgress(ctx context.Context) error {
	return nil
}

func (m *MockWatcher) Close() error {
	return nil
}

func (m *MockWatcher) notify(event *clientv3.Event) {
	m.events <- clientv3.WatchResponse{
		Events: []*clientv3.Event{event},
	}
}

// MockTxn implements clientv3.Txn for testing
type MockTxn struct {
	kv *MockKV
}

func (m *MockTxn) If(cs ...clientv3.Cmp) clientv3.Txn {
	return m
}

func (m *MockTxn) Then(ops ...clientv3.Op) clientv3.Txn {
	return m
}

func (m *MockTxn) Else(ops ...clientv3.Op) clientv3.Txn {
	return m
}

func (m *MockTxn) Commit() (*clientv3.TxnResponse, error) {
	return &clientv3.TxnResponse{}, nil
}

// MockEtcdClient implements EtcdClient for testing
type MockEtcdClient struct {
	endpoints []string
	closed    bool
	kv        *MockKV
	watcher   *MockWatcher
	mu        sync.RWMutex
}

func NewMockEtcdClient(endpoints []string) *MockEtcdClient {
	kv := NewMockKV()
	return &MockEtcdClient{
		endpoints: endpoints,
		kv:        kv,
		watcher:   NewMockWatcher(kv, WithWatchBuffer(10)),
	}
}

func (m *MockEtcdClient) Endpoints() []string {
	return m.endpoints
}

func (m *MockEtcdClient) Status(ctx context.Context, endpoint string) (*clientv3.StatusResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, fmt.Errorf("client closed")
	}
	return &clientv3.StatusResponse{}, nil
}

func (m *MockEtcdClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *MockEtcdClient) KV() clientv3.KV {
	return m.kv
}

func (m *MockEtcdClient) Watcher() clientv3.Watcher {
	return m.watcher
}
