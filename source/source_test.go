package source

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockConfigSource struct{}

func (m *mockConfigSource) Load(ctx context.Context, cm configManager) error {
	return nil
}

func (m *mockConfigSource) Watch(ctx context.Context, cm configManager, cb WatchOnChangeCallback) error {
	return nil
}

type mockGlobalConfigSource struct {
	mockConfigSource
}

func (m *mockGlobalConfigSource) IsGlobal() bool {
	return true
}

type mockStoppableConfigSource struct {
	mockConfigSource
	stopped bool
}

func (m *mockStoppableConfigSource) Stop() error {
	m.stopped = true
	return nil
}

type mockPersistableConfigSource struct {
	mockConfigSource
}

func (m *mockPersistableConfigSource) Persist(cm configManager, namespace string, keys ...string) error {
	return nil
}

func TestFindSourceByType(t *testing.T) {
	t.Run("finds source of correct type", func(t *testing.T) {
		sources := []ConfigSource{
			&mockConfigSource{},
			&mockGlobalConfigSource{},
			&mockStoppableConfigSource{},
		}

		result, err := FindSourceByType[*mockGlobalConfigSource](sources)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.IsGlobal())
	})

	t.Run("finds first matching source when multiple exist", func(t *testing.T) {
		global1 := &mockGlobalConfigSource{}
		global2 := &mockGlobalConfigSource{}
		sources := []ConfigSource{
			&mockConfigSource{},
			global1,
			global2,
		}

		result, err := FindSourceByType[*mockGlobalConfigSource](sources)
		assert.NoError(t, err)
		assert.Same(t, global1, result)
	})

	t.Run("returns error when source type not found", func(t *testing.T) {
		sources := []ConfigSource{
			&mockConfigSource{},
			&mockStoppableConfigSource{},
		}

		result, err := FindSourceByType[*mockGlobalConfigSource](sources)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no source of type")
	})

	t.Run("returns zero value when source not found", func(t *testing.T) {
		sources := []ConfigSource{
			&mockConfigSource{},
		}

		result, err := FindSourceByType[*mockGlobalConfigSource](sources)
		assert.Error(t, err)
		var zero *mockGlobalConfigSource
		assert.Equal(t, zero, result)
	})

	t.Run("finds base ConfigSource type", func(t *testing.T) {
		sources := []ConfigSource{
			&mockConfigSource{},
			&mockGlobalConfigSource{},
		}

		result, err := FindSourceByType[*mockConfigSource](sources)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("finds StoppableConfigSource", func(t *testing.T) {
		sources := []ConfigSource{
			&mockConfigSource{},
			&mockStoppableConfigSource{},
		}

		result, err := FindSourceByType[*mockStoppableConfigSource](sources)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("finds PersistableConfigSource", func(t *testing.T) {
		sources := []ConfigSource{
			&mockConfigSource{},
			&mockPersistableConfigSource{},
		}

		result, err := FindSourceByType[*mockPersistableConfigSource](sources)
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("handles empty slice", func(t *testing.T) {
		sources := []ConfigSource{}

		result, err := FindSourceByType[*mockConfigSource](sources)
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("handles nil slice", func(t *testing.T) {
		var sources []ConfigSource = nil

		result, err := FindSourceByType[*mockConfigSource](sources)
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}
