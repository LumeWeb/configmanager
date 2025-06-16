package sync

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/event/v2"
	"go.uber.org/zap"
)

func TestManagerDefault(t *testing.T) {
	t.Run("NewManager", func(t *testing.T) {
		client := NewDummySyncClient()
		eventMgr := event.NewManager[ConfigEvent]("")
		logger := zap.NewNop()

		mgr := NewManager(client, eventMgr, logger)
		assert.NotNil(t, mgr)
		assert.Equal(t, client, mgr.client)
		assert.Equal(t, eventMgr, mgr.eventMgr)
		assert.Equal(t, logger, mgr.logger)
	})

	t.Run("Start", func(t *testing.T) {
		client := NewDummySyncClient()
		mgr := NewManager(client, nil, nil)

		err := mgr.Start(context.Background())
		assert.NoError(t, err)
	})

	t.Run("Stop", func(t *testing.T) {
		client := NewDummySyncClient()
		mgr := NewManager(client, nil, nil)

		err := mgr.Stop()
		assert.NoError(t, err)
	})

	t.Run("Push", func(t *testing.T) {
		client := NewDummySyncClient()
		eventMgr := event.NewManager[ConfigEvent]("")
		// Create logger with debug level for tests
		logger := zap.NewNop()
		mgr := NewManager(client, eventMgr, logger)

		var callbackCalled bool
		callback := func(key string, value any) {
			callbackCalled = true
			assert.Equal(t, "test.key", key)
			assert.Equal(t, "test_value", value)
		}

		err := mgr.Push(context.Background(), "test.key", "test_value", callback)
		assert.NoError(t, err)
		assert.True(t, callbackCalled)
		assert.Equal(t, "test_value", client.Get("test.key"))
	})

	t.Run("PushWithWatch", func(t *testing.T) {
		client := NewDummySyncClient()
		eventMgr := event.NewManager[ConfigEvent]("")
		mgr := NewManager(client, eventMgr, nil)

		// Push initial value - this will implicitly watch the key
		err := mgr.Push(context.Background(), "test.key", "initial_value", nil)
		require.NoError(t, err)

		// Verify the change was detected and event fired
		var eventFired bool
		eventMgr.AddListener("test.key", event.NewListenerFunc(func(e event.Event[ConfigEvent]) error {
			eventFired = true
			evt := e.Data()
			assert.Equal(t, "test.key", evt.key)
			assert.Equal(t, "external_change", evt.value)
			return nil
		}))

		// Simulate an external change by calling Push directly on the client
		// This will properly trigger the watch callback
		err = client.Push(context.Background(), "test.key", "external_change", nil)
		require.NoError(t, err)

		// Wait for event with timeout
		timeout := time.After(1 * time.Second)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		
		for {
			select {
			case <-timeout:
				assert.Fail(t, "Expected event to be fired for external change")
				return
			case <-ticker.C:
				if eventFired {
					return
				}
			}
		}
	})

	t.Run("PushLocalChange", func(t *testing.T) {
		client := NewDummySyncClient()
		eventMgr := event.NewManager[ConfigEvent]("")
		mgr := NewManager(client, eventMgr, nil)

		// First watch the key
		err := mgr.watchKey(context.Background(), "test.key")
		require.NoError(t, err)

		// Push a local change
		err = mgr.Push(context.Background(), "test.key", "local_change", nil)
		require.NoError(t, err)

		// Simulate an external change (should be ignored since it's local)
		var eventFired bool
		eventMgr.AddListener("test.key", event.NewListenerFunc(func(e event.Event[ConfigEvent]) error {
			eventFired = true
			return nil
		}))

		client.Set("test.key", "external_change")

		// Wait briefly to ensure no event was fired
		select {
		case <-time.After(100 * time.Millisecond):
			assert.False(t, eventFired, "Event should not fire for local changes")
		}
	})

	t.Run("WatchKey", func(t *testing.T) {
		client := NewDummySyncClient()
		eventMgr := event.NewManager[ConfigEvent]("")
		mgr := NewManager(client, eventMgr, nil)

		err := mgr.watchKey(context.Background(), "test.key")
		assert.NoError(t, err)

		// Verify key is now watched
		mgr.watchMu.RLock()
		_, exists := mgr.watchedKeys["test.key"]
		mgr.watchMu.RUnlock()
		assert.True(t, exists)
	})

	t.Run("CleanupStaleChanges", func(t *testing.T) {
		client := NewDummySyncClient()
		mgr := NewManager(client, nil, nil)
		mgr.timeout = 10 * time.Millisecond // Short timeout for test

		// Add a stale change
		mgr.localChanges.Store("stale.key", time.Now().Add(-20*time.Millisecond))

		// Wait for cleanup using channel notification
		cleanupCh := make(chan struct{})
		go func() {
			for {
				_, exists := mgr.localChanges.Load("stale.key")
				if !exists {
					close(cleanupCh)
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()

		select {
		case <-cleanupCh:
			// Cleanup completed successfully
		case <-time.After(1 * time.Second):
			assert.Fail(t, "stale.key was not cleaned up")
		}
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		client := NewDummySyncClient()
		eventMgr := event.NewManager[ConfigEvent]("")
		mgr := NewManager(client, eventMgr, nil)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := mgr.Start(ctx)
		assert.NoError(t, err)

		err = mgr.Push(ctx, "test.key", "value", nil)
		assert.NoError(t, err)

		err = mgr.Stop()
		assert.NoError(t, err)
	})
}
