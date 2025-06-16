package source

import (
	"context"
	"reflect"
	"testing"

	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
)

// mockManager is a mock implementation of the manager interface needed by DefaultConfigSource
type mockManager struct {
	structs map[string]reflect.Type
}

func newMockManager() *mockManager {
	return &mockManager{
		structs: make(map[string]reflect.Type),
	}
}

func (m *mockManager) RegisterStruct(key string, cfg any) error {
	typ := reflect.TypeOf(cfg)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	m.structs[key] = typ
	return nil
}

func (m *mockManager) GetRegisteredStructs() map[string]reflect.Type {
	return m.structs
}

// testConfigWithDefaults implements ConfigDefaults
type testConfigWithDefaults struct {
	Host string `config:"host"`
	Port int    `config:"port"`
}

func (t *testConfigWithDefaults) Defaults() map[string]any {
	return map[string]any{
		"host": "default_host",
		"port": 8080,
	}
}

// testConfigWithoutDefaults does not implement ConfigDefaults
type testConfigWithoutDefaults struct {
	Name string `config:"name"`
}

// testConfigWithEmptyDefaults implements ConfigDefaults but returns an empty map
type testConfigWithEmptyDefaults struct{}

func (t *testConfigWithEmptyDefaults) Defaults() map[string]any {
	return map[string]any{}
}

func TestNewDefaultConfigSource(t *testing.T) {
	mgr := newMockManager()

	t.Run("nil defaults", func(t *testing.T) {
		dcs := NewDefaultConfigSource(mgr, nil)
		assert.NotNil(t, dcs)
		assert.Empty(t, dcs.defaults)
	})

	t.Run("empty defaults", func(t *testing.T) {
		dcs := NewDefaultConfigSource(mgr, map[string]any{})
		assert.NotNil(t, dcs)
		assert.Empty(t, dcs.defaults)
	})

	t.Run("flat defaults", func(t *testing.T) {
		defaults := map[string]any{
			"key1": "value1",
			"key2": 123,
		}
		dcs := NewDefaultConfigSource(mgr, defaults)
		assert.NotNil(t, dcs)
		assert.Equal(t, defaults, dcs.defaults)
	})

	t.Run("nested defaults", func(t *testing.T) {
		defaults := map[string]any{
			"parent": map[string]any{
				"child1": "value1",
				"child2": true,
			},
			"key1": 123,
		}
		expectedFlatDefaults := map[string]any{
			"parent.child1": "value1",
			"parent.child2": true,
			"key1":          123,
		}
		dcs := NewDefaultConfigSource(mgr, defaults)
		assert.NotNil(t, dcs)
		assert.Equal(t, expectedFlatDefaults, dcs.defaults)
	})
}

func TestDefaultConfigSource_Load(t *testing.T) {
	ctx := context.Background()

	t.Run("load static defaults", func(t *testing.T) {
		mgr := newMockManager()
		k := koanf.New(".")
		defaults := map[string]any{
			"app.name": "TestApp",
			"app.port": 8000,
		}
		dcs := NewDefaultConfigSource(mgr, defaults)

		err := dcs.Load(ctx, k)
		assert.NoError(t, err)

		assert.Equal(t, "TestApp", k.String("app.name"))
		assert.Equal(t, 8000, k.Int("app.port"))
	})

	t.Run("load struct defaults", func(t *testing.T) {
		mgr := newMockManager()
		err := mgr.RegisterStruct("db", testConfigWithDefaults{})
		assert.NoError(t, err)

		k := koanf.New(".")
		dcs := NewDefaultConfigSource(mgr, nil)

		err = dcs.Load(ctx, k)
		assert.NoError(t, err)

		assert.Equal(t, "default_host", k.String("db.host"))
		assert.Equal(t, 8080, k.Int("db.port"))
	})

	t.Run("load struct without defaults", func(t *testing.T) {
		mgr := newMockManager()
		err := mgr.RegisterStruct("user", testConfigWithoutDefaults{})
		assert.NoError(t, err)

		k := koanf.New(".")
		dcs := NewDefaultConfigSource(mgr, nil)

		err = dcs.Load(ctx, k)
		assert.NoError(t, err)

		assert.False(t, k.Exists("user.name"))
	})

	t.Run("load struct with empty defaults", func(t *testing.T) {
		mgr := newMockManager()
		err := mgr.RegisterStruct("emptycfg", testConfigWithEmptyDefaults{})
		assert.NoError(t, err)

		k := koanf.New(".")
		dcs := NewDefaultConfigSource(mgr, nil)

		err = dcs.Load(ctx, k)
		assert.NoError(t, err)

		assert.False(t, k.Exists("emptycfg.somekey")) // Ensure no keys are added
		assert.Empty(t, k.All())
	})

	t.Run("static defaults do not overwrite existing values", func(t *testing.T) {
		mgr := newMockManager()
		k := koanf.New(".")
		err := k.Set("app.name", "ExistingApp")
		assert.NoError(t, err)

		defaults := map[string]any{
			"app.name": "DefaultAppName",
		}
		dcs := NewDefaultConfigSource(mgr, defaults)

		err = dcs.Load(ctx, k)
		assert.NoError(t, err)

		assert.Equal(t, "ExistingApp", k.String("app.name"))
	})

	t.Run("struct defaults do not overwrite existing values", func(t *testing.T) {
		mgr := newMockManager()
		err := mgr.RegisterStruct("db", testConfigWithDefaults{})
		assert.NoError(t, err)

		k := koanf.New(".")
		err = k.Set("db.host", "existing_db_host")
		assert.NoError(t, err)

		dcs := NewDefaultConfigSource(mgr, nil)

		err = dcs.Load(ctx, k)
		assert.NoError(t, err)

		assert.Equal(t, "existing_db_host", k.String("db.host"))
		assert.Equal(t, 8080, k.Int("db.port")) // Port should still be default
	})

	t.Run("load both static and struct defaults", func(t *testing.T) {
		mgr := newMockManager()
		err := mgr.RegisterStruct("db", testConfigWithDefaults{})
		assert.NoError(t, err)

		k := koanf.New(".")
		staticDefaults := map[string]any{
			"app.name": "TestApp",
		}
		dcs := NewDefaultConfigSource(mgr, staticDefaults)

		err = dcs.Load(ctx, k)
		assert.NoError(t, err)

		// Check struct defaults
		assert.Equal(t, "default_host", k.String("db.host"))
		assert.Equal(t, 8080, k.Int("db.port"))
		// Check static defaults
		assert.Equal(t, "TestApp", k.String("app.name"))
	})

	t.Run("order of loading: struct defaults first, then static defaults", func(t *testing.T) {
		mgr := newMockManager()
		err := mgr.RegisterStruct("conflict", testConfigWithDefaults{}) // Defaults host to "default_host"
		assert.NoError(t, err)

		k := koanf.New(".")
		staticDefaults := map[string]any{
			"conflict.host": "static_host_override", // Static default for the same key
		}
		dcs := NewDefaultConfigSource(mgr, staticDefaults)

		err = dcs.Load(ctx, k)
		assert.NoError(t, err)

		// Static defaults are loaded after struct defaults. If a key exists (e.g. from struct default),
		// static default for the same key should NOT overwrite it.
		assert.Equal(t, "default_host", k.String("conflict.host"))
		assert.Equal(t, 8080, k.Int("conflict.port"))
	})

	t.Run("static defaults for a key not in struct defaults", func(t *testing.T) {
		mgr := newMockManager()
		err := mgr.RegisterStruct("db", testConfigWithDefaults{}) // Defaults host and port
		assert.NoError(t, err)

		k := koanf.New(".")
		staticDefaults := map[string]any{
			"db.user": "static_user", // Static default for a key not in struct defaults
		}
		dcs := NewDefaultConfigSource(mgr, staticDefaults)

		err = dcs.Load(ctx, k)
		assert.NoError(t, err)

		assert.Equal(t, "default_host", k.String("db.host"))
		assert.Equal(t, 8080, k.Int("db.port"))
		assert.Equal(t, "static_user", k.String("db.user"))
	})

}

func TestDefaultConfigSource_Watch(t *testing.T) {
	mgr := newMockManager()
	dcs := NewDefaultConfigSource(mgr, nil)
	k := koanf.New(".")
	ctx := context.Background()

	err := dcs.Watch(ctx, k, func(changedKeys []string, err error) {
		assert.Fail(t, "Watch callback should not be called for DefaultConfigSource")
	})
	assert.NoError(t, err, "Watch should return nil and be a no-op")
}
