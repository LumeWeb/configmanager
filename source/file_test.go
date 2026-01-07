package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestFileSource_Load(t *testing.T) {
	t.Run("load valid file", func(t *testing.T) {
		tmpFile := createTempFile(t, "test.key: test_value\n")
		defer func() {
			if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
				t.Errorf("failed to remove temp file: %v", err)
			}
		}()

		f := NewFileSource(tmpFile).(*fileSource)
		mgr := newMockManager()

		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)
		mgr.assertValue(t, "test.key", "test_value")
		// Verify BulkSetAtomic was called
		assert.Contains(t, mgr.setCalls, "BulkSetAtomic", "expected BulkSetAtomic to be called")
	})

	t.Run("load empty file", func(t *testing.T) {
		tmpFile := createTempFile(t, "")
		defer func() {
			if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
				t.Errorf("failed to remove temp file: %v", err)
			}
		}()

		f := NewFileSource(tmpFile).(*fileSource)
		mgr := newMockManager()

		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)
		assert.Empty(t, mgr.Keys())
		// Verify BulkSetAtomic was called even for empty file
		assert.Contains(t, mgr.setCalls, "BulkSetAtomic", "expected BulkSetAtomic to be called")
	})

	t.Run("load non-existent file", func(t *testing.T) {
		f := NewFileSource("nonexistent.yaml").(*fileSource)
		mgr := newMockManager()

		err := f.Load(context.Background(), mgr)
		require.Error(t, err)
	})
}

func TestFileSource_Watch(t *testing.T) {
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

		f := NewFileSource(tmpFile).(*fileSource)
		mgr := newMockManager()

		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), mgr, func(changedKeys []string, err error) {
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
			val1, _, err1 := mgr.Get("test.key")
			val2, _, err2 := mgr.Get("test.key2")
			val3, _, err3 := mgr.Get("test.key3")
			require.NoError(t, err1)
			require.NoError(t, err2)
			require.NoError(t, err3)
			assert.Equal(t, "updated", val1)
			assert.Equal(t, "initial", val2)
			assert.Equal(t, "initial", val3)
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

		f := NewFileSource(tmpFile).(*fileSource)
		mgr := newMockManager()

		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), mgr, func(changedKeys []string, err error) {
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

		f := NewFileSource(tmpFile, WithChangedThreshold(0.5)).(*fileSource)
		mgr := newMockManager()

		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), mgr, func(changedKeys []string, err error) {
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

		f := NewFileSource(tmpFile, WithChangedThreshold(0.5)).(*fileSource)
		mgr := newMockManager()

		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), mgr, func(changedKeys []string, err error) {
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

		f := NewFileSource(tmpFile, WithChangedThreshold(1)).(*fileSource)
		mgr := newMockManager()

		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), mgr, func(changedKeys []string, err error) {
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

		f := NewFileSource(tmpFile, WithChangedThreshold(1)).(*fileSource)
		mgr := newMockManager()

		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), mgr, func(changedKeys []string, err error) {
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

		f := NewFileSource(tmpFile, WithChangedThreshold(0.5)).(*fileSource)
		mgr := newMockManager()

		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), mgr, func(changedKeys []string, err error) {
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

		f := NewFileSource(tmpFile, WithChangedThreshold(0.5)).(*fileSource)
		mgr := newMockManager()

		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		err = f.Watch(context.Background(), mgr, func(changedKeys []string, err error) {
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

	t.Run("watch with parse error", func(t *testing.T) {
		tmpFile := createTempFile(t, "key: valid\n")
		defer os.Remove(tmpFile)

		f := NewFileSource(tmpFile).(*fileSource)
		mgr := newMockManager()

		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)

		changeChan := make(chan []string, 1)
		errChan := make(chan error, 1)
		err = f.Watch(context.Background(), mgr, func(changedKeys []string, err error) {
			changeChan <- changedKeys
			errChan <- err
		})
		require.NoError(t, err)
		defer stopWatcher(t, f)

		// Write invalid YAML
		err = os.WriteFile(tmpFile, []byte("invalid: yaml: content:\nbroken"), 0644)
		require.NoError(t, err)

		select {
		case err := <-errChan:
			assert.Error(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for error notification")
		}
	})

	t.Run("watch with set error", func(t *testing.T) {
		tmpFile := createTempFile(t, "key: initial\n")
		defer os.Remove(tmpFile)

		f := NewFileSource(tmpFile).(*fileSource)
		mgr := newMockManager()
		mgr.shouldFailOnSet = true

		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)

		errChan := make(chan error, 1)
		err = f.Watch(context.Background(), mgr, func(changedKeys []string, err error) {
			errChan <- err
		})
		require.NoError(t, err)
		defer stopWatcher(t, f)

		// Modify file
		err = os.WriteFile(tmpFile, []byte("key: updated\n"), 0644)
		require.NoError(t, err)

		select {
		case err := <-errChan:
			assert.Error(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for error notification")
		}
	})
}

func TestFileSource_WithChangedThreshold(t *testing.T) {
	f := NewFileSource("").(*fileSource)
	WithChangedThreshold(0.8)(f)
	assert.Equal(t, 0.8, f.changedThreshold)
}

func TestFileSource_WithFileSourceLogger(t *testing.T) {
	t.Run("sets custom logger", func(t *testing.T) {
		logger := zap.NewExample()
		f := NewFileSource("", WithFileSourceLogger(logger)).(*fileSource)
		assert.Equal(t, logger, f.logger)
	})

	t.Run("default logger is no-op", func(t *testing.T) {
		f := NewFileSource("").(*fileSource)
		assert.NotNil(t, f.logger)
	})
}

func TestFileSource_Load_WithLogging(t *testing.T) {
	t.Run("logs changes on subsequent load", func(t *testing.T) {
		tmpFile := createTempFile(t, "key1: value1\n")
		defer os.Remove(tmpFile)

		// Create a logger that we can use
		logger := zap.NewNop()
		f := NewFileSource(tmpFile, WithFileSourceLogger(logger)).(*fileSource)
		mgr := newMockManager()

		// First load - no changes logged (initial load)
		err := f.Load(context.Background(), mgr)
		require.NoError(t, err)

		// Verify initial state
		val1, _, _ := mgr.Get("key1")
		assert.Equal(t, "value1", val1)

		// Modify file
		err = os.WriteFile(tmpFile, []byte("key1: updated\nkey2: value2\n"), 0644)
		require.NoError(t, err)

		// Second load - changes should be logged (this exercises the logging code path)
		err = f.Load(context.Background(), mgr)
		require.NoError(t, err)

		// Verify values were updated
		val1, _, _ = mgr.Get("key1")
		val2, _, _ := mgr.Get("key2")
		assert.Equal(t, "updated", val1)
		assert.Equal(t, "value2", val2)
	})
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

func stopWatcher(t *testing.T, f *fileSource) {
	if stoppable, ok := any(f).(StoppableConfigSource); ok {
		err := stoppable.Stop()
		require.NoError(t, err)
	}
}

func TestFileSource_Persist(t *testing.T) {
	t.Run("persist all keys", func(t *testing.T) {
		tmpFile := createTempFile(t, "")
		defer os.Remove(tmpFile)

		f := NewFileSource(tmpFile).(*fileSource)
		mgr := newMockManager()
		err := mgr.BulkSetAtomic(context.Background(), map[string]any{
			"key1": "value1",
			"key2": "value2",
		})
		require.NoError(t, err)

		err = f.Persist(mgr, "")
		require.NoError(t, err)

		// Verify file contents
		data, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		assert.Contains(t, string(data), "key1: value1")
		assert.Contains(t, string(data), "key2: value2")
	})

	t.Run("persist with key prefix", func(t *testing.T) {
		tmpFile := createTempFile(t, "")
		defer os.Remove(tmpFile)

		f := NewFileSource(tmpFile).(*fileSource)
		mgr := newMockManager()
		// First verify the manager has the expected keys
		err := mgr.BulkSetAtomic(context.Background(), map[string]any{
			"prefix1.key1": "value1",
			"prefix1.key2": "value2",
			"prefix2.key1": "value3",
		})
		require.NoError(t, err)
		require.True(t, mgr.Exists("prefix1.key1"))
		require.True(t, mgr.Exists("prefix1.key2"))
		require.True(t, mgr.Exists("prefix2.key1"))

		// Persist specific keys with namespace "prefix1" and unqualified keys
		err = f.Persist(mgr, "prefix1", "key1", "key2")
		require.NoError(t, err)

		// Verify file contents contains keys without namespace prefix
		data, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		assert.Contains(t, string(data), "key1: value1")
		assert.Contains(t, string(data), "key2: value2")
		assert.NotContains(t, string(data), "prefix1.key1") // Namespace should be stripped
		assert.NotContains(t, string(data), "prefix2.key1") // Other namespace not present
	})

	t.Run("persist with multiple prefixes - keys may overwrite", func(t *testing.T) {
		tmpFile := createTempFile(t, "")
		defer os.Remove(tmpFile)

		f := NewFileSource(tmpFile).(*fileSource)
		mgr := newMockManager()
		err := mgr.BulkSetAtomic(context.Background(), map[string]any{
			"prefix1.key1": "value1",
			"prefix2.key1": "value2", // Same key name under different prefix
			"prefix3.key1": "value3",
		})
		require.NoError(t, err)

		err = f.Persist(mgr, "", "prefix1.key1", "prefix2.key1")
		require.NoError(t, err)

		// Verify file contents - should include keys without namespace prefix
		data, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		assert.Contains(t, string(data), "key1: value1") // prefix1 stripped
		assert.Contains(t, string(data), "key1: value2") // prefix2 stripped
		assert.NotContains(t, string(data), "prefix3.key1")
	})

	t.Run("persist empty config", func(t *testing.T) {
		tmpFile := createTempFile(t, "")
		defer os.Remove(tmpFile)

		f := NewFileSource(tmpFile).(*fileSource)
		mgr := newMockManager()

		err := f.Persist(mgr, "")
		require.NoError(t, err)

		// Verify file is empty
		data, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		assert.Equal(t, "{}\n", string(data))
	})

	t.Run("error creating temp file", func(t *testing.T) {
		// Create a read-only directory
		tmpDir, err := os.MkdirTemp("", "testdir")
		require.NoError(t, err)
		defer func() {
			// Restore writable permissions before cleanup
			os.Chmod(tmpDir, 0755)
			os.RemoveAll(tmpDir)
		}()
		os.Chmod(tmpDir, 0555) // Read and execute only

		f := NewFileSource(filepath.Join(tmpDir, "config.yaml")).(*fileSource)
		mgr := newMockManager()
		err = mgr.BulkSetAtomic(context.Background(), map[string]any{"key": "value"})
		require.NoError(t, err)

		err = f.Persist(mgr, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create temporary file")
	})

	t.Run("error writing to temp file - unsupported type", func(t *testing.T) {
		tmpFile := createTempFile(t, "")
		defer os.Remove(tmpFile)

		f := NewFileSource(tmpFile).(*fileSource)
		mgr := newMockManager()
		// Use a channel which can't be marshaled to YAML
		err := mgr.BulkSetAtomic(context.Background(), map[string]any{"key": make(chan int)})
		require.NoError(t, err)

		err = f.Persist(mgr, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot persist config")
	})

	t.Run("persist with nested structures", func(t *testing.T) {
		tmpFile := createTempFile(t, "")
		defer os.Remove(tmpFile)

		f := NewFileSource(tmpFile).(*fileSource)
		mgr := newMockManager()
		err := mgr.BulkSetAtomic(context.Background(), map[string]any{
			"database": map[string]any{
				"host":     "localhost",
				"port":     5432,
				"username": "admin",
			},
			"features": []any{"feature1", "feature2"},
		})
		require.NoError(t, err)

		err = f.Persist(mgr, "")
		require.NoError(t, err)

		data, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		assert.Contains(t, string(data), "host: localhost")
		assert.Contains(t, string(data), "port: 5432")
		assert.Contains(t, string(data), "- feature1")
		assert.Contains(t, string(data), "- feature2")
	})

	t.Run("error when renaming temp file", func(t *testing.T) {
		// Create a directory at the destination path to make rename fail
		// (os.Rename cannot overwrite a directory with a file)
		tmpDir, err := os.MkdirTemp("", "config_test_*.yaml")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		f := NewFileSource(tmpDir).(*fileSource)
		mgr := newMockManager()
		err = mgr.BulkSetAtomic(context.Background(), map[string]any{"key": "value"})
		require.NoError(t, err)

		err = f.Persist(mgr, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to rename")
	})
}

func TestCheckForUnsupportedTypes(t *testing.T) {
	t.Run("valid types", func(t *testing.T) {
		validData := map[string]any{
			"string": "value",
			"number": 42,
			"float":  3.14,
			"bool":   true,
			"slice":  []any{1, 2, 3},
			"nested": map[string]any{
				"inner": map[string]any{
					"deep": "value",
				},
			},
		}

		err := checkForUnsupportedTypes(validData)
		assert.NoError(t, err)
	})

	t.Run("function type", func(t *testing.T) {
		invalidData := map[string]any{
			"valid":   "value",
			"invalid": func() {},
		}

		err := checkForUnsupportedTypes(invalidData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported type")
	})

	t.Run("channel type", func(t *testing.T) {
		invalidData := map[string]any{
			"valid":   "value",
			"invalid": make(chan int),
		}

		err := checkForUnsupportedTypes(invalidData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported type")
		assert.Contains(t, err.Error(), "channel")
	})

	t.Run("nested function type", func(t *testing.T) {
		invalidData := map[string]any{
			"nested": map[string]any{
				"deep": map[string]any{
					"invalid": func() {},
				},
			},
		}

		err := checkForUnsupportedTypes(invalidData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported type")
	})

	t.Run("function in slice", func(t *testing.T) {
		invalidData := map[string]any{
			"items": []any{
				"valid",
				42,
				func() {},
			},
		}

		err := checkForUnsupportedTypes(invalidData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported type")
	})
}
