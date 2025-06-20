package source

import (
	"context"
	"fmt"
)

// TestingInvalidSource is a ConfigSource that always returns errors.
// Useful for testing error handling.
type TestingInvalidSource struct {
	loadError  string
	watchError string
}

// New creates a new TestingInvalidSource with custom error messages.
// If empty strings are provided, default error messages will be used.
func New(loadError, watchError string) *TestingInvalidSource {
	if loadError == "" {
		loadError = "invalid source: forced load error"
	}
	if watchError == "" {
		watchError = "invalid source: forced watch error"
	}
	return &TestingInvalidSource{
		loadError:  loadError,
		watchError: watchError,
	}
}

// Load always returns an error.
func (i *TestingInvalidSource) Load(ctx context.Context, cm configManager) error {
	return fmt.Errorf("%s", i.loadError)
}

// Watch always returns an error.
func (i *TestingInvalidSource) Watch(ctx context.Context, cm configManager, cb WatchOnChangeCallback) error {
	return fmt.Errorf("%s", i.watchError)
}
