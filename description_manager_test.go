package configmanager

import (
	"sync"
	"testing"
)

// Test helpers
func setupTestManager(t *testing.T, config any) Manager {
	t.Helper()
	cm, err := NewConfigManager(
		UsingSources(),
		WithConfigStruct("", config),
	)
	if err != nil {
		t.Fatalf("Failed to create ConfigManager: %v", err)
	}
	return cm
}

func assertDescription(t *testing.T, dm DescriptionManager, key, expected string) {
	t.Helper()
	if desc := dm.GetDescription(key); desc != expected {
		t.Errorf("Expected '%s' for key '%s', got '%s'", expected, key, desc)
	}
}

// DescriptionManager implementation tests
func TestNewDescriptionManager(t *testing.T) {
	dm := NewDescriptionManager()
	if dm == nil {
		t.Fatal("NewDescriptionManager returned nil")
	}
}

func TestDescriptionManager_SetAndGetDescription(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		desc     string
		expected string
	}{
		{
			name:     "set and get description",
			key:      "test.key",
			desc:     "Test description",
			expected: "Test description",
		},
		{
			name:     "get non-existent key returns empty string",
			key:      "nonexistent",
			desc:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dm := NewDescriptionManager()
			if tt.desc != "" {
				dm.SetDescription(tt.key, tt.desc)
			}
			assertDescription(t, dm, tt.key, tt.expected)
		})
	}
}

func TestDescriptionManager_SetDescriptions(t *testing.T) {
	dm := NewDescriptionManager()

	descriptions := map[string]string{
		"key1": "Description 1",
		"key2": "Description 2",
		"key3": "Description 3",
	}

	dm.SetDescriptions(descriptions)

	for key, expectedDesc := range descriptions {
		assertDescription(t, dm, key, expectedDesc)
	}
}

func TestDescriptionManager_GetAllDescriptions(t *testing.T) {
	dm := NewDescriptionManager()

	dm.SetDescription("key1", "Description 1")
	dm.SetDescription("key2", "Description 2")

	all := dm.GetAllDescriptions()

	if len(all) != 2 {
		t.Errorf("Expected 2 descriptions, got %d", len(all))
	}

	assertDescription(t, dm, "key1", "Description 1")
	assertDescription(t, dm, "key2", "Description 2")

	// Verify that modifying the returned map doesn't affect the internal state
	all["key3"] = "Description 3"
	if dm.GetDescription("key3") != "" {
		t.Error("Modifying returned map should not affect internal state")
	}
}

func TestDescriptionManager_GetDescriptionsForPrefix(t *testing.T) {
	dm := NewDescriptionManager()

	dm.SetDescription("database.host", "Database host")
	dm.SetDescription("database.port", "Database port")
	dm.SetDescription("database.user", "Database user")
	dm.SetDescription("app.name", "Application name")
	dm.SetDescription("app.port", "Application port")

	tests := []struct {
		name          string
		prefix        string
		expectedCount int
	}{
		{
			name:          "prefix matching",
			prefix:        "database",
			expectedCount: 3,
		},
		{
			name:          "exact key match",
			prefix:        "database.host",
			expectedCount: 1,
		},
		{
			name:          "empty prefix returns all",
			prefix:        "",
			expectedCount: 5,
		},
		{
			name:          "non-existent prefix",
			prefix:        "nonexistent",
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			descs := dm.GetDescriptionsForPrefix(tt.prefix)
			if len(descs) != tt.expectedCount {
				t.Errorf("Expected %d descriptions for prefix '%s', got %d", tt.expectedCount, tt.prefix, len(descs))
			}
		})
	}
}

func TestDescriptionManager_ConcurrentAccess(t *testing.T) {
	dm := NewDescriptionManager()
	var wg sync.WaitGroup

	// Test concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "concurrent.key." + string(rune('a'+n%26))
			dm.SetDescription(key, "Description "+string(rune('a'+n%26)))
		}(i)
	}

	wg.Wait()

	// Verify all writes
	for i := 0; i < 100; i++ {
		key := "concurrent.key." + string(rune('a'+i%26))
		if dm.GetDescription(key) == "" {
			t.Errorf("Concurrent write failed for key %s", key)
		}
	}

	// Test concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "concurrent.key." + string(rune('a'+n%26))
			_ = dm.GetDescription(key)
		}(i)
	}

	wg.Wait()
}

// Struct description extraction tests
func TestExtractStructDescriptions(t *testing.T) {
	type NestedConfig struct {
		Field1 string `config:"field1" desc:"Nested field 1"`
		Field2 int    `config:"field2" desc:"Nested field 2"`
	}

	type TestConfig struct {
		SimpleField string        `config:"simple" desc:"Simple field description"`
		NumberField int           `config:"number" desc:"Number field description"`
		Nested      NestedConfig  `config:"nested" desc:"Nested config"`
		PtrNested   *NestedConfig `config:"ptr_nested" desc:"Pointer to nested config"`
		NoDesc      string        `config:"no_desc"`
		unexported  string        `config:"unexported" desc:"This should be ignored"`
	}

	cm := setupTestManager(t, TestConfig{})
	dm := cm.DescriptionManager()

	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{"simple field", "simple", "Simple field description"},
		{"number field", "number", "Number field description"},
		{"nested field", "nested.field1", "Nested field 1"},
		{"pointer to nested field", "ptr_nested.field2", "Nested field 2"},
		{"field without description", "no_desc", ""},
		{"unexported field", "unexported", ""},
		{"nested config description", "nested", "Nested config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertDescription(t, dm, tt.key, tt.expected)
		})
	}
}

// Manager interface description tests
func TestWithDescriptions(t *testing.T) {
	descriptions := map[string]string{
		"manual.key1": "Manual description 1",
		"manual.key2": "Manual description 2",
	}

	cm, err := NewConfigManager(
		UsingSources(),
		WithDescriptions(descriptions),
	)
	if err != nil {
		t.Fatalf("Failed to create ConfigManager: %v", err)
	}

	dm := cm.DescriptionManager()
	assertDescription(t, dm, "manual.key1", "Manual description 1")
	assertDescription(t, dm, "manual.key2", "Manual description 2")
}

func TestGetDescription(t *testing.T) {
	type TestConfig struct {
		Field string `config:"field" desc:"Field description"`
	}

	cm := setupTestManager(t, TestConfig{})
	assertDescription(t, cm, "field", "Field description")
	assertDescription(t, cm, "nonexistent", "")
}

func TestGetAllDescriptions(t *testing.T) {
	type TestConfig struct {
		Field1 string `config:"field1" desc:"Description 1"`
		Field2 string `config:"field2" desc:"Description 2"`
	}

	cm, err := NewConfigManager(
		UsingSources(),
		WithConfigStruct("", TestConfig{}),
		WithDescriptions(map[string]string{
			"manual.key": "Manual description",
		}),
	)
	if err != nil {
		t.Fatalf("Failed to create ConfigManager: %v", err)
	}

	all := cm.GetAllDescriptions()

	if len(all) != 3 {
		t.Errorf("Expected 3 descriptions, got %d", len(all))
	}

	assertDescription(t, cm, "field1", "Description 1")
	assertDescription(t, cm, "manual.key", "Manual description")
}

func TestGetDescriptionsForPrefix(t *testing.T) {
	type TestConfig struct {
		DatabaseHost string `config:"database.host" desc:"DB host"`
		DatabasePort int    `config:"database.port" desc:"DB port"`
		AppName      string `config:"app.name" desc:"App name"`
	}

	cm := setupTestManager(t, TestConfig{})

	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{"database.host description", "database.host", "DB host"},
		{"database.port description", "database.port", "DB port"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertDescription(t, cm, tt.key, tt.expected)
		})
	}

	dbDescs := cm.GetDescriptionsForPrefix("database")
	if len(dbDescs) != 2 {
		t.Errorf("Expected 2 descriptions for 'database' prefix, got %d", len(dbDescs))
	}
	if dbDescs["database.host"] != "DB host" {
		t.Errorf("Expected 'DB host', got '%s'", dbDescs["database.host"])
	}
}

func TestDescriptionInErrorMessages(t *testing.T) {
	type TestConfig struct {
		Field string `config:"field" desc:"This is a test field"`
	}

	cm := setupTestManager(t, TestConfig{})

	_, _, err := cm.Get("nonexistent")
	if err == nil {
		t.Fatal("Expected error for non-existent key")
	}

	errMsg := err.Error()
	if errMsg == "" {
		t.Fatal("Error message should not be empty")
	}

	// The error should mention the key
	if !contains(errMsg, "nonexistent") {
		t.Errorf("Error message should contain the key name, got: %s", errMsg)
	}
}

// Manager SetDescription/SetDescriptions tests
func TestManager_SetDescription(t *testing.T) {
	type TestConfig struct {
		Field string `config:"field" desc:"Original description"`
	}

	cm := setupTestManager(t, TestConfig{})

	// Verify initial description from struct tag
	assertDescription(t, cm, "field", "Original description")

	// Set a new description at runtime
	cm.SetDescription("field", "Updated description")
	assertDescription(t, cm, "field", "Updated description")

	// Set description for a new key that wasn't in the struct
	cm.SetDescription("new.key", "New key description")
	assertDescription(t, cm, "new.key", "New key description")
}

func TestManager_SetDescriptions(t *testing.T) {
	type TestConfig struct {
		Field1 string `config:"field1" desc:"Original field 1"`
		Field2 string `config:"field2" desc:"Original field 2"`
	}

	cm := setupTestManager(t, TestConfig{})

	// Verify initial descriptions
	assertDescription(t, cm, "field1", "Original field 1")
	assertDescription(t, cm, "field2", "Original field 2")

	// Set multiple descriptions at once
	newDescriptions := map[string]string{
		"field1":   "Updated field 1",
		"field2":   "Updated field 2",
		"new.key1": "New key 1",
		"new.key2": "New key 2",
	}

	cm.SetDescriptions(newDescriptions)

	// Verify all descriptions were updated
	for key, expectedDesc := range newDescriptions {
		assertDescription(t, cm, key, expectedDesc)
	}

	// Verify struct descriptions are updated
	assertDescription(t, cm, "field1", "Updated field 1")
	assertDescription(t, cm, "field2", "Updated field 2")
}

func TestManager_SetDescription_WithNamespaces(t *testing.T) {
	cm := setupTestManager(t, struct{}{})

	// Set descriptions with different namespaces
	cm.SetDescription("database.host", "Database host")
	cm.SetDescription("database.port", "Database port")
	cm.SetDescription("app.name", "Application name")

	// Verify descriptions are accessible
	assertDescription(t, cm, "database.host", "Database host")
	assertDescription(t, cm, "database.port", "Database port")
	assertDescription(t, cm, "app.name", "Application name")

	// Get descriptions by prefix
	dbDescs := cm.GetDescriptionsForPrefix("database")
	if len(dbDescs) != 2 {
		t.Errorf("Expected 2 descriptions for 'database' prefix, got %d", len(dbDescs))
	}
	if dbDescs["database.host"] != "Database host" {
		t.Errorf("Expected 'Database host', got '%s'", dbDescs["database.host"])
	}
}

func TestManager_SetDescription_OverwriteStructTag(t *testing.T) {
	type TestConfig struct {
		Field string `config:"field" desc:"Struct tag description"`
	}

	cm := setupTestManager(t, TestConfig{})

	// Verify initial description from struct tag
	assertDescription(t, cm, "field", "Struct tag description")

	// Overwrite with runtime description
	cm.SetDescription("field", "Runtime description")
	assertDescription(t, cm, "field", "Runtime description")
}

func TestManager_SetDescription_Concurrent(t *testing.T) {
	cm := setupTestManager(t, struct{}{})
	var wg sync.WaitGroup

	// Test concurrent SetDescription calls
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "concurrent.key." + string(rune('a'+n%26))
			cm.SetDescription(key, "Description "+string(rune('a'+n%26)))
		}(i)
	}

	wg.Wait()

	// Verify all descriptions were set
	dm := cm.DescriptionManager()
	for i := 0; i < 100; i++ {
		key := "concurrent.key." + string(rune('a'+i%26))
		if dm.GetDescription(key) == "" {
			t.Errorf("Concurrent SetDescription failed for key %s", key)
		}
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
