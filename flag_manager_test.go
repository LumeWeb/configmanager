package configmanager

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlagManager(t *testing.T) {
	fm := NewFlagManager()

	// Test SetFlags
	fm.SetFlags("test.key", []string{"flag1", "flag2"})
	flags := fm.GetFlags("test.key")
	assert.Equal(t, []string{"flag1", "flag2"}, flags, "SetFlags should set the flags correctly")

	// Test GetFlags
	flags = fm.GetFlags("nonexistent.key")
	assert.Nil(t, flags, "GetFlags should return nil for nonexistent key")

	// Test HasFlag
	assert.True(t, fm.HasFlag("test.key", "flag1"), "HasFlag should return true if flag exists")
	assert.False(t, fm.HasFlag("test.key", "flag3"), "HasFlag should return false if flag does not exist")
	assert.False(t, fm.HasFlag("nonexistent.key", "flag1"), "HasFlag should return false for nonexistent key")
}
