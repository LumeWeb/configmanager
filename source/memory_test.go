package source

import (
	"context"
	"testing"
	"time"

	"github.com/knadh/koanf/v2"
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
		k := koanf.New(".")
		err := src.Load(context.Background(), k)
		assert.NoError(t, err)

		assert.Equal(t, "value", k.Get("test.key"))
		assert.Equal(t, 42, k.Get("test.num"))
		assert.Equal(t, true, k.Get("test.bool"))
	})

	t.Run("Set and Load new data", func(t *testing.T) {
		src.Set("new.key", "new value")
		k := koanf.New(".")
		err := src.Load(context.Background(), k)
		assert.NoError(t, err)

		assert.Equal(t, "new value", k.Get("new.key"))
	})

	t.Run("Delete key", func(t *testing.T) {
		src.Delete("test.key")
		k := koanf.New(".")
		err := src.Load(context.Background(), k)
		assert.NoError(t, err)

		assert.Nil(t, k.Get("test.key"))
	})

	t.Run("Clear all data", func(t *testing.T) {
		src.Clear()
		k := koanf.New(".")
		err := src.Load(context.Background(), k)
		assert.NoError(t, err)

		assert.Empty(t, k.All())
	})

	t.Run("Watch notifications", func(t *testing.T) {
		k := koanf.New(".")
		changeChan := make(chan []string, 5) // Buffer for multiple notifications

		err := src.Watch(context.Background(), k, func(changedKeys []string, err error) {
			changeChan <- changedKeys
		})
		assert.NoError(t, err)

		// Set a new value
		src.Set("watch.test", "value")

		// Wait for first notification
		select {
		case changedKeys := <-changeChan:
			assert.Equal(t, []string{"watch.test"}, changedKeys)
			assert.Equal(t, "value", k.Get("watch.test"))
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
		src.Delete("watch.test2")
		
		// Wait for fourth notification
		select {
		case changedKeys := <-changeChan:
			assert.Equal(t, []string{"watch.test2"}, changedKeys)
			assert.Equal(t, 42, k.Get("watch.test2"))
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for delete watch notification")
		}

		// Test clear notification
		src.Clear()
		
		// Wait for fifth notification
		select {
		case changedKeys := <-changeChan:
			assert.ElementsMatch(t, []string{"watch.test", "watch.test3"}, changedKeys)
			assert.Equal(t, map[string]any{}, k.All())
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for clear watch notification")
		}

		// Clean up by closing the channel
		close(changeChan)
	})

	t.Run("Persist does nothing", func(t *testing.T) {
		err := src.Persist(context.Background(), koanf.New("."))
		assert.NoError(t, err)
	})
}
