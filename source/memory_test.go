package source

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMemoryConfigSource(t *testing.T) {
	initialData := map[string]any{
		"test.key":  "value",
		"test.num":  42,
		"test.bool": true,
	}

	src := NewMemoryConfigSource(initialData)

	t.Run("Load initial data", func(t *testing.T) {
		mgr := newMockManager()
		err := src.Load(context.Background(), mgr)
		assert.NoError(t, err)

		mgr.assertValue(t, "test.key", "value")
		mgr.assertValue(t, "test.num", 42)
		mgr.assertValue(t, "test.bool", true)
		
		// Verify BulkSetAtomic was called
		assert.Greater(t, len(mgr.setCalls), 0, "expected BulkSetAtomic to be called")
	})

	t.Run("Set and Load new data", func(t *testing.T) {
		src.Set("new.key", "new value")
		mgr := newMockManager()
		err := src.Load(context.Background(), mgr)
		assert.NoError(t, err)

		val, _, err := mgr.Get("new.key")
		require.NoError(t, err)
		assert.Equal(t, "new value", val)
	})

	t.Run("Delete key", func(t *testing.T) {
		src.Delete("test.key")
		mgr := newMockManager()
		err := src.Load(context.Background(), mgr)
		assert.NoError(t, err)

		val, _, err := mgr.Get("test.key")
		require.Error(t, err)
		assert.Nil(t, val)
	})

	t.Run("Clear all data", func(t *testing.T) {
		src.Clear()
		mgr := newMockManager()
		err := src.Load(context.Background(), mgr)
		assert.NoError(t, err)

		assert.Empty(t, mgr.All())
	})

	t.Run("Watch notifications", func(t *testing.T) {
		mgr := newMockManager()
		changeChan := make(chan []string, 5) // Buffer for multiple notifications

		err := src.Watch(context.Background(), mgr, func(changedKeys []string, err error) {
			changeChan <- changedKeys
		})
		assert.NoError(t, err)

		// Set a new value
		src.Set("watch.test", "value")

		// Wait for first notification
		select {
		case changedKeys := <-changeChan:
			assert.Equal(t, []string{"watch.test"}, changedKeys)
			val, _, err := mgr.Get("watch.test")
			require.NoError(t, err)
			assert.Equal(t, "value", val)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for first watch notification")
		}

		// Test multiple changes
		src.Set("watch.test2", 42)
		src.Set("watch.test3", true)

		// Wait for second notification
		select {
		case changedKeys := <-changeChan:
			assert.Equal(t, []string{"watch.test2"}, changedKeys)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for second watch notification")
		}

		// Wait for third notification
		select {
		case changedKeys := <-changeChan:
			assert.Equal(t, []string{"watch.test3"}, changedKeys)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for third watch notification")
		}

		// Test delete notification
		src.DeleteFromManager("watch.test2", mgr)

		// Wait for fourth notification
		select {
		case changedKeys := <-changeChan:
			assert.Equal(t, []string{"watch.test2"}, changedKeys)
			val, _, err := mgr.Get("watch.test2")
			require.Error(t, err)
			assert.Nil(t, val)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for delete watch notification")
		}

		// Test clear notification
		src.Clear()

		// Wait for fifth notification
		select {
		case changedKeys := <-changeChan:
			assert.ElementsMatch(t, []string{"watch.test", "watch.test3"}, changedKeys)
			assert.Equal(t, map[string]any{}, mgr.All())
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for clear watch notification")
		}

		// Clean up by closing the channel
		close(changeChan)
	})

	t.Run("Persist does nothing", func(t *testing.T) {
		mgr := newMockManager()
		err := src.Persist(context.Background(), mgr)
		assert.NoError(t, err)
	})
}
