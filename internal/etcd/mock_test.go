package etcd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestMockEtcdClient(t *testing.T) {
	t.Run("Endpoints", func(t *testing.T) {
		endpoints := []string{"localhost:2379"}
		client := NewMockEtcdClient(endpoints)
		assert.Equal(t, endpoints, client.Endpoints())
	})

	t.Run("Status", func(t *testing.T) {
		client := NewMockEtcdClient([]string{"localhost:2379"})
		resp, err := client.Status(context.Background(), "localhost:2379")
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Close", func(t *testing.T) {
		client := NewMockEtcdClient([]string{"localhost:2379"})
		err := client.Close()
		assert.NoError(t, err)
		_, err = client.Status(context.Background(), "localhost:2379")
		assert.Error(t, err)
	})
}

func TestMockKV(t *testing.T) {
	ctx := context.Background()
	mockKV := NewMockKV()

	t.Run("Put and Get", func(t *testing.T) {
		// Test basic put and get
		_, err := mockKV.Put(ctx, "key1", "value1")
		assert.NoError(t, err)

		resp, err := mockKV.Get(ctx, "key1")
		assert.NoError(t, err)
		require.Len(t, resp.Kvs, 1)
		assert.Equal(t, "key1", string(resp.Kvs[0].Key))
		assert.Equal(t, "value1", string(resp.Kvs[0].Value))

		// Test get non-existent key
		resp, err = mockKV.Get(ctx, "nonexistent")
		assert.NoError(t, err)
		assert.Empty(t, resp.Kvs)
	})

	t.Run("Delete", func(t *testing.T) {
		// Setup
		_, err := mockKV.Put(ctx, "key2", "value2")
		assert.NoError(t, err)

		// Test delete
		_, err = mockKV.Delete(ctx, "key2")
		assert.NoError(t, err)

		// Verify deleted
		resp, err := mockKV.Get(ctx, "key2")
		assert.NoError(t, err)
		assert.Empty(t, resp.Kvs)
	})

	t.Run("Watch notifications", func(t *testing.T) {
		mockWatcher := NewMockWatcher(mockKV)
		watchChan := mockWatcher.Watch(ctx, "watchkey")

		// Test Put notification
		_, err := mockKV.Put(ctx, "watchkey", "watchvalue")
		assert.NoError(t, err)

		select {
		case resp := <-watchChan:
			require.Len(t, resp.Events, 1)
			event := resp.Events[0]
			assert.Equal(t, clientv3.EventTypePut, event.Type)
			assert.Equal(t, "watchkey", string(event.Kv.Key))
			assert.Equal(t, "watchvalue", string(event.Kv.Value))
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for watch notification")
		}

		// Test Delete notification
		_, err = mockKV.Delete(ctx, "watchkey")
		assert.NoError(t, err)

		select {
		case resp := <-watchChan:
			require.Len(t, resp.Events, 1)
			event := resp.Events[0]
			assert.Equal(t, clientv3.EventTypeDelete, event.Type)
			assert.Equal(t, "watchkey", string(event.Kv.Key))
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for watch notification")
		}
	})

	t.Run("Watch with prefix", func(t *testing.T) {
		mockWatcher := NewMockWatcher(mockKV)
		watchChan := mockWatcher.Watch(ctx, "prefix/", clientv3.WithPrefix())

		// Put keys with prefix
		_, err := mockKV.Put(ctx, "prefix/key1", "value1")
		assert.NoError(t, err)
		_, err = mockKV.Put(ctx, "prefix/key2", "value2")
		assert.NoError(t, err)

		// Should receive notifications for both keys
		received := 0
		for received < 2 {
			select {
			case resp := <-watchChan:
				require.Len(t, resp.Events, 1)
				assert.Contains(t, []string{"prefix/key1", "prefix/key2"}, string(resp.Events[0].Kv.Key))
				received++
			case <-time.After(1 * time.Second):
				t.Fatal("timeout waiting for watch notifications")
			}
		}
	})

	t.Run("Get with prefix", func(t *testing.T) {
		// Setup test data
		_, err := mockKV.Put(ctx, "prefix/key1", "value1")
		assert.NoError(t, err)
		_, err = mockKV.Put(ctx, "prefix/key2", "value2")
		assert.NoError(t, err)
		_, err = mockKV.Put(ctx, "other/key", "value3") // Should not be included
		assert.NoError(t, err)

		// Test Get with prefix
		resp, err := mockKV.Get(ctx, "prefix/", clientv3.WithPrefix())
		assert.NoError(t, err)
		assert.Len(t, resp.Kvs, 2)

		// Verify returned keys
		keys := make([]string, 0, len(resp.Kvs))
		for _, kv := range resp.Kvs {
			keys = append(keys, string(kv.Key))
		}
		assert.Contains(t, keys, "prefix/key1")
		assert.Contains(t, keys, "prefix/key2")
		assert.NotContains(t, keys, "other/key")
	})

	t.Run("Delete with prefix", func(t *testing.T) {
		// Setup test data
		_, err := mockKV.Put(ctx, "prefix/key1", "value1")
		assert.NoError(t, err)
		_, err = mockKV.Put(ctx, "prefix/key2", "value2")
		assert.NoError(t, err)
		_, err = mockKV.Put(ctx, "other/key", "value3") // Should not be deleted
		assert.NoError(t, err)

		// Test Delete with prefix
		_, err = mockKV.Delete(ctx, "prefix/", clientv3.WithPrefix())
		assert.NoError(t, err)

		// Verify deletion
		resp, err := mockKV.Get(ctx, "prefix/", clientv3.WithPrefix())
		assert.NoError(t, err)
		assert.Empty(t, resp.Kvs)

		// Verify other key still exists
		resp, err = mockKV.Get(ctx, "other/key")
		assert.NoError(t, err)
		assert.Len(t, resp.Kvs, 1)
		assert.Equal(t, "other/key", string(resp.Kvs[0].Key))
	})

	t.Run("Context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		mockWatcher := NewMockWatcher(mockKV)
		watchChan := mockWatcher.Watch(ctx, "cancelkey")

		// Cancel the context
		cancel()

		// Channel should close
		select {
		case _, ok := <-watchChan:
			assert.False(t, ok, "channel should be closed after context cancellation")
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for channel close")
		}
	})

	t.Run("Transaction", func(t *testing.T) {
		txn := mockKV.Txn(ctx)
		assert.NotNil(t, txn)

		// Mock transaction just returns success
		resp, err := txn.Then(
			clientv3.OpPut("txnkey", "txnvalue"),
		).Commit()
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Compact", func(t *testing.T) {
		resp, err := mockKV.Compact(ctx, 1)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestMockWatcher(t *testing.T) {
	ctx := context.Background()
	mockKV := NewMockKV()
	mockWatcher := NewMockWatcher(mockKV)

	t.Run("RequestProgress", func(t *testing.T) {
		err := mockWatcher.RequestProgress(ctx)
		assert.NoError(t, err)
	})

	t.Run("Close", func(t *testing.T) {
		err := mockWatcher.Close()
		assert.NoError(t, err)
	})

	t.Run("Multiple watchers", func(t *testing.T) {
		watcher1 := NewMockWatcher(mockKV)
		watcher2 := NewMockWatcher(mockKV)

		chan1 := watcher1.Watch(ctx, "multikey")
		chan2 := watcher2.Watch(ctx, "multikey")

		// Trigger event
		_, err := mockKV.Put(ctx, "multikey", "multivalue")
		assert.NoError(t, err)

		// Both watchers should receive the event
		select {
		case resp := <-chan1:
			require.Len(t, resp.Events, 1)
			assert.Equal(t, "multikey", string(resp.Events[0].Kv.Key))
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for watcher1 notification")
		}

		select {
		case resp := <-chan2:
			require.Len(t, resp.Events, 1)
			assert.Equal(t, "multikey", string(resp.Events[0].Kv.Key))
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for watcher2 notification")
		}
	})
}
