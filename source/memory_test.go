package source

import (
	"context"
	"testing"

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

	t.Run("Watch returns nil", func(t *testing.T) {
		err := src.Watch(context.Background(), koanf.New("."), func(changedKeys []string, err error) {})
		assert.NoError(t, err)
	})

	t.Run("Persist does nothing", func(t *testing.T) {
		err := src.Persist(context.Background(), koanf.New("."))
		assert.NoError(t, err)
	})
}
