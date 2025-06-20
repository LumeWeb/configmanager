package sync

import (
	"context"
	"go.uber.org/zap"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDummySyncClient(t *testing.T) {
	logger := zap.NewNop()

	t.Run("NewDummySyncClient", func(t *testing.T) {
		client := NewDummySyncClient().WithLogger(logger)
		assert.NotNil(t, client)
		assert.NotNil(t, client.data)
		assert.Empty(t, client.data)
	})

	t.Run("Start", func(t *testing.T) {
		client := NewDummySyncClient()
		err := client.Start(context.Background())
		assert.NoError(t, err)
	})

	t.Run("Stop", func(t *testing.T) {
		client := NewDummySyncClient()
		err := client.Stop()
		assert.NoError(t, err)
	})

	t.Run("Push", func(t *testing.T) {
		client := NewDummySyncClient()
		var callbackCalled bool
		var callbackKey string
		var callbackValue any

		callback := func(key string, value any) {
			callbackCalled = true
			callbackKey = key
			callbackValue = value
		}

		// Test pushing a value
		err := client.Push(context.Background(), "test.key", "test_value", callback)
		assert.NoError(t, err)

		// Verify data was stored
		assert.Equal(t, "test_value", client.Get("test.key"))

		// Verify callback was called
		assert.True(t, callbackCalled)
		assert.Equal(t, "test.key", callbackKey)
		assert.Equal(t, "test_value", callbackValue)

		// Test pushing without callback
		callbackCalled = false
		err = client.Push(context.Background(), "test.key2", 42, nil)
		assert.NoError(t, err)
		assert.False(t, callbackCalled)
		assert.Equal(t, 42, client.Get("test.key2"))
	})

	t.Run("Watch", func(t *testing.T) {
		client := NewDummySyncClient().WithLogger(logger)
		var watchCalled bool
		var watchKey string
		var watchValue any

		// Watch should register the callback
		err := client.Watch(context.Background(), "test.key", func(key string, value any) {
			watchCalled = true
			watchKey = key
			watchValue = value
		})
		assert.NoError(t, err)

		// Push should trigger the watch callback
		err = client.Push(context.Background(), "test.key", "test_value", nil)
		assert.NoError(t, err)

		// Verify callback was called with correct values
		assert.True(t, watchCalled)
		assert.Equal(t, "test.key", watchKey)
		assert.Equal(t, "test_value", watchValue)

		// Test with different value
		watchCalled = false
		err = client.Push(context.Background(), "test.key", 42, nil)
		assert.NoError(t, err)
		assert.True(t, watchCalled)
		assert.Equal(t, "test.key", watchKey)
		assert.Equal(t, 42, watchValue)
	})

	t.Run("WatchOnlyCalledOnChange", func(t *testing.T) {
		client := NewDummySyncClient()
		var callCount int

		err := client.Watch(context.Background(), "test.key", func(key string, value any) {
			callCount++
		})
		assert.NoError(t, err)

		// First push - should trigger callback
		err = client.Push(context.Background(), "test.key", "value1", nil)
		assert.NoError(t, err)
		assert.Equal(t, 1, callCount)

		// Push same value - should not trigger callback
		err = client.Push(context.Background(), "test.key", "value1", nil)
		assert.NoError(t, err)
		assert.Equal(t, 1, callCount)

		// Push different value - should trigger callback
		err = client.Push(context.Background(), "test.key", "value2", nil)
		assert.NoError(t, err)
		assert.Equal(t, 2, callCount)
	})

	t.Run("Get", func(t *testing.T) {
		client := NewDummySyncClient()
		client.data["existing"] = "value"

		// Test getting existing value
		assert.Equal(t, "value", client.Get("existing"))

		// Test getting non-existent value
		assert.Nil(t, client.Get("nonexistent"))
	})

	t.Run("Set", func(t *testing.T) {
		client := NewDummySyncClient()

		// Set new value
		client.Set("test.key", "test_value")
		assert.Equal(t, "test_value", client.data["test.key"])

		// Overwrite existing value
		client.Set("test.key", 42)
		assert.Equal(t, 42, client.data["test.key"])
	})

	t.Run("Reset", func(t *testing.T) {
		client := NewDummySyncClient()
		client.data["key1"] = "value1"
		client.data["key2"] = "value2"

		client.Reset()
		assert.Empty(t, client.data)
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		client := NewDummySyncClient()
		done := make(chan bool)

		go func() {
			for i := 0; i < 100; i++ {
				client.Set("key1", i)
			}
			done <- true
		}()

		go func() {
			for i := 0; i < 100; i++ {
				client.Get("key1")
			}
			done <- true
		}()

		<-done
		<-done
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		client := NewDummySyncClient()
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()

		// These should all work fine with cancelled context
		err := client.Start(ctx)
		assert.NoError(t, err)

		err = client.Push(ctx, "test.key", "value", nil)
		assert.NoError(t, err)

		err = client.Watch(ctx, "test.key", func(string, any) {})
		assert.NoError(t, err)

		err = client.Stop()
		assert.NoError(t, err)
	})
}

func TestDummySyncClient_Configure(t *testing.T) {
	client := NewDummySyncClient()

	// Create a mock manager
	mockManager := &mockManager{}

	err := client.Configure(mockManager, "sync.config")
	assert.NoError(t, err)
	assert.Equal(t, mockManager, client.manager)
	assert.Equal(t, "sync.config", client.configNS)
}

type mockManager struct {
	data map[string]any
}

func (m *mockManager) Start(ctx context.Context) error { return nil }
func (m *mockManager) Stop() error                     { return nil }
func (m *mockManager) Push(ctx context.Context, key string, value any, callback PushCallback) error {
	return nil
}
func (m *mockManager) Configure(manager configManager, namespace string) error {
	return nil
}
func (m *mockManager) Get(key string, target ...any) (any, any, error) {
	return nil, nil, nil
}
func (m *mockManager) All() map[string]any {
	if m.data == nil {
		m.data = make(map[string]any)
	}
	return m.data
}
func (m *mockManager) Delete(key string) {
	delete(m.data, key)
}
func (m *mockManager) Delim() string {
	return "."
}
func (m *mockManager) Keys() []string {
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}
func (m *mockManager) Set(ctx context.Context, key string, value any) error {
	if m.data == nil {
		m.data = make(map[string]any)
	}
	m.data[key] = value
	return nil
}
