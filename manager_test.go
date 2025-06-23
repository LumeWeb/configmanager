package configmanager

import (
	"context"
	"fmt"
	"github.com/Oudwins/zog"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
	"go.lumeweb.com/configmanager/source"
	csync "go.lumeweb.com/configmanager/sync"
	"go.uber.org/zap"
)

// Define a struct that implements ConfigSchemaProvider
type SchemaValidatedConfig struct {
	Email    string `config:"email"`
	Password string `config:"password"`
}

// Implement ConfigSchemaProvider
func (s *SchemaValidatedConfig) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"email": zog.String().Email(),
		"password": zog.String().
			Min(8).
			ContainsUpper().
			ContainsDigit(),
	})
}

type TestConfig struct {
	StringValue   string        `config:"string_value"`
	IntValue      int           `config:"int_value"`
	BoolValue     bool          `config:"bool_value"`
	DurationValue time.Duration `config:"duration_value"`
}

func newTestManagerWithData(data map[string]any) *ConfigManagerDefault {
	memSource := source.NewMemoryConfigSource(data)
	cm, err := NewConfigManager([]source.ConfigSource{memSource})
	if err != nil {
		panic(err)
	}

	// Load the configuration after creating the manager
	if err := cm.Load(); err != nil {
		panic(err)
	}

	return cm
}

func newTestManager() *ConfigManagerDefault {
	return newTestManagerWithData(nil)
}

func TestConfigManager_Copy(t *testing.T) {
	cm := newTestManager()

	// Set some state
	cm.Set(context.Background(), "test.key", "value")
	cm.RegisterStruct("test.struct", struct{}{})
	cm.DisableValidation()

	// Create copy
	copy := cm.copy()

	// Verify copied state
	assert.NotSame(t, cm, copy)
	assert.NotSame(t, cm.koanf, copy.koanf)
	assert.Equal(t, cm.sources, copy.sources)
	assert.Equal(t, cm.logger, copy.logger)
	assert.Equal(t, cm.configStructs, copy.configStructs)
	assert.Equal(t, cm.validationEnabled, copy.validationEnabled)

	// Verify copy has empty config data
	assert.Empty(t, copy.All())

	// Verify validation state is preserved
	assert.False(t, copy.ValidationEnabled())
}

func TestNewConfigManager(t *testing.T) {
	cm := newTestManagerWithData(map[string]any{
		"test.key": "test_value",
	})
	assert.NotNil(t, cm)
	assert.IsType(t, &ConfigManagerDefault{}, cm)
	assert.IsType(t, &koanf.Koanf{}, cm.koanf)
	assert.NotNil(t, cm.flagManager)
	assert.NotNil(t, cm.configStructs)

	// Verify memory source was loaded
	raw, decoded, err := cm.Get("test.key")
	assert.NoError(t, err)
	assert.Equal(t, "test_value", raw)
	assert.Equal(t, "test_value", decoded)
}

func TestWithDelimiter(t *testing.T) {
	// Create manager with custom delimiter
	cm, err := NewConfigManager([]source.ConfigSource{}, WithDelimiter("/"))
	require.NoError(t, err)

	// Verify delimiter is set correctly
	assert.Equal(t, "/", cm.Delim())

	// Test setting and getting with custom delimiter
	err = cm.Set(context.Background(), "test/key", "value")
	assert.NoError(t, err)

	val, _, err := cm.Get("test/key")
	assert.NoError(t, err)
	assert.Equal(t, "value", val)

	// Verify nested keys work with custom delimiter
	err = cm.Set(context.Background(), "test/nested/key", "nested_value")
	assert.NoError(t, err)

	nestedVal, _, err := cm.Get("test/nested/key")
	assert.NoError(t, err)
	assert.Equal(t, "nested_value", nestedVal)

	// Verify keys are properly split
	keys := cm.Keys()
	assert.Contains(t, keys, "test/key")
	assert.Contains(t, keys, "test/nested/key")
}

func TestConfigManager_WildcardSubscriptions(t *testing.T) {
	testCases := []struct {
		name          string
		key           string
		patterns      []string // Patterns to subscribe to
		expectedMatch []string // Expected patterns that should match
	}{
		{
			"exact match",
			"parent.child.key",
			[]string{"**", "parent.*", "parent.child.*", "parent.child.key", "parent.*.key"},
			[]string{"**", "parent.child.*", "parent.child.key", "parent.*.key"}, // parent.* removed since it shouldn't match grandchildren
		},
		{
			"child wildcard",
			"parent.child.other",
			[]string{"**", "parent.*", "parent.child.*"},
			[]string{"**", "parent.child.*"}, // parent.* removed since it shouldn't match grandchildren
		},
		{
			"parent wildcard",
			"parent.sibling",
			[]string{"**", "parent.*"},
			[]string{"**", "parent.*"},
		},
		{
			"root key",
			"other",
			[]string{"**"},
			[]string{"**"},
		},
		{
			"middle wildcard",
			"parent.middle.key",
			[]string{"**", "parent.*", "parent.*.key"},
			[]string{"**", "parent.*.key"}, // parent.* removed since it shouldn't match grandchildren
		},
		{
			"multi wildcard",
			"a.x.b.y.c",
			[]string{"**", "a.*.b.*.c"},
			[]string{"**", "a.*.b.*.c"},
		},
		{
			"no match",
			"unrelated.key",
			[]string{"**"},
			[]string{"**"},
		},
		{
			"empty key",
			"",
			[]string{"**"},
			[]string{"**"},
		},
		{
			"multi wildcard match",
			"parent.child.grandchild",
			[]string{"parent.*.*"},
			[]string{"parent.*.*"},
		},
		{
			"multi wildcard different segments",
			"parent.other.descendant",
			[]string{"parent.*.*"},
			[]string{"parent.*.*"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create fresh config manager for each test
			cm := newTestManager()
			// Use production logger with no debug output
			cm.logger = zap.NewNop()

			// Track matched patterns
			var matchedPatterns []string
			var mu sync.Mutex

			// Subscribe to all patterns for this test case
			var unsubs []func()
			for _, pattern := range tc.patterns {
				unsub := cm.Subscribe(pattern, func(matchedPattern, key string, value any) {
					mu.Lock()
					matchedPatterns = append(matchedPatterns, pattern)
					mu.Unlock()
				})
				unsubs = append(unsubs, unsub)
			}

			// Trigger the change
			err := cm.Set(context.Background(), tc.key, "value")
			assert.NoError(t, err)

			// Verify matches
			assert.ElementsMatch(t, tc.expectedMatch, matchedPatterns,
				"key %q should match patterns %v", tc.key, tc.expectedMatch)

			// Clean up subscriptions
			for _, unsub := range unsubs {
				unsub()
			}
		})
	}

	t.Run("unsubscribe stops notifications", func(t *testing.T) {
		cm := newTestManager()
		cm.logger = zap.NewExample()

		var received bool
		unsub := cm.Subscribe("test.key", func(_, _ string, _ any) {
			received = true
		})

		// Unsubscribe immediately
		unsub()

		// Trigger change
		err := cm.Set(context.Background(), "test.key", "value")
		assert.NoError(t, err)

		// Should not have received notification
		assert.False(t, received)
	})
}

func TestConfigManager_SetGetExists(t *testing.T) {
	cm := newTestManager()

	// Test Set and Get with string
	err := cm.Set(context.Background(), "test.string", "test_value")
	assert.NoError(t, err)
	raw, decoded, err := cm.Get("test.string")
	assert.NoError(t, err)
	assert.Equal(t, "test_value", raw)
	assert.Equal(t, "test_value", decoded)

	// Test Set and Get with int
	err = cm.Set(context.Background(), "test.int", 123)
	assert.NoError(t, err)
	raw, decoded, err = cm.Get("test.int")
	assert.NoError(t, err)
	assert.Equal(t, 123, raw)
	assert.Equal(t, 123, decoded)

	// Test Set and Get with bool
	err = cm.Set(context.Background(), "test.bool", true)
	assert.NoError(t, err)
	raw, decoded, err = cm.Get("test.bool")
	assert.NoError(t, err)
	assert.Equal(t, true, raw)
	assert.Equal(t, true, decoded)

	// Test Exists
	assert.True(t, cm.Exists("test.string"))
	assert.False(t, cm.Exists("nonexistent.key"))

	// Test Get non-existent key
	raw, decoded, err = cm.Get("nonexistent.key")
	assert.Error(t, err)
	assert.Nil(t, raw)
	assert.Nil(t, decoded)
}

func TestConfigManager_All(t *testing.T) {
	cm := newTestManager()

	err := cm.Set(context.Background(), "test.string", "test_value")
	require.NoError(t, err)

	err = cm.Set(context.Background(), "test.int", 123)
	require.NoError(t, err)

	all := cm.All()
	assert.Equal(t, map[string]any{
		"test.string": "test_value",
		"test.int":    123,
	}, all)
}

func TestConfigManager_RegisterStructGet(t *testing.T) {
	t.Run("value registration with pointer target", func(t *testing.T) {
		cm := newTestManager()
		// Register the value type
		err := cm.RegisterStruct("test.struct", TestConfig{})
		assert.NoError(t, err)

		// Set values
		err = cm.Set(context.Background(), "test.struct.string_value", "struct_string")
		assert.NoError(t, err)
		err = cm.Set(context.Background(), "test.struct.int_value", 456)
		assert.NoError(t, err)

		// Get with pointer target
		var targetCfg TestConfig
		_, cfg, err := cm.Get("test.struct", &targetCfg)
		assert.NoError(t, err)
		assert.IsType(t, &TestConfig{}, cfg)
		assert.Equal(t, "struct_string", cfg.(*TestConfig).StringValue)
	})

	t.Run("value registration with nil target", func(t *testing.T) {
		cm := newTestManager()
		err := cm.RegisterStruct("test.struct", TestConfig{})
		assert.NoError(t, err)

		err = cm.Set(context.Background(), "test.struct.string_value", "nil_target")
		assert.NoError(t, err)

		// Get with nil target
		raw, decoded, err := cm.Get("test.struct", nil)
		assert.NoError(t, err)
		assert.IsType(t, map[string]interface{}{}, raw)
		assert.IsType(t, &TestConfig{}, decoded)
		assert.Equal(t, "nil_target", decoded.(*TestConfig).StringValue)
	})

	t.Run("value registration with value target", func(t *testing.T) {
		cm := newTestManager()
		err := cm.RegisterStruct("test.struct", TestConfig{})
		assert.NoError(t, err)

		err = cm.Set(context.Background(), "test.struct.string_value", "value_target")
		assert.NoError(t, err)

		// This should fail since we can't set into a value target
		var targetCfg TestConfig
		_, _, err = cm.Get("test.struct", targetCfg)
		assert.Error(t, err)
	})

	t.Run("pointer registration with pointer target", func(t *testing.T) {
		cm := newTestManager()
		// Register pointer type
		err := cm.RegisterStruct("test.struct", &TestConfig{})
		assert.NoError(t, err)

		err = cm.Set(context.Background(), "test.struct.string_value", "pointer_reg")
		require.NoError(t, err)

		var targetCfg TestConfig
		raw, decoded, err := cm.Get("test.struct", &targetCfg)
		assert.NoError(t, err)
		assert.IsType(t, map[string]interface{}{}, raw)
		assert.IsType(t, &TestConfig{}, decoded)
		assert.Equal(t, "pointer_reg", decoded.(*TestConfig).StringValue)
	})

	t.Run("pointer registration with nil target", func(t *testing.T) {
		cm := newTestManager()
		err := cm.RegisterStruct("test.struct", &TestConfig{})
		assert.NoError(t, err)

		err = cm.Set(context.Background(), "test.struct.string_value", "nil_ptr_target")
		assert.NoError(t, err)

		raw, decoded, err := cm.Get("test.struct", nil)
		assert.NoError(t, err)
		assert.IsType(t, map[string]interface{}{}, raw)
		assert.IsType(t, &TestConfig{}, decoded)
		assert.Equal(t, "nil_ptr_target", decoded.(*TestConfig).StringValue)
	})

	t.Run("type mismatch detection", func(t *testing.T) {
		cm := newTestManager()
		err := cm.RegisterStruct("test.struct", TestConfig{})
		assert.NoError(t, err)

		// Set some data first
		err = cm.Set(context.Background(), "test.struct.string_value", "test_value")
		assert.NoError(t, err)
		err = cm.Set(context.Background(), "test.struct.int_value", 123)
		assert.NoError(t, err)

		type MismatchConfig struct {
			Different string `config:"different"`
		}

		var target MismatchConfig
		_, _, err = cm.Get("test.struct", &target)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not match registered type")
	})
}

func TestConfigManager_TypeConversions(t *testing.T) {
	cm := newTestManager()

	type ConversionTestStruct struct {
		StringFromInt   string        `config:"string_from_int"`
		StringFromBool  string        `config:"string_from_bool"`
		IntFromString   int           `config:"int_from_string"`
		IntFromFloat    int           `config:"int_from_float"`
		FloatFromString float64       `config:"float_from_string"`
		FloatFromInt    float64       `config:"float_from_int"`
		BoolFromString  bool          `config:"bool_from_string"`
		BoolFromInt     bool          `config:"bool_from_int"`
		DurationFromInt time.Duration `config:"duration_from_int"`
		DurationFromStr time.Duration `config:"duration_from_str"`
		SliceFromString []string      `config:"slice_from_string"`
	}

	err := cm.RegisterStruct("test.conversions", ConversionTestStruct{})
	assert.NoError(t, err)

	// Set values with different types than the struct fields
	err = cm.Set(context.Background(), "test.conversions.string_from_int", 123)
	assert.NoError(t, err)
	err = cm.Set(context.Background(), "test.conversions.string_from_bool", true)
	assert.NoError(t, err)
	err = cm.Set(context.Background(), "test.conversions.int_from_string", "456")
	assert.NoError(t, err)
	err = cm.Set(context.Background(), "test.conversions.int_from_float", 789.0)
	assert.NoError(t, err)
	err = cm.Set(context.Background(), "test.conversions.float_from_string", "3.14")
	assert.NoError(t, err)
	err = cm.Set(context.Background(), "test.conversions.float_from_int", 42)
	assert.NoError(t, err)
	err = cm.Set(context.Background(), "test.conversions.bool_from_string", "true")
	assert.NoError(t, err)
	err = cm.Set(context.Background(), "test.conversions.bool_from_int", 1)
	assert.NoError(t, err)
	err = cm.Set(context.Background(), "test.conversions.duration_from_int", 60) // seconds
	assert.NoError(t, err)
	err = cm.Set(context.Background(), "test.conversions.duration_from_str", "2m")
	assert.NoError(t, err)
	err = cm.Set(context.Background(), "test.conversions.slice_from_string", "a,b,c")
	assert.NoError(t, err)

	// Get the struct
	var cfg ConversionTestStruct
	_, _, err = cm.Get("test.conversions", &cfg)
	assert.NoError(t, err)

	// Verify conversions
	assert.Equal(t, "123", cfg.StringFromInt)
	assert.Equal(t, "true", cfg.StringFromBool)
	assert.Equal(t, 456, cfg.IntFromString)
	assert.Equal(t, 789, cfg.IntFromFloat)
	assert.Equal(t, 3.14, cfg.FloatFromString)
	assert.Equal(t, 42.0, cfg.FloatFromInt)
	assert.True(t, cfg.BoolFromString)
	assert.True(t, cfg.BoolFromInt)
	assert.Equal(t, time.Minute, cfg.DurationFromInt)
	assert.Equal(t, 2*time.Minute, cfg.DurationFromStr)
	assert.Equal(t, []string{"a", "b", "c"}, cfg.SliceFromString)
}

func TestConfigManager_NestedStructConversions(t *testing.T) {
	cm := newTestManager()

	type NestedStruct struct {
		Value int `config:"value"`
	}

	type ParentStruct struct {
		Nested    NestedStruct  `config:"nested"`
		NestedPtr *NestedStruct `config:"nested_ptr"`
	}

	err := cm.RegisterStruct("test.nested", ParentStruct{})
	assert.NoError(t, err)

	// Set values and check for errors
	err = cm.Set(context.Background(), "test.nested.nested.value", "123") // string to int
	assert.NoError(t, err)
	err = cm.Set(context.Background(), "test.nested.nested_ptr.value", 456.0) // float to int
	assert.NoError(t, err)

	// Get the struct
	var cfg ParentStruct
	_, _, err = cm.Get("test.nested", &cfg)
	assert.NoError(t, err)

	// Verify conversions
	assert.Equal(t, 123, cfg.Nested.Value)
	assert.Equal(t, 456, cfg.NestedPtr.Value)
}

func TestConfigManager_findNearestStructKey(t *testing.T) {
	cm := newTestManager()

	type TestConfig struct {
		StringValue string `config:"string_value"`
	}

	err := cm.RegisterStruct("test", TestConfig{})
	assert.NoError(t, err)

	assert.Equal(t, "test", cm.findNearestStructKey("test.string_value"))
	assert.Equal(t, "test", cm.findNearestStructKey("test.string_value.nested"))
	assert.Equal(t, "", cm.findNearestStructKey("nonexistent.string_value"))
}

func TestConfigManager_SetAtomic(t *testing.T) {
	cm := newTestManager()

	// Set initial values
	err := cm.Set(context.Background(), "test.string", "initial_string")
	require.NoError(t, err)

	err = cm.Set(context.Background(), "test.int", 123)
	require.NoError(t, err)

	// Define updates
	updates := map[string]any{
		"test.string": "updated_string",
		"test.int":    456,
	}

	// Perform atomic update
	err = cm.SetAtomic(context.Background(), updates)
	assert.NoError(t, err)

	// Verify values
	val, _, _ := cm.Get("test.string")
	assert.Equal(t, "updated_string", val)
	val, _, _ = cm.Get("test.int")
	assert.Equal(t, 456, val)
}

// Register a struct for validation testing with schema
type testStruct struct {
	Name string `config:"name"`
	Age  int    `config:"age"`
}

func (t *testStruct) Schema() zog.ZogSchema {
	return zog.Struct(zog.Shape{
		"name": zog.String().Required().Min(1),
		"age":  zog.Int().Required().GT(0),
	})
}

func TestConfigManager_BulkSetAtomic(t *testing.T) {
	cm := newTestManager()

	// Register struct for validation
	err := cm.RegisterStruct("test.struct", testStruct{})
	require.NoError(t, err)

	// Define updates that would fail individual validation
	updates := map[string]any{
		"test.string":      "updated",
		"test.struct.name": "", // invalid empty name
		"test.struct.age":  25,
	}

	// Perform bulk atomic update - should fail validation
	err = cm.BulkSetAtomic(context.Background(), updates)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")

	// Verify values were NOT updated due to validation failure
	val, _, _ := cm.Get("test.string")
	assert.Equal(t, nil, val) // should remain initial value
	val, _, _ = cm.Get("test.struct.name")
	assert.Equal(t, nil, val)
	val, _, _ = cm.Get("test.struct.age")
	assert.Equal(t, nil, val)

	// Now try valid updates
	validUpdates := map[string]any{
		"test.string":      "updated",
		"test.struct.name": "valid_name",
		"test.struct.age":  25,
	}

	err = cm.BulkSetAtomic(context.Background(), validUpdates)
	assert.NoError(t, err)

	// Verify values were updated
	val, _, _ = cm.Get("test.string")
	assert.Equal(t, "updated", val)
	val, _, _ = cm.Get("test.struct.name")
	assert.Equal(t, "valid_name", val)
	val, _, _ = cm.Get("test.struct.age")
	assert.Equal(t, 25, val)
}

func TestConfigManager_BulkSet(t *testing.T) {
	cm := newTestManager()

	err := cm.RegisterStruct("test.struct", testStruct{})
	require.NoError(t, err)

	// Define updates that would fail individual validation
	updates := map[string]any{
		"test.string":      "updated",
		"test.struct.name": "", // invalid empty name
		"test.struct.age":  25,
	}

	// Perform bulk update - should fail validation but partial updates may occur
	err = cm.BulkSet(context.Background(), updates)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")

	// Verify values were partially updated before validation failure
	val, _, _ := cm.Get("test.string")
	assert.Equal(t, "updated", val) // simple value was updated
	val, _, _ = cm.Get("test.struct.name")
	assert.Equal(t, "", val) // invalid value was set
	val, _, _ = cm.Get("test.struct.age")
	assert.Equal(t, 25, val) // valid value was set

	// Now try valid updates
	validUpdates := map[string]any{
		"test.string":      "updated",
		"test.struct.name": "valid_name",
		"test.struct.age":  25,
	}

	err = cm.BulkSet(context.Background(), validUpdates)
	assert.NoError(t, err)

	// Verify values were updated
	val, _, _ = cm.Get("test.string")
	assert.Equal(t, "updated", val)
	val, _, _ = cm.Get("test.struct.name")
	assert.Equal(t, "valid_name", val)
	val, _, _ = cm.Get("test.struct.age")
	assert.Equal(t, 25, val)
}

func TestConfigManager_WithLogger(t *testing.T) {
	logger := zap.NewExample()
	cm := newTestManager()
	cm.logger = logger

	assert.Equal(t, logger, cm.logger)
}

func TestConfigManager_getFilteredKeys(t *testing.T) {
	cm := newTestManager()

	err := cm.Set(context.Background(), "test.string", "test_value")
	require.NoError(t, err)

	err = cm.Set(context.Background(), "test.int", 123)
	require.NoError(t, err)

	err = cm.Set(context.Background(), "other.value", true)
	require.NoError(t, err)

	// No prefix
	keys := cm.getFilteredKeys()
	assert.ElementsMatch(t, []string{"test.string", "test.int", "other.value"}, keys)

	// Single prefix
	keys = cm.getFilteredKeys("test")
	assert.ElementsMatch(t, []string{"test.string", "test.int"}, keys)

	// Multiple prefixes
	keys = cm.getFilteredKeys("test", "other")
	assert.ElementsMatch(t, []string{"test.string", "test.int", "other.value"}, keys)
}

func TestConfigManager_isVolatile(t *testing.T) {
	cm := newTestManager()

	cm.flagManager.SetFlags("test.volatile", []string{"volatile"})
	cm.flagManager.SetFlags("test.sync", []string{"sync"})

	assert.True(t, cm.isVolatile("test.volatile"))
	assert.False(t, cm.isVolatile("test.sync"))
	assert.False(t, cm.isVolatile("test.nonexistent"))
}

type AlwaysValid struct {
	Value string `config:"value"`
}

func (a *AlwaysValid) Validate() error {
	return nil
}

type AlwaysInvalid struct {
	Value string `config:"value"`
}

func (a *AlwaysInvalid) Validate() error {
	return fmt.Errorf("always invalid")
}

type LengthValidator struct {
	Value string `config:"value"`
}

func (l *LengthValidator) Validate() error {
	if len(l.Value) < 10 {
		return fmt.Errorf("value too short")
	}
	return nil
}

func newTestConfigManager(t *testing.T) *ConfigManagerDefault {
	cm := newTestManager()
	cm.logger = zap.NewNop()

	// Register test structs
	err := cm.RegisterStruct("test.always_valid", AlwaysValid{})
	assert.NoError(t, err)
	err = cm.RegisterStruct("test.always_invalid", AlwaysInvalid{})
	assert.NoError(t, err)
	err = cm.RegisterStruct("test.length_validator", LengthValidator{})
	assert.NoError(t, err)

	return cm
}

func TestConfigManager_Validate(t *testing.T) {
	t.Run("always valid passes validation", func(t *testing.T) {
		cm := newTestConfigManager(t)
		err := cm.Set(context.Background(), "test.always_valid.value", "any value")
		assert.NoError(t, err)
		assert.NoError(t, cm.Validate("test.always_valid"))
	})

	t.Run("always invalid fails validation", func(t *testing.T) {
		cm := newTestConfigManager(t)
		// First set a value directly in koanf to bypass validation
		err := cm.koanf.Set("test.always_invalid.value", "initial")
		assert.NoError(t, err)

		// Now try to validate - should fail
		err = cm.Validate("test.always_invalid")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "always invalid",
			"validation should fail with expected error")

		// Verify the value was not changed by validation
		raw, decoded, err := cm.Get("test.always_invalid.value")
		assert.NoError(t, err)
		assert.Equal(t, "initial", raw, "validation failure should not modify the value")
		assert.Equal(t, "initial", decoded, "validation failure should not modify the value")
	})

	t.Run("conditional validation - valid case", func(t *testing.T) {
		cm := newTestConfigManager(t)
		err := cm.Set(context.Background(), "test.length_validator.value", "long enough")
		assert.NoError(t, err)
		assert.NoError(t, cm.Validate("test.length_validator"))
	})

	t.Run("conditional validation - invalid case", func(t *testing.T) {
		cm := newTestConfigManager(t)
		err := cm.Set(context.Background(), "test.length_validator.value", "short")
		assert.Contains(t, err.Error(), "value too short")
		err = cm.Validate("test.length_validator")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "value too short")
	})

	t.Run("nonexistent key returns error", func(t *testing.T) {
		cm := newTestConfigManager(t)
		nonexistentKey := "test.nonexistent.key"

		// First verify the key doesn't exist
		assert.False(t, cm.Exists(nonexistentKey))

		// Validate should return an error
		err := cm.Validate(nonexistentKey)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.Contains(t, err.Error(), nonexistentKey)
	})
}

func TestConfigManager_implementsInterface(t *testing.T) {
	cm, _ := NewConfigManager([]source.ConfigSource{})

	type TestConfig struct {
		StringValue string `config:"string_value"`
	}

	err := cm.RegisterStruct("test", TestConfig{})
	require.NoError(t, err)

	validatorType := reflect.TypeOf((*Validator)(nil)).Elem()
	assert.False(t, cm.implementsInterface("test", validatorType))
}

func TestConfigManager_DeleteNotifiesWatchers(t *testing.T) {
	cm := newTestManagerWithData(map[string]any{
		"test.key": "test_value",
	})

	// Track if callback was called
	var callbackCalled bool
	var callbackKey string
	var callbackOldValue any
	var callbackNewValue any

	// Get the old value before setting up subscription
	oldValue, _, _ := cm.Get("test.key")

	// Subscribe to changes
	unsub := cm.Subscribe("test.key", func(pattern, key string, value any) {
		callbackCalled = true
		callbackKey = key
		callbackOldValue = oldValue
		callbackNewValue, _, _ = cm.Get("test.key") // Get current value after change
	})
	defer unsub()

	// Delete the key
	cm.Delete("test.key")

	// Verify callback was called with correct values
	assert.True(t, callbackCalled)
	assert.Equal(t, "test.key", callbackKey)
	assert.Equal(t, "test_value", callbackOldValue)
	assert.Nil(t, callbackNewValue)

	// Verify key was actually deleted
	val, _, err := cm.Get("test.key")
	assert.Error(t, err)
	assert.Nil(t, val)
}

type mockSyncManager struct {
	configured bool
	started    bool
	stopped    bool
	configure  func(cm csync.CManager, ns string, opts ...csync.SyncOption) error
	start      func(ctx context.Context) error
	stop       func() error
	push       func(ctx context.Context, key string, value any, callback csync.PushCallback) error
}

func (m *mockSyncManager) Configure(cm csync.CManager, ns string, opts ...csync.SyncOption) error {
	if m.configure != nil {
		return m.configure(cm, ns, opts...)
	}
	m.configured = true
	return nil
}

func (m *mockSyncManager) Start(ctx context.Context) error {
	if m.start != nil {
		return m.start(ctx)
	}
	m.started = true
	return nil
}

func (m *mockSyncManager) Stop() error {
	if m.stop != nil {
		return m.stop()
	}
	m.stopped = true
	return nil
}

func (m *mockSyncManager) Push(ctx context.Context, key string, value any, callback csync.PushCallback) error {
	if m.push != nil {
		return m.push(ctx, key, value, callback)
	}
	return nil
}

func TestConfigManager_SetupSync(t *testing.T) {
	t.Run("successful setup", func(t *testing.T) {
		cm := newTestManager()
		mockSync := &mockSyncManager{}
		cm.syncMgr = mockSync

		err := cm.SetupSync()
		assert.NoError(t, err)
		assert.True(t, mockSync.configured)
		assert.True(t, mockSync.started)
	})

	t.Run("configure error", func(t *testing.T) {
		cm := newTestManager()
		mockSync := &mockSyncManager{
			configure: func(cm csync.CManager, ns string, opts ...csync.SyncOption) error {
				return fmt.Errorf("configure error")
			},
		}
		cm.syncMgr = mockSync

		err := cm.SetupSync()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "configure error")
		assert.False(t, mockSync.started)
	})

	t.Run("start error", func(t *testing.T) {
		cm := newTestManager()
		mockSync := &mockSyncManager{
			start: func(ctx context.Context) error {
				return fmt.Errorf("start error")
			},
		}
		cm.syncMgr = mockSync

		err := cm.SetupSync()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "start error")
		assert.True(t, mockSync.configured)
	})

	t.Run("nil sync manager", func(t *testing.T) {
		cm := newTestManager()
		cm.syncMgr = nil

		err := cm.SetupSync()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sync manager is nil")
	})

	t.Run("with options", func(t *testing.T) {
		cm := newTestManager()
		mockSync := &mockSyncManager{}
		cm.syncMgr = mockSync

		// Option that changes sync config namespace
		opt := func(cm *ConfigManagerDefault) error {
			cm.syncConfigNS = "custom.sync.ns"
			return nil
		}

		err := cm.SetupSync(opt)
		assert.NoError(t, err)
		assert.Equal(t, "custom.sync.ns", cm.syncConfigNS)
		assert.True(t, mockSync.configured)
		assert.True(t, mockSync.started)
	})

	t.Run("option error", func(t *testing.T) {
		cm := newTestManager()
		mockSync := &mockSyncManager{}
		cm.syncMgr = mockSync

		// Option that returns error
		opt := func(cm *ConfigManagerDefault) error {
			return fmt.Errorf("option error")
		}

		err := cm.SetupSync(opt)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "option error")
		assert.False(t, mockSync.configured)
		assert.False(t, mockSync.started)
	})
}

func TestConfigManager_RegisterAndLoadSource(t *testing.T) {
	cm := newTestManager()

	// Create a new memory source with test data
	testData := map[string]any{
		"runtime.key": "runtime_value",
		"runtime.num": 42,
	}
	memSource := source.NewMemoryConfigSource(testData)

	// Register the source without loading
	cm.RegisterSource(memSource)
	assert.NotContains(t, cm.koanf.All(), "runtime.key")

	// Now explicitly load and watch it
	err := cm.LoadSource(memSource, true, true)
	assert.NoError(t, err)

	// Verify the new values are loaded
	assert.Equal(t, "runtime_value", cm.koanf.Get("runtime.key"))
	assert.Equal(t, 42, cm.koanf.Get("runtime.num"))

	// Test watching by updating the source
	updateChan := make(chan struct{})
	var once sync.Once
	cm.Subscribe("runtime.key", func(_, _ string, _ any) {
		once.Do(func() {
			close(updateChan)
		})
	})

	memSource.Set("runtime.key", "updated_value")

	// Wait for update notification with timeout
	select {
	case <-updateChan:
		assert.Equal(t, "updated_value", cm.koanf.Get("runtime.key"))
	case <-time.After(1 * time.Second):
		assert.Fail(t, "timeout waiting for config update")
	}

	// Test loading invalid source
	invalidSource := &source.TestingInvalidSource{}
	err = cm.LoadSource(invalidSource, true, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load source")
}

type invalidSource struct{}

func (i *invalidSource) Load(ctx context.Context, cm Manager) error {
	return fmt.Errorf("forced error")
}

func (i *invalidSource) Watch(ctx context.Context, cm Manager, cb source.WatchOnChangeCallback) error {
	return nil
}

func TestConfigManager_handleConfigChanges_MultipleKeys(t *testing.T) {
	cm := newTestManager()

	// Create memory source with test data
	memSource := source.NewMemoryConfigSource(map[string]any{
		"key1": "value1",
		"key2": "value2",
	})

	// First register the source
	cm.RegisterSource(memSource)
	// Then register the namespace
	cm.RegisterNamespace("test.ns", memSource)

	// Load initial config
	err := cm.LoadNamespace("test.ns")
	assert.NoError(t, err)

	// Track received changes
	var receivedChanges []string
	var mu sync.Mutex

	// Set up watch callback
	unsub := cm.Subscribe("test.ns.*", func(pattern, key string, value any) {
		mu.Lock()
		defer mu.Unlock()
		receivedChanges = append(receivedChanges, key)
	})
	defer unsub()

	// Simulate multiple changes from source
	memSource.Set("key1", "updated1")
	memSource.Set("key2", "updated2")

	// Wait for both changes to be received
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(receivedChanges) == 2
	}, 1*time.Second, 100*time.Millisecond)

	// Verify both keys were notified
	assert.Contains(t, receivedChanges, "test.ns.key1")
	assert.Contains(t, receivedChanges, "test.ns.key2")
}

func TestConfigManager_NamespaceKeyHandling(t *testing.T) {
	// Create a namespace that matches exactly one of our test keys
	ns := "plugin.test_plugin.protocol"
	memSource := source.NewMemoryConfigSource(map[string]any{
		"protocol":  "http", // Note: key is relative to namespace
		"other.key": "value",
	})

	cm, err := NewConfigManager([]source.ConfigSource{})
	require.NoError(t, err)

	// Register the source first
	cm.RegisterSource(memSource)
	// Then register the namespace
	cm.RegisterNamespace(ns, memSource)
	require.NoError(t, cm.LoadNamespace(ns))

	tests := []struct {
		name        string
		key         string
		wantErr     bool
		wantValue   any
		errContains string
	}{
		{
			name:      "exact namespace match",
			key:       ns,
			wantValue: map[string]interface{}{
				"protocol": "http",
				"other": map[string]interface{}{
					"key": "value",
				},
			},
		},
		{
			name:        "nested key under namespace",
			key:         ns + ".subkey",
			wantErr:     true,
			errContains: "not found",
		},
		{
			name:        "double dot at end",
			key:         ns + "..",
			wantErr:     true,
			errContains: "not found",
		},
		{
			name:        "double dot in middle",
			key:         "plugin..test_plugin.protocol",
			wantErr:     true,
			errContains: "not found",
		},
		{
			name:        "non-namespaced key",
			key:         "other.key",
			wantErr:     true,
			errContains: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, _, err := cm.Get(tt.key)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, val)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantValue, val)
			}
		})
	}
}

func TestConfigManager_GlobalSources(t *testing.T) {
	t.Run("global env source", func(t *testing.T) {
		// Create global env source
		envSrc := source.NewEnvConfigSource("TEST_", "_", source.WithEnvGlobal())
		
		// Create manager with the source
		cm, err := NewConfigManager([]source.ConfigSource{envSrc})
		require.NoError(t, err)

		// Set some env vars
		t.Setenv("TEST_DB_HOST", "localhost")
		t.Setenv("TEST_DB_PORT", "5432")

		// Load config
		err = cm.Load()
		require.NoError(t, err)

		// Verify keys are loaded without namespace
		assert.True(t, cm.Exists("db.host"))
		assert.Equal(t, "localhost", cm.koanf.Get("db.host"))
		assert.True(t, cm.Exists("db.port"))
		assert.Equal(t, "5432", cm.koanf.Get("db.port"))
	})

	t.Run("global default source", func(t *testing.T) {
		// Create manager first
		cm := newTestManager()

		// Create global default source
		defaults := map[string]any{
			"app.name": "TestApp",
			"app.port": 8000,
		}
		defaultSrc := source.NewDefaultConfigSource(cm, 
			source.WithDefaults(defaults),
			source.WithGlobal(true))

		// Register and load the source
		cm.RegisterSource(defaultSrc)
		err := cm.LoadSource(defaultSrc, true, false)
		require.NoError(t, err)

		// Verify keys are loaded without namespace
		assert.True(t, cm.Exists("app.name"))
		assert.Equal(t, "TestApp", cm.koanf.Get("app.name"))
		assert.True(t, cm.Exists("app.port"))
		assert.Equal(t, 8000, cm.koanf.Get("app.port"))
	})

	t.Run("non-global sources still use namespaces", func(t *testing.T) {
		// Create non-global env source with namespace
		envSrc := source.NewEnvConfigSource("TEST_", "_") // No WithEnvGlobal()
		
		// Create manager and register namespace
		cm, err := NewConfigManager([]source.ConfigSource{envSrc})
		require.NoError(t, err)
		cm.RegisterNamespace("test", envSrc)

		// Set some env vars
		t.Setenv("TEST_DB_HOST", "localhost")
		t.Setenv("TEST_DB_PORT", "5432")

		// Load config
		err = cm.Load()
		require.NoError(t, err)

		// Verify keys are loaded with namespace
		assert.True(t, cm.Exists("test.db.host"))
		assert.Equal(t, "localhost", cm.koanf.Get("test.db.host"))
		assert.True(t, cm.Exists("test.db.port"))
		assert.Equal(t, "5432", cm.koanf.Get("test.db.port"))
	})
}

func TestConfigManager_NamespaceRegistration(t *testing.T) {
	t.Run("basic namespace registration", func(t *testing.T) {
		cm := newTestManager()

		// Register source with namespace
		memSource := source.NewMemoryConfigSource(map[string]any{
			"key1": "value1",
		})
		cm.RegisterSource(memSource) // First register the source
		cm.RegisterNamespace("test.ns", memSource) // Then register namespace

		// Load should apply namespace
		err := cm.Load()
		assert.NoError(t, err)

		// Verify namespace applied
		val, _, err := cm.Get("test.ns.key1")
		assert.NoError(t, err)
		assert.Equal(t, "value1", val)

		// Verify source is properly registered
		nsSources := cm.RegisteredNamespaces()
		assert.Contains(t, nsSources, "test.ns")
		assert.Equal(t, memSource, nsSources["test.ns"])
	})

	t.Run("multiple namespaces", func(t *testing.T) {
		cm := newTestManager()

		src1 := source.NewMemoryConfigSource(map[string]any{"key": "value1"})
		src2 := source.NewMemoryConfigSource(map[string]any{"key": "value2"})

		cm.RegisterSource(src1) // Register sources first
		cm.RegisterSource(src2)
		cm.RegisterNamespace("ns1", src1)
		cm.RegisterNamespace("ns2", src2)

		err := cm.Load()
		assert.NoError(t, err)

		// Verify both namespaces loaded correctly
		val1, _, err := cm.Get("ns1.key")
		assert.NoError(t, err)
		assert.Equal(t, "value1", val1)

		val2, _, err := cm.Get("ns2.key")
		assert.NoError(t, err)
		assert.Equal(t, "value2", val2)

		// Verify both sources registered
		nsSources := cm.RegisteredNamespaces()
		assert.Contains(t, nsSources, "ns1")
		assert.Contains(t, nsSources, "ns2")
	})

	t.Run("duplicate namespace registration", func(t *testing.T) {
		cm := newTestManager()

		src1 := source.NewMemoryConfigSource(map[string]any{"key": "value1"})
		src2 := source.NewMemoryConfigSource(map[string]any{"key": "value2"})

		cm.RegisterSource(src1) // Register sources first
		cm.RegisterSource(src2)
		cm.RegisterNamespace("dupe", src1)
		cm.RegisterNamespace("dupe", src2) // Should overwrite

		err := cm.Load()
		assert.NoError(t, err)

		// Should use the last registered source
		val, _, err := cm.Get("dupe.key")
		assert.NoError(t, err)
		assert.Equal(t, "value2", val)
	})

	t.Run("empty namespace", func(t *testing.T) {
		cm := newTestManager()

		src := source.NewMemoryConfigSource(map[string]any{"key": "value"})
		cm.RegisterSource(src) // Register source first
		cm.RegisterNamespace("", src) // Empty namespace

		err := cm.Load()
		assert.NoError(t, err)

		// Keys should be loaded without namespace prefix
		val, _, err := cm.Get("key")
		assert.NoError(t, err)
		assert.Equal(t, "value", val)
	})

	t.Run("nested namespaces", func(t *testing.T) {
		cm := newTestManager()

		src1 := source.NewMemoryConfigSource(map[string]any{"key": "value1"})
		src2 := source.NewMemoryConfigSource(map[string]any{"key": "value2"})

		cm.RegisterSource(src1) // Register sources first
		cm.RegisterSource(src2)
		cm.RegisterNamespace("parent.child1", src1)
		cm.RegisterNamespace("parent.child2", src2)

		err := cm.Load()
		assert.NoError(t, err)

		// Verify nested namespaces loaded correctly
		val1, _, err := cm.Get("parent.child1.key")
		assert.NoError(t, err)
		assert.Equal(t, "value1", val1)

		val2, _, err := cm.Get("parent.child2.key")
		assert.NoError(t, err)
		assert.Equal(t, "value2", val2)

		// Verify parent namespace isn't registered as a source
		nsSources := cm.RegisteredNamespaces()
		assert.NotContains(t, nsSources, "parent")
		assert.Contains(t, nsSources, "parent.child1")
		assert.Contains(t, nsSources, "parent.child2")
	})
}

func TestConfigManager_UnregisterNamespace(t *testing.T) {
	cm := newTestManager()

	memSource := source.NewMemoryConfigSource(map[string]any{"key": "value"})

	// First register the source with the manager
	cm.RegisterSource(memSource)
	// Then register the namespace
	cm.RegisterNamespace("test.ns", memSource)

	// Load and verify
	err := cm.Load()
	assert.NoError(t, err)
	assert.True(t, cm.Exists("test.ns.key"))

	// Unregister
	err = cm.UnregisterNamespace("test.ns")
	assert.NoError(t, err)

	// Verify keys removed
	assert.False(t, cm.Exists("test.ns.key"))
	assert.NotContains(t, cm.RegisteredNamespaces(), "test.ns")

	// Verify source was not removed from sources list
	assert.True(t, lo.ContainsBy(cm.sources, func(s source.ConfigSource) bool {
		return s == memSource
	}))
}

func TestConfigManager_LoadNamespace(t *testing.T) {
	cm := newTestManager()

	src1 := source.NewMemoryConfigSource(map[string]any{"key": "value1"})
	src2 := source.NewMemoryConfigSource(map[string]any{"key": "value2"})

	cm.RegisterNamespace("ns1", src1)
	cm.RegisterNamespace("ns2", src2)

	// Load just one namespace
	err := cm.LoadNamespace("ns1")
	assert.NoError(t, err)

	// Verify only ns1 loaded
	assert.True(t, cm.Exists("ns1.key"))
	assert.False(t, cm.Exists("ns2.key"))

	// Now load the other namespace
	err = cm.LoadNamespace("ns2")
	assert.NoError(t, err)
	assert.True(t, cm.Exists("ns2.key"))
}

func TestConfigManager_loadSource_WithNamespace(t *testing.T) {
	cm := newTestManager()

	// Register a source with namespace
	memSource := source.NewMemoryConfigSource(map[string]any{
		"key1": "value1",
		"key2": "value2",
	})
	cm.RegisterNamespace("test.ns", memSource)

	err := cm.loadSource(memSource)
	assert.NoError(t, err)

	// Verify keys are namespaced
	val, _, err := cm.Get("test.ns.key1")
	assert.NoError(t, err)
	assert.Equal(t, "value1", val)

	val, _, err = cm.Get("test.ns.key2")
	assert.NoError(t, err)
	assert.Equal(t, "value2", val)
}

func TestConfigManager_loadSource_NoNamespace(t *testing.T) {
	cm := newTestManager()

	// Source without namespace
	memSource := source.NewMemoryConfigSource(map[string]any{
		"key1": "value1",
	})

	err := cm.loadSource(memSource)
	assert.NoError(t, err)

	// Verify keys are not namespaced
	val, _, err := cm.Get("key1")
	assert.NoError(t, err)
	assert.Equal(t, "value1", val)
}

func TestConfigManager_ValidationControl(t *testing.T) {
	cm := newTestManager()

	// Register a struct that requires validation
	err := cm.RegisterStruct("test.validation", testStruct{})
	require.NoError(t, err)

	// Validation should be enabled by default
	assert.True(t, cm.ValidationEnabled())

	// Try setting invalid data - should fail
	err = cm.Set(context.Background(), "test.validation.name", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")

	// Disable validation
	cm.DisableValidation()
	assert.False(t, cm.ValidationEnabled())

	// Now setting invalid data should succeed
	err = cm.Set(context.Background(), "test.validation.name", "")
	assert.NoError(t, err)

	// Re-enable validation
	cm.EnableValidation()
	assert.True(t, cm.ValidationEnabled())

	// Setting invalid data should fail again
	err = cm.Set(context.Background(), "test.validation.age", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestConfigManager_ValidateRegisteredStructs(t *testing.T) {
	cm := newTestManager()

	// Register test structs
	err := cm.RegisterStruct("test.valid", testStruct{})
	require.NoError(t, err)
	err = cm.RegisterStruct("test.invalid", testStruct{})
	require.NoError(t, err)

	// Set valid data for first struct
	err = cm.BulkSet(context.Background(), map[string]any{
		"test.valid.name": "valid",
		"test.valid.age":  25,
	})
	require.NoError(t, err)

	// Set invalid data for second struct (bypassing validation)
	cm.DisableValidation()
	err = cm.BulkSet(context.Background(), map[string]any{
		"test.invalid.name": "",
		"test.invalid.age":  0,
	})
	require.NoError(t, err)
	cm.EnableValidation()

	// Validate should fail due to invalid struct
	err = cm.ValidateRegisteredStructs()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed for struct test.invalid")
	assert.Contains(t, err.Error(), "name")
	assert.Contains(t, err.Error(), "age")

	// Fix invalid struct and validate again
	err = cm.BulkSet(context.Background(), map[string]any{
		"test.invalid.name": "fixed",
		"test.invalid.age":  1,
	})
	require.NoError(t, err)

	err = cm.ValidateRegisteredStructs()
	assert.NoError(t, err)
}

type rootConfig struct {
	AppName string `config:"app_name"`
	Debug   bool   `config:"debug"`
}

func TestConfigManager_Root(t *testing.T) {
	cm := newTestManager()
	err := cm.RegisterStruct("", rootConfig{})
	require.NoError(t, err)

	// Set some values
	err = cm.Set(context.Background(), "app_name", "test_app")
	require.NoError(t, err)
	err = cm.Set(context.Background(), "debug", true)
	require.NoError(t, err)

	// Test with target
	var target rootConfig
	rootCfg, err := cm.Root(&target)
	require.NoError(t, err)
	assert.Equal(t, "test_app", target.AppName)
	assert.Equal(t, true, target.Debug)
	assert.Equal(t, &target, rootCfg)

	// Test without target
	rootCfg, err = cm.Root(nil)
	require.NoError(t, err)
	assert.IsType(t, &rootConfig{}, rootCfg)
	assert.Equal(t, "test_app", rootCfg.(*rootConfig).AppName)
	assert.Equal(t, true, rootCfg.(*rootConfig).Debug)

	// Test error when no root struct registered
	cm2 := newTestManager()
	_, err = cm2.Root(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no root configuration struct registered")

	// Test root struct decoding
	cm3 := newTestManager()
	err = cm3.RegisterStruct("", rootConfig{})
	require.NoError(t, err)

	// Set some values
	err = cm3.Set(context.Background(), "app_name", "test_app")
	require.NoError(t, err)
	err = cm3.Set(context.Background(), "debug", true)
	require.NoError(t, err)

	// Test Root() with nil target
	rootCfg, err = cm3.Root(nil)
	require.NoError(t, err)
	assert.IsType(t, &rootConfig{}, rootCfg)
	assert.Equal(t, "test_app", rootCfg.(*rootConfig).AppName)
	assert.Equal(t, true, rootCfg.(*rootConfig).Debug)
}

func TestConfigManager_ValidateWithZogSchema(t *testing.T) {
	cm, _ := NewConfigManager([]source.ConfigSource{})
	logger := zap.NewNop()
	cm.logger = logger

	// Register the struct
	err := cm.RegisterStruct("test.schema", SchemaValidatedConfig{})
	assert.NoError(t, err)

	t.Run("valid config passes schema validation", func(t *testing.T) {
		err := cm.Set(context.Background(), "test.schema.email", "valid@example.com")
		assert.NoError(t, err)
		err = cm.Set(context.Background(), "test.schema.password", "Password123")
		assert.NoError(t, err)

		err = cm.Validate("test.schema")
		assert.NoError(t, err)
	})

	t.Run("invalid email fails schema validation", func(t *testing.T) {
		err := cm.Set(context.Background(), "test.schema.email", "invalid-email")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "email") // Check for email field error
		assert.Contains(t, err.Error(), "valid") // Check for validation keyword

		// Verify the invalid value wasn't actually set
		raw, decoded, err := cm.Get("test.schema.email")
		assert.NoError(t, err)
		assert.NotEqual(t, "invalid-email", raw)
		assert.NotEqual(t, "invalid-email", decoded)
	})

	t.Run("password without digit fails schema validation", func(t *testing.T) {
		err := cm.Set(context.Background(), "test.schema.email", "valid@example.com")
		assert.NoError(t, err)
		err = cm.Set(context.Background(), "test.schema.password", "weak")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "password") // Check for password field
		assert.Contains(t, err.Error(), "digit")    // Check for digit requirement

		// Verify the invalid value wasn't actually set
		raw, decoded, err := cm.Get("test.schema.password")
		assert.NoError(t, err)
		assert.NotEqual(t, "weak", raw)
		assert.NotEqual(t, "weak", decoded)
	})

	t.Run("partial validation shows all errors", func(t *testing.T) {
		// First set valid values
		err := cm.Set(context.Background(), "test.schema.email", "valid@example.com")
		assert.NoError(t, err)
		err = cm.Set(context.Background(), "test.schema.password", "Valid1234")
		assert.NoError(t, err)

		// Then try to set invalid values - these should fail validation
		err = cm.Set(context.Background(), "test.schema.email", "invalid")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "email")

		err = cm.Set(context.Background(), "test.schema.password", "weak")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "password")
		// The validation error may show either requirement depending on which fails first
		assert.True(t, strings.Contains(err.Error(), "uppercase") || strings.Contains(err.Error(), "digit"),
			"error should mention either uppercase or digit requirement")

		// Verify original valid values remain unchanged
		raw, decoded, err := cm.Get("test.schema.email")
		assert.NoError(t, err)
		assert.Equal(t, "valid@example.com", raw)
		assert.Equal(t, "valid@example.com", decoded)

		raw, decoded, err = cm.Get("test.schema.password")
		assert.NoError(t, err)
		assert.Equal(t, "Valid1234", raw)
		assert.Equal(t, "Valid1234", decoded)
	})
}
