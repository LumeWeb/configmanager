package source

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// FileConfigSource loads configuration from a file using koanf's file provider.
type FileConfigSource struct {
	path     string
	provider *file.File
}

// NewFileConfigSource creates a new FileConfigSource.
// Returns an error if the path cannot be resolved to an absolute path.
func NewFileConfigSource(path string) (*FileConfigSource, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for %q: %w", path, err)
	}
	return &FileConfigSource{
		path:     absPath,
		provider: file.Provider(absPath),
	}, nil
}

// Load loads the configuration from the file into the config manager.
func (f *FileConfigSource) Load(ctx context.Context, cm configManager) error {
	// Create temporary koanf to load file
	tmpKoanf := koanf.New(".")
	if err := tmpKoanf.Load(f.provider, nil); err != nil {
		return err
	}

	// Use BulkSetAtomic for atomic loading of all values
	if err := cm.BulkSetAtomic(ctx, tmpKoanf.All()); err != nil {
		return fmt.Errorf("failed to bulk set config values: %w", err)
	}
	return nil
}

// Watch watches for changes in the file and triggers the onChange function when a change occurs.
func (f *FileConfigSource) Watch(ctx context.Context, cm configManager, onChange WatchOnChangeCallback) error {
	// Use the file provider's built-in Watch() functionality
	return f.provider.Watch(func(event any, err error) {
		if err != nil {
			// Pass through watch errors
			onChange(nil, err)
			return
		}

		// Reload the config file on changes
		if err := f.Load(ctx, cm); err != nil {
			// Pass through the load error with WatchAllChanges to indicate
			// the configuration may be in an inconsistent state
			onChange([]string{WatchAllChanges}, fmt.Errorf("failed to reload config file: %w", err))
			return
		}

		// Notify that all keys may have changed
		onChange([]string{WatchAllChanges}, nil)
	})
}

// Stop stops the file watcher if it's running.
func (f *FileConfigSource) Stop() error {
	return f.provider.Unwatch()
}
