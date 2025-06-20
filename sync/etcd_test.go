package sync

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.lumeweb.com/configmanager/config"
	"go.lumeweb.com/configmanager/internal/etcd"
	"go.uber.org/zap"
	"testing"
)

func newMockEtcdManager(t *testing.T) etcd.EtcdManager {
	mockClient := etcd.NewMockEtcdClient([]string{"mock:2379"})
	mockManager, err := etcd.NewEtcdManager(&config.EtcdConfig{
		Endpoints:   []string{"mock:2379"},
		DialTimeout: 5,
	}, zap.NewNop(),
		etcd.WithClient(mockClient),
	)
	require.NoError(t, err)
	return mockManager
}

func TestNewEtcdSyncClient(t *testing.T) {
	mockManager := newMockEtcdManager(t)
	client := NewEtcdSyncClient(mockManager, "test", zap.NewNop())
	require.NotNil(t, client)
	assert.Equal(t, mockManager, client.manager)
	assert.Equal(t, "test", client.prefix)
}

func TestEtcdSyncClient_Start(t *testing.T) {
	t.Run("successful connection", func(t *testing.T) {
		mockManager := newMockEtcdManager(t)
		client := NewEtcdSyncClient(mockManager, "", zap.NewNop())
		err := client.Start(context.Background())
		assert.NoError(t, err)
	})

	t.Run("connection error", func(t *testing.T) {
		mockManager := newMockEtcdManager(t)
		err := mockManager.Close()
		require.NoError(t, err) // Make manager return error on status check

		client := NewEtcdSyncClient(mockManager, "", zap.NewNop())
		err = client.Start(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to connect to etcd")
	})
}

func TestEtcdSyncClient_Stop(t *testing.T) {
	mockManager := newMockEtcdManager(t)
	client := NewEtcdSyncClient(mockManager, "", zap.NewNop(), WithEtcdManager(mockManager))
	err := client.Stop()
	assert.NoError(t, err)
}

func TestEtcdSyncClient_Configure(t *testing.T) {
	t.Run("successful configuration", func(t *testing.T) {
		// Create a mock config manager that returns a valid etcd config
		mockConfigManager := &mockConfigManager{
			data: map[string]any{
				"sync.config": &config.EtcdConfig{
					Endpoints:   []string{"mock:2379"},
					DialTimeout: 5,
				},
			},
		}

		mockManager := newMockEtcdManager(t)
		mockManager2 := newMockEtcdManager(t)
		client := NewEtcdSyncClient(mockManager, "", zap.NewNop())

		// Test successful configuration with mock manager option
		err := client.Configure(mockConfigManager, "sync.config", WithSyncEtcdClientOption(WithEtcdManager(mockManager2)))
		assert.NoError(t, err)
		assert.Equal(t, "sync.config", client.configNS)
		assert.NotNil(t, client.manager)

		// Explicitly start after configuration
		err = client.Start(context.Background())
		assert.NoError(t, err)
	})

	t.Run("configuration with existing manager", func(t *testing.T) {
		mockConfigManager := &mockConfigManager{
			data: map[string]any{
				"sync.config": &config.EtcdConfig{
					Endpoints:   []string{"mock:2379"},
					DialTimeout: 5,
				},
			},
		}

		// Create initial mock manager
		initialManager := newMockEtcdManager(t)
		client := NewEtcdSyncClient(initialManager, "", zap.NewNop())

		// Configure with new manager via options
		mockManager := newMockEtcdManager(t)
		err := client.Configure(mockConfigManager, "sync.config", WithSyncEtcdClientOption(WithEtcdManager(mockManager)))
		assert.NoError(t, err)
		assert.Equal(t, mockManager, client.manager)

		// Explicitly start after configuration
		err = client.Start(context.Background())
		assert.NoError(t, err)
	})

	t.Run("configuration with invalid namespace", func(t *testing.T) {
		mockConfigManager := &mockConfigManager{
			data: make(map[string]any),
		}

		client := NewEtcdSyncClient(nil, "", zap.NewNop())
		err := client.Configure(mockConfigManager, "nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get etcd config from manager")
	})

	t.Run("configuration with manager cleanup", func(t *testing.T) {
		mockConfigManager := &mockConfigManager{
			data: map[string]any{
				"sync.config": &config.EtcdConfig{
					Endpoints:   []string{"mock:2379"},
					DialTimeout: 5,
				},
			},
		}

		// Create initial mock manager
		initialManager := newMockEtcdManager(t)
		client := NewEtcdSyncClient(initialManager, "", zap.NewNop())

		// Configure with new manager via options
		mockManager := newMockEtcdManager(t)
		err := client.Configure(mockConfigManager, "sync.config", WithSyncEtcdClientOption(WithEtcdManager(mockManager)))
		assert.NoError(t, err)
		assert.Equal(t, mockManager, client.manager)
	})
}

type mockEtcdManager struct {
	client etcd.EtcdClient
	delim  string
}

func (m *mockEtcdManager) Delim() string {
	if m.delim == "" {
		return "."
	}
	return m.delim
}

func (m *mockEtcdManager) Client() etcd.EtcdClient {
	return m.client
}

func (m *mockEtcdManager) KV() clientv3.KV {
	if m.client == nil {
		return nil
	}
	return m.client.KV()
}

func (m *mockEtcdManager) Watcher() clientv3.Watcher {
	if m.client == nil {
		return nil
	}
	return m.client.Watcher()
}

func (m *mockEtcdManager) Close() error {
	if m.client == nil {
		return nil
	}
	return m.client.Close()
}

type mockConfigManager struct {
	data  map[string]any
	delim string
}

func (m *mockConfigManager) Delim() string {
	if m.delim == "" {
		return "."
	}
	return m.delim
}

func (m *mockConfigManager) Get(key string, target ...any) (any, any, error) {
	val, ok := m.data[key]
	if !ok {
		return nil, nil, fmt.Errorf("key not found")
	}
	return val, nil, nil
}

func (m *mockConfigManager) All() map[string]any {
	if m.data == nil {
		m.data = make(map[string]any)
	}
	return m.data
}

func (m *mockConfigManager) Start(ctx context.Context) error { return nil }
func (m *mockConfigManager) Stop() error                     { return nil }
func (m *mockConfigManager) Push(ctx context.Context, key string, value any, callback PushCallback) error {
	return nil
}

func (m *mockConfigManager) Delete(key string) {
	delete(m.data, key)
}

func (m *mockConfigManager) Keys() []string {
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}

func (m *mockConfigManager) Set(ctx context.Context, key string, value any) error {
	if m.data == nil {
		m.data = make(map[string]any)
	}
	m.data[key] = value
	return nil
}

func TestEtcdSyncClient_fullKey(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		key      string
		expected string
	}{
		{
			name:     "empty prefix",
			prefix:   "",
			key:      "test.key",
			expected: "test.key",
		},
		{
			name:     "with prefix",
			prefix:   "myapp",
			key:      "config.key",
			expected: "myapp/config.key",
		},
		{
			name:     "empty key",
			prefix:   "myapp",
			key:      "",
			expected: "myapp/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &EtcdSyncClient{
				prefix: tt.prefix,
			}

			result := client.fullKey(tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}
