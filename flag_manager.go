package configmanager

import "sync"

// FlagManager manages flags associated with configuration keys
type FlagManager interface {
	// SetFlags sets flags for a configuration key
	SetFlags(key string, flags []string)
	// GetFlags gets flags for a configuration key
	GetFlags(key string) []string
	// HasFlag checks if a key has a specific flag
	HasFlag(key string, flag string) bool
}

// FlagManagerDefault is the default implementation of FlagManager
type FlagManagerDefault struct {
	flags map[string][]string
	mu    sync.RWMutex
}

// NewFlagManager creates a new FlagManagerDefault
func NewFlagManager() *FlagManagerDefault {
	return &FlagManagerDefault{
		flags: make(map[string][]string),
	}
}

// SetFlags sets flags for a configuration key
func (f *FlagManagerDefault) SetFlags(key string, flags []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flags[key] = flags
}

// GetFlags gets flags for a configuration key
func (f *FlagManagerDefault) GetFlags(key string) []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.flags[key]
}

// HasFlag checks if a key has a specific flag
func (f *FlagManagerDefault) HasFlag(key string, flag string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, fl := range f.flags[key] {
		if fl == flag {
			return true
		}
	}
	return false
}
