package source

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"reflect"
	"slices"
	"sync"
	"testing"
)

// setupDefaultConfigTest creates a mock manager and DefaultConfigSource for testing
func setupDefaultConfigTest(t *testing.T, defaults map[string]any, tagName string) (*mockManager, *DefaultConfigSource) {
	mgr := newMockManager()
	var opts []DefaultConfigOption
	if defaults != nil {
		opts = append(opts, WithDefaultSourceDefaults(defaults))
	}
	if tagName != "" {
		opts = append(opts, WithDefaultSourceTagName(tagName))
	}
	return mgr, NewDefaultConfigSource(mgr, opts...)
}

// isNumeric checks if a value is a numeric type
func isNumeric(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	}
	return false
}

// numericEqual compares two numeric values regardless of their concrete types
func numericEqual(a, b any) bool {
	return reflect.ValueOf(a).Convert(reflect.TypeOf(float64(0))).Float() ==
		reflect.ValueOf(b).Convert(reflect.TypeOf(float64(0))).Float()
}

// mockManager is a mock implementation of configManager and manager interfaces
type mockManager struct {
	mu       sync.RWMutex
	data     map[string]any
	structs  map[string]reflect.Type
	delim    string
	setCalls []string // tracks Set calls for testing
}

func newMockManager(delim ...string) *mockManager {
	d := "."
	if len(delim) > 0 {
		d = delim[0]
	}
	return &mockManager{
		data:    make(map[string]any),
		structs: make(map[string]reflect.Type),
		delim:   d,
	}
}

func (m *mockManager) All() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.data
}

// GetRegisteredStructs implements the manager interface
func (m *mockManager) GetRegisteredStructs() map[string]reflect.Type {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.structs
}

// RegisterStruct implements the manager interface
func (m *mockManager) RegisterStruct(key string, cfg any) error {
	typ := reflect.TypeOf(cfg)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.structs[key] = typ
	return nil
}

func (m *mockManager) Get(key string, target ...any) (any, any, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, exists := m.data[key]
	if !exists {
		return nil, nil, fmt.Errorf("key %s not found", key)
	}
	return val, val, nil
}

func (m *mockManager) Exists(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.data[key]
	return exists
}

func (m *mockManager) Set(ctx context.Context, key string, value any) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setCalls = append(m.setCalls, key)
	m.data[key] = value
	return nil
}

func (m *mockManager) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
}

func (m *mockManager) BulkSet(ctx context.Context, updates map[string]any) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, value := range updates {
		m.setCalls = append(m.setCalls, key)
		m.data[key] = value
	}
	return nil
}

func (m *mockManager) BulkSetAtomic(ctx context.Context, updates map[string]any) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Record that BulkSetAtomic was called
	m.setCalls = append(m.setCalls, "BulkSetAtomic")

	// First validate all updates
	for key, value := range updates {
		if value == nil {
			return fmt.Errorf("nil value for key %s", key)
		}
	}

	// Then apply all updates
	for key, value := range updates {
		m.setCalls = append(m.setCalls, key)
		m.data[key] = value
	}
	return nil
}

func (m *mockManager) SetAtomic(ctx context.Context, updates map[string]any) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// First validate all updates
	for key, value := range updates {
		if value == nil {
			return fmt.Errorf("nil value for key %s", key)
		}
	}

	// Then apply all updates
	for key, value := range updates {
		m.setCalls = append(m.setCalls, key)
		m.data[key] = value
	}
	return nil
}

func (m *mockManager) Keys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, len(m.data))
	i := 0
	for k := range m.data {
		keys[i] = k
		i++
	}
	return keys
}

func (m *mockManager) Delim() string {
	return m.delim
}

// Additional test helper methods
func (m *mockManager) assertSetCalled(t *testing.T, key string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !slices.Contains(m.setCalls, key) {
		t.Errorf("expected Set(%q) to be called", key)
	}
}

func (m *mockManager) assertValue(t *testing.T, key string, expected any) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.data[key]
	if !ok {
		t.Errorf("key %q not found in manager", key)
		return
	}

	// Handle numeric type differences using reflection
	if isNumeric(expected) && isNumeric(val) {
		if !numericEqual(expected, val) {
			t.Errorf("for key %q, got %v (%T), want %v (%T)", key, val, val, expected, expected)
		}
		return
	}

	if !reflect.DeepEqual(val, expected) {
		t.Errorf("for key %q, got %v (%T), want %v (%T)", key, val, val, expected, expected)
	}
}

// testConfigWithDefaultSourceDefaults implements ConfigDefaults
type testConfigWithDefaultSourceDefaults struct {
	Host string `config:"host"`
	Port int    `config:"port"`
}

func (t *testConfigWithDefaultSourceDefaults) Defaults() map[string]any {
	return map[string]any{
		"Host": "default_host",
		"Port": 8080,
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
		dcs := NewDefaultConfigSource(mgr)
		assert.NotNil(t, dcs)
		assert.Empty(t, dcs.defaults)
	})

	t.Run("empty defaults", func(t *testing.T) {
		dcs := NewDefaultConfigSource(mgr, WithDefaultSourceDefaults(map[string]any{}))
		assert.NotNil(t, dcs)
		assert.Empty(t, dcs.defaults)
	})

	t.Run("flat defaults", func(t *testing.T) {
		defaults := map[string]any{
			"key1": "value1",
			"key2": 123,
		}
		dcs := NewDefaultConfigSource(mgr, WithDefaultSourceDefaults(defaults), WithDefaultSourceTagName("config"))
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
		dcs := NewDefaultConfigSource(mgr, WithDefaultSourceDefaults(defaults))
		assert.NotNil(t, dcs)
		assert.Equal(t, expectedFlatDefaults, dcs.defaults)
	})
}

func TestDefaultConfigSource_Load(t *testing.T) {
	ctx := context.Background()

	t.Run("load static defaults", func(t *testing.T) {
		defaults := map[string]any{
			"app.name": "TestApp",
			"app.port": 8000,
		}
		mgr, dcs := setupDefaultConfigTest(t, defaults, "")

		err := dcs.Load(ctx, mgr)
		assert.NoError(t, err)

		mgr.assertValue(t, "app.name", "TestApp")
		mgr.assertValue(t, "app.port", 8000)
	})

	t.Run("load struct defaults", func(t *testing.T) {
		mgr, dcs := setupDefaultConfigTest(t, nil, "config")
		err := mgr.RegisterStruct("db", testConfigWithDefaultSourceDefaults{})
		assert.NoError(t, err)

		err = dcs.Load(ctx, mgr)
		assert.NoError(t, err)

		mgr.assertValue(t, "db.host", "default_host")
		mgr.assertValue(t, "db.port", 8080)
	})

	t.Run("load struct without defaults", func(t *testing.T) {
		mgr, dcs := setupDefaultConfigTest(t, nil, "")
		err := mgr.RegisterStruct("user", testConfigWithoutDefaults{})
		assert.NoError(t, err)

		err = dcs.Load(ctx, mgr)
		assert.NoError(t, err)

		_, _, err = mgr.Get("user.name")
		assert.Error(t, err)
	})

	t.Run("load struct with empty defaults", func(t *testing.T) {
		mgr, dcs := setupDefaultConfigTest(t, nil, "")
		err := mgr.RegisterStruct("emptycfg", testConfigWithEmptyDefaults{})
		assert.NoError(t, err)

		err = dcs.Load(ctx, mgr)
		assert.NoError(t, err)

		_, _, err = mgr.Get("emptycfg.somekey")
		assert.Error(t, err)
	})

	t.Run("static defaults do not overwrite existing values", func(t *testing.T) {
		mgr, dcs := setupDefaultConfigTest(t, map[string]any{"app.name": "DefaultAppName"}, "")
		err := mgr.Set(context.Background(), "app.name", "ExistingApp")
		assert.NoError(t, err)

		err = dcs.Load(ctx, mgr)
		assert.NoError(t, err)

		mgr.assertValue(t, "app.name", "ExistingApp")
	})

	t.Run("struct defaults do not overwrite existing values", func(t *testing.T) {
		mgr, dcs := setupDefaultConfigTest(t, nil, "")
		err := mgr.RegisterStruct("db", testConfigWithDefaultSourceDefaults{})
		assert.NoError(t, err)
		err = mgr.Set(context.Background(), "db.host", "existing_db_host")
		assert.NoError(t, err)

		err = dcs.Load(ctx, mgr)
		assert.NoError(t, err)

		mgr.assertValue(t, "db.host", "existing_db_host")
		mgr.assertValue(t, "db.port", 8080) // Port should still be default
	})

	t.Run("load both static and struct defaults", func(t *testing.T) {
		mgr, dcs := setupDefaultConfigTest(t, map[string]any{"app.name": "TestApp"}, "")
		err := mgr.RegisterStruct("db", testConfigWithDefaultSourceDefaults{})
		assert.NoError(t, err)

		err = dcs.Load(ctx, mgr)
		assert.NoError(t, err)

		// Check struct defaults
		mgr.assertValue(t, "db.host", "default_host")
		mgr.assertValue(t, "db.port", 8080)
		// Check static defaults
		mgr.assertValue(t, "app.name", "TestApp")
	})

	t.Run("order of loading: struct defaults first, then static defaults", func(t *testing.T) {
		mgr, dcs := setupDefaultConfigTest(t, map[string]any{"conflict.host": "static_host_override"}, "")
		err := mgr.RegisterStruct("conflict", testConfigWithDefaultSourceDefaults{}) // Defaults Host to "default_host"
		assert.NoError(t, err)

		err = dcs.Load(ctx, mgr)
		assert.NoError(t, err)

		// Static defaults are loaded after struct defaults. If a key exists (e.g. from struct default),
		// static default for the same key should NOT overwrite it.
		mgr.assertValue(t, "conflict.host", "default_host")
		mgr.assertValue(t, "conflict.port", 8080)
	})

	t.Run("static defaults for a key not in struct defaults", func(t *testing.T) {
		mgr, dcs := setupDefaultConfigTest(t, map[string]any{"db.user": "static_user"}, "")
		err := mgr.RegisterStruct("db", testConfigWithDefaultSourceDefaults{}) // Defaults host and port
		assert.NoError(t, err)

		err = dcs.Load(ctx, mgr)
		assert.NoError(t, err)

		mgr.assertValue(t, "db.host", "default_host")
		mgr.assertValue(t, "db.port", 8080)
		mgr.assertValue(t, "db.user", "static_user")
	})

}

var _ ConfigDefaults = (*testConfig)(nil)

// testConfig implements ConfigDefaults
type testConfig struct {
	FieldOne   string `config:"field_one"`
	FieldTwo   string // No tag
	fieldThree string `config:"field_three"` // Unexported
	Nested     struct {
		ChildOne string `config:"child_one"`
		ChildTwo int
	} `config:"nested"`
}

func (t *testConfig) Defaults() map[string]any {
	return map[string]any{
		"FieldOne":    "value_one",   // Field name match (tag is "field_one")
		"FieldTwo":    "value_two",   // Field name match (no tag)
		"fieldThree":  "value_three", // Should be skipped (unexported)
		"field_four":  "value_four",  // Should be skipped (no match)
		"FieldTwoAlt": "value_alt",   // Should be skipped (no match)
		"Nested": map[string]any{
			"ChildOne": "nested_value",
			"ChildTwo": 42,
		},
	}
}

func TestDefaultConfigSource_Load_StructFieldMatching(t *testing.T) {
	ctx := context.Background()

	mgr, dcs := setupDefaultConfigTest(t, nil, "config")
	err := dcs.manager.RegisterStruct("test", &testConfig{})
	assert.NoError(t, err)

	err = dcs.Load(ctx, mgr)
	assert.NoError(t, err)

	// Check expected values were set
	mgr.assertValue(t, "test.field_one", "value_one")           // FieldOne's tag is "field_one"
	mgr.assertValue(t, "test.FieldTwo", "value_two")            // FieldTwo has no tag so uses field name
	mgr.assertValue(t, "test.nested.child_one", "nested_value") // Nested field with tag

	// Check unexpected values were NOT set
	_, _, err = mgr.Get("test.field_three")
	assert.Error(t, err)
	_, _, err = mgr.Get("test.field_four")
	assert.Error(t, err)
	_, _, err = mgr.Get("test.FieldTwoAlt")
	assert.Error(t, err)
}

func TestProcessStructDefaults_EmptyDefaults(t *testing.T) {
	ctx := context.Background()
	mgr, dcs := setupDefaultConfigTest(t, nil, "config")

	// Register a struct with no defaults
	type EmptyStruct struct {
		Field string `config:"field"`
	}
	err := mgr.RegisterStruct("empty", EmptyStruct{})
	assert.NoError(t, err)

	// Load the defaults - this internally calls processStructDefaults
	err = dcs.Load(ctx, mgr)
	assert.NoError(t, err)
	assert.False(t, mgr.Exists("empty.field"))
}

func TestProcessDirectDefaults_NonStructFields(t *testing.T) {
	ctx := context.Background()

	type TestStruct struct {
		StrField string `config:"str_field"`
		IntField int    `config:"int_field"`
	}

	// Create defaults that match the struct fields
	defaults := map[string]any{
		"test": map[string]any{
			"str_field": "default_str",
			"int_field": 42,
		},
	}

	mgr, dcs := setupDefaultConfigTest(t, defaults, "config")

	// Register the struct
	err := mgr.RegisterStruct("test", TestStruct{})
	assert.NoError(t, err)

	// Load the defaults
	err = dcs.Load(ctx, mgr)
	assert.NoError(t, err)

	// Verify keys were set
	assert.True(t, mgr.Exists("test.str_field"))
	assert.True(t, mgr.Exists("test.int_field"))

	// Get values directly from manager to verify
	strVal, _, err := mgr.Get("test.str_field")
	assert.NoError(t, err)
	assert.Equal(t, "default_str", strVal)

	intVal, _, err := mgr.Get("test.int_field")
	assert.NoError(t, err)
	assert.Equal(t, 42, intVal)
}

func TestProcessNestedStructs_AllFields(t *testing.T) {
	ctx := context.Background()
	mgr, dcs := setupDefaultConfigTest(t, nil, "config")

	type Nested struct {
		Child string `config:"child"`
	}
	type Parent struct {
		Nested1 Nested `config:"nested1"`
		Nested2 Nested `config:"nested2"`
	}

	// Register the struct with defaults
	err := mgr.RegisterStruct("test", ParentWithDefaultSourceDefaults{})
	assert.NoError(t, err)

	// Load the defaults - this internally processes nested structs
	err = dcs.Load(ctx, mgr)
	assert.NoError(t, err)

	// Verify nested values were set correctly
	mgr.assertValue(t, "test.nested1.child", "child1")
	assert.False(t, mgr.Exists("test.nested2.child"))
}

type TestFindMatchingFieldStruct struct {
	FieldOne   string `config:"field_one"`
	FieldTwo   string `config:"field_two"`
	fieldThree string `config:"field_three"` // Unexported
}

func (t *TestFindMatchingFieldStruct) Defaults() map[string]any {
	return map[string]any{
		"FieldOne": "value_one",
		"FieldTwo": "value_two",
	}
}

func TestFindMatchingField_Indirect(t *testing.T) {
	// Implement ConfigDefaults to test field matching behavior
	type TestConfig struct {
		TestFindMatchingFieldStruct
	}

	cfg := &TestConfig{}

	mgr, dcs := setupDefaultConfigTest(t, nil, "config")
	err := mgr.RegisterStruct("test", cfg)
	assert.NoError(t, err)

	err = dcs.Load(context.Background(), mgr)
	assert.NoError(t, err)

	// Verify expected fields were set
	mgr.assertValue(t, "test.field_one", "value_one")
	mgr.assertValue(t, "test.field_two", "value_two")

	// Verify unexpected fields were NOT set
	_, _, err = mgr.Get("test.field_three")
	assert.Error(t, err)
	_, _, err = mgr.Get("test.nonexistent")
	assert.Error(t, err)
}

func TestSetDefaultValue_Indirect(t *testing.T) {
	ctx := context.Background()

	// Create defaults that will be loaded
	defaults := map[string]any{
		"test.key":     "default_value",
		"existing.key": "default_value",
	}

	mgr, dcs := setupDefaultConfigTest(t, defaults, "")

	// Set an existing value first
	err := mgr.Set(ctx, "existing.key", "existing_value")
	assert.NoError(t, err)

	// Load the defaults - this internally calls setDefaultValue
	err = dcs.Load(ctx, mgr)
	assert.NoError(t, err)

	// Verify new key got default value
	mgr.assertValue(t, "test.key", "default_value")

	// Verify existing key was NOT overwritten
	mgr.assertValue(t, "existing.key", "existing_value")
}

func TestDefaultConfigSource_Watch(t *testing.T) {
	mgr, dcs := setupDefaultConfigTest(t, nil, "config")
	ctx := context.Background()

	err := dcs.Watch(ctx, mgr, func(changedKeys []string, err error) {
		assert.Fail(t, "Watch callback should not be called for DefaultConfigSource")
	})
	assert.NoError(t, err, "Watch should return nil and be a no-op")
}

type Nested struct {
	Child string `config:"child"`
}

type ParentWithDefaultSourceDefaults struct {
	Nested1 Nested `config:"nested1"`
	Nested2 Nested `config:"nested2"`
}

func (p *ParentWithDefaultSourceDefaults) Defaults() map[string]any {
	return map[string]any{
		"Nested1": map[string]any{
			"Child": "child1",
		},
	}
}
