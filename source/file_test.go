package source

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileSourceWrapper_Load(t *testing.T) {
	t.Run("load valid file", func(t *testing.T) {
		tmpFile := createTempFile(t, "test.key: test_value\n")
		defer func() {
			if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
				t.Errorf("failed to remove temp file: %v", err)
			}
		}()

		f := NewFileSource(tmpFile).(*fileSourceWrapper)
		k := koanf.New(".")

		err := f.Load(context.Background(), k)
		require.NoError(t, err)
		assert.Equal(t, "test_value", k.String("test.key"))
	})

	t.Run("load empty file", func(t *testing.T) {
		tmpFile := createTempFile(t, "")
		defer func() {
			if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
				t.Errorf("failed to remove temp file: %v", err)
			}
		}()

		f := NewFileSource(tmpFile).(*fileSourceWrapper)
		k := koanf.New(".")

		err := f.Load(context.Background(), k)
		require.NoError(t, err)
		assert.Empty(t, k.Keys())
	})

	t.Run("load non-existent file", func(t *testing.T) {
		f := NewFileSource("nonexistent.yaml").(*fileSourceWrapper)
		k := koanf.New(".")

		err := f.Load(context.Background(), k)
		require.Error(t, err)
	})
}

func TestFileSourceWrapper_Watch(t *testing.T) {
	t.Run("detect file changes", func(t *testing.T) {
		tmpFile := createTempFile(t, `
test.key: initial
test.key2: initial
test.key3: initial
`)
		defer func() {
			if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
				t.Errorf("failed to remove temp file: %v", err)
			}
		}()

		f := NewFileSource(tmpFile).(*fileSourceWrapper)
		k := koanf.New(".")

		err := f.Load(context.Background(), k)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), k, func(changedKeys []string, err error) {
			changeChan <- changedKeys
		})
		require.NoError(t, err)
		defer stopWatcher(t, f)

		// Modify file - change only one key
		err = os.WriteFile(tmpFile, []byte(`
test.key: updated
test.key2: initial
test.key3: initial
`), 0644)
		require.NoError(t, err)

		select {
		case keys := <-changeChan:
			assert.Equal(t, []string{"test.key"}, keys)
			assert.Equal(t, "updated", k.String("test.key"))
			assert.Equal(t, "initial", k.String("test.key2"))
			assert.Equal(t, "initial", k.String("test.key3"))
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for change notification")
		}
	})

	t.Run("detect file deletion", func(t *testing.T) {
		tmpFile := createTempFile(t, "test.key: initial\n")
		defer func() {
			if _, err := os.Stat(tmpFile); err == nil {
				err := os.Remove(tmpFile)
				assert.NoError(t, err)
			}
		}()

		f := NewFileSource(tmpFile).(*fileSourceWrapper)
		k := koanf.New(".")

		err := f.Load(context.Background(), k)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), k, func(changedKeys []string, err error) {
			changeChan <- changedKeys
		})
		require.NoError(t, err)
		defer stopWatcher(t, f)

		// Delete file
		err = os.Remove(tmpFile)
		assert.NoError(t, err)

		select {
		case keys := <-changeChan:
			assert.Equal(t, AllChanges, keys)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for delete notification")
		}
	})

	t.Run("many changes trigger full reload", func(t *testing.T) {
		tmpFile := createTempFile(t, `
key1: v1
key2: v2 
key3: v3
key4: v4
`)
		defer func() {
			if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
				t.Errorf("failed to remove temp file: %v", err)
			}
		}()

		f := NewFileSource(tmpFile, WithChangedThreshold(0.5)).(*fileSourceWrapper)
		k := koanf.New(".")

		err := f.Load(context.Background(), k)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), k, func(changedKeys []string, err error) {
			changeChan <- changedKeys
		})
		require.NoError(t, err)
		defer stopWatcher(t, f)

		// Modify file with many changes
		err = os.WriteFile(tmpFile, []byte(`
key1: new1
key2: v2
key3: new3
key5: v5
`), 0644)
		require.NoError(t, err)

		select {
		case keys := <-changeChan:
			assert.Equal(t, AllChanges, keys)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for change notification")
		}
	})

	t.Run("few changes return specific keys", func(t *testing.T) {
		tmpFile := createTempFile(t, `
key1: v1
key2: v2
key3: v3
key4: v4
`)
		defer func() {
			if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
				t.Errorf("failed to remove temp file: %v", err)
			}
		}()

		f := NewFileSource(tmpFile, WithChangedThreshold(0.5)).(*fileSourceWrapper)
		k := koanf.New(".")

		err := f.Load(context.Background(), k)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), k, func(changedKeys []string, err error) {
			changeChan <- changedKeys
		})
		require.NoError(t, err)
		defer stopWatcher(t, f)

		// Modify file with few changes
		err = os.WriteFile(tmpFile, []byte(`
key1: new1
key2: v2
key3: v3
key4: v4
`), 0644)
		require.NoError(t, err)

		select {
		case keys := <-changeChan:
			assert.Equal(t, []string{"key1"}, keys)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for change notification")
		}
	})

	t.Run("new keys count as changes", func(t *testing.T) {
		tmpFile := createTempFile(t, "key1: v1\n")
		defer func(name string) {
			err := os.Remove(name)
			require.NoError(t, err)
		}(tmpFile)

		f := NewFileSource(tmpFile, WithChangedThreshold(1)).(*fileSourceWrapper)
		k := koanf.New(".")

		err := f.Load(context.Background(), k)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), k, func(changedKeys []string, err error) {
			changeChan <- changedKeys
		})
		require.NoError(t, err)
		defer stopWatcher(t, f)

		// Add new key
		err = os.WriteFile(tmpFile, []byte(`
key1: v1
key2: v2
`), 0644)
		require.NoError(t, err)

		select {
		case keys := <-changeChan:
			assert.Equal(t, []string{"key2"}, keys)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for change notification")
		}
	})

	t.Run("deleted keys count as changes", func(t *testing.T) {
		tmpFile := createTempFile(t, `
key1: v1
key2: v2
`)
		defer func() {
			if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
				t.Errorf("failed to remove temp file: %v", err)
			}
		}()

		f := NewFileSource(tmpFile, WithChangedThreshold(1)).(*fileSourceWrapper)
		k := koanf.New(".")

		err := f.Load(context.Background(), k)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), k, func(changedKeys []string, err error) {
			changeChan <- changedKeys
		})
		require.NoError(t, err)
		defer stopWatcher(t, f)

		// Remove key
		err = os.WriteFile(tmpFile, []byte("key1: v1\n"), 0644)
		require.NoError(t, err)

		select {
		case keys := <-changeChan:
			assert.Equal(t, []string{"key2"}, keys)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for change notification")
		}
	})

	t.Run("empty old state with new keys", func(t *testing.T) {
		tmpFile := createTempFile(t, "")
		defer func() {
			if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
				t.Errorf("failed to remove temp file: %v", err)
			}
		}()

		f := NewFileSource(tmpFile, WithChangedThreshold(0.5)).(*fileSourceWrapper)
		k := koanf.New(".")

		err := f.Load(context.Background(), k)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), k, func(changedKeys []string, err error) {
			changeChan <- changedKeys
		})
		require.NoError(t, err)
		defer stopWatcher(t, f)

		// Add keys to empty file
		err = os.WriteFile(tmpFile, []byte(`
key1: v1
key2: v2
`), 0644)
		require.NoError(t, err)

		select {
		case keys := <-changeChan:
			assert.Equal(t, AllChanges, keys)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for change notification")
		}
	})

	t.Run("no changes", func(t *testing.T) {
		tmpFile := createTempFile(t, "key: value\n")
		defer func() {
			if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
				t.Errorf("failed to remove temp file: %v", err)
			}
		}()

		f := NewFileSource(tmpFile, WithChangedThreshold(0.5)).(*fileSourceWrapper)
		k := koanf.New(".")

		err := f.Load(context.Background(), k)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), k, func(changedKeys []string, err error) {
			changeChan <- changedKeys
		})
		require.NoError(t, err)
		defer stopWatcher(t, f)

		// Write same content
		err = os.WriteFile(tmpFile, []byte("key: value\n"), 0644)
		require.NoError(t, err)

		select {
		case keys := <-changeChan:
			assert.Nil(t, keys)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for change notification")
		}
	})
}

func TestFileSourceWrapper_WithChangedThreshold(t *testing.T) {
	f := NewFileSource("").(*fileSourceWrapper)
	WithChangedThreshold(0.8)(f)
	assert.Equal(t, 0.8, f.changedThreshold)
}

// Helper functions
func createTempFile(t *testing.T, content string) string {
	tmpFile, err := os.CreateTemp("", "config_test_*.yaml")
	require.NoError(t, err)
	defer tmpFile.Close()

	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	return tmpFile.Name()
}

func stopWatcher(t *testing.T, f *fileSourceWrapper) {
	if stoppable, ok := any(f).(StoppableConfigSource); ok {
		err := stoppable.Stop()
		require.NoError(t, err)
	}
}
