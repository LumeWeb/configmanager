package source

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/configmanager/config"
	"go.lumeweb.com/configmanager/internal/etcd"
	"go.uber.org/zap"
)

type etcdTestFixture struct {
	ctx         context.Context
	logger      *zap.Logger
	mockKV      *etcd.MockKV
	mockWatcher *etcd.MockWatcher
	etcdManager etcd.EtcdManager
	source      *EtcdConfigSource
}

func setupEtcdTest(t *testing.T, initialData map[string]string) *etcdTestFixture {
	ctx := context.Background()
	logger := zap.NewNop()

	// Create mock etcd client
	mockClient := etcd.NewMockEtcdClient([]string{"mock"})

	// Create EtcdManager in mock mode
	etcdConfig := &config.EtcdConfig{
		Endpoints:   []string{"mock"}, // not used in mock mode
		DialTimeout: 5,
		Prefix:      "config",
	}

	// Create mock etcd manager with mock client
	etcdManager, err := etcd.NewEtcdManager(etcdConfig, logger,
		etcd.WithClient(mockClient))
	require.NoError(t, err)
	t.Cleanup(func() {
		err := etcdManager.Close()
		require.NoError(t, err)
	})

	// Get the mock KV client from manager
	mockKV := etcdManager.KV().(*etcd.MockKV)

	// Pre-populate mock etcd with test data if provided
	if initialData != nil {
		for k, v := range initialData {
			_, err = mockKV.Put(ctx, k, v)
			require.NoError(t, err)
		}
	}

	// Create etcd config source with mock manager
	source, err := NewEtcdConfigSource(etcdConfig, WithEtcdSourceEtcdManager(etcdManager), WithEtcdSourceLogger(logger))
	require.NoError(t, err)

	return &etcdTestFixture{
		ctx:         ctx,
		logger:      logger,
		mockKV:      etcdManager.KV().(*etcd.MockKV),
		mockWatcher: etcdManager.Watcher().(*etcd.MockWatcher),
		etcdManager: etcdManager,
		source:      source,
	}
}

func TestEtcdConfigSource(t *testing.T) {
	t.Run("Load", func(t *testing.T) {
		f := setupEtcdTest(t, map[string]string{
			"config/key1":   `"value1"`,
			"config/key2":   `42`,
			"config/nested": `{"subkey":"subvalue"}`,
		})

		k := koanf.New(".")
		err := f.source.Load(f.ctx, k)
		require.NoError(t, err)

		// Verify loaded values
		assert.Equal(t, "value1", k.String("key1"))
		assert.Equal(t, 42, k.Int("key2"))
		assert.Equal(t, "subvalue", k.String("nested.subkey"))
	})

	t.Run("Watch", func(t *testing.T) {
		f := setupEtcdTest(t, nil) // No initial data needed for this test

		k := koanf.New(".")
		changeChan := make(chan []string, 1)

		// Start watching
		err := f.source.Watch(f.ctx, k, func(changedKeys []string, err error) {
			changeChan <- changedKeys
		})
		require.NoError(t, err)

		// Simulate etcd change
		newValue := map[string]any{"newkey": "newvalue"}
		jsonVal, err := json.Marshal(newValue)
		require.NoError(t, err)
		_, err = f.mockKV.Put(f.ctx, "config/changed", string(jsonVal))
		require.NoError(t, err)

		// Wait for change notification
		select {
		case changedKeys := <-changeChan:
			assert.Equal(t, []string{"changed"}, changedKeys)
			assert.Equal(t, "newvalue", k.String("changed.newkey"))

		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for watch notification")
		}
	})

	t.Run("Persist", func(t *testing.T) {
		f := setupEtcdTest(t, nil) // No initial data needed for this test

		k := koanf.New(".")
		err := k.Set("key.to.persist", "persisted-value")
		require.NoError(t, err)

		err = f.source.Persist(f.ctx, k, "key.to.persist")
		require.NoError(t, err)

		// Verify value was persisted to etcd
		resp, err := f.mockKV.Get(f.ctx, "config/key.to.persist")
		require.NoError(t, err)
		require.Len(t, resp.Kvs, 1)

		var decodedVal string
		err = json.Unmarshal(resp.Kvs[0].Value, &decodedVal)
		require.NoError(t, err)

		assert.Equal(t, "persisted-value", decodedVal)
	})

	t.Run("Stop", func(t *testing.T) {
		f := setupEtcdTest(t, nil) // No initial data needed for this test
		err := f.source.Stop()
		require.NoError(t, err)
	})
}
