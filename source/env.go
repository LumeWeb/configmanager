package source

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ArrayStrategy defines how environment variable values are parsed as arrays.
type ArrayStrategy int

const (
	// ArrayStrategyAuto automatically detects the best parsing strategy.
	// Tries in order: index-based → delimited (comma, space) → JSON arrays.
	ArrayStrategyAuto ArrayStrategy = iota

	// ArrayStrategyIndex uses index-based environment variables (APP_KEY_0, APP_KEY_1).
	// Clear ordering but requires multiple env vars.
	ArrayStrategyIndex

	// ArrayStrategyDelimited uses a delimiter to split values (e.g., "value1,value2,value3").
	// Delimiter defaults to comma but can be configured.
	ArrayStrategyDelimited

	// ArrayStrategyJSON parses values as JSON arrays.
	// Example: '["value1","value2"]' → []string{"value1", "value2"}
	ArrayStrategyJSON
)

// EnvConfigSource loads configuration from environment variables.
type EnvConfigSource struct {
	prefix         string
	delimiter      string
	global         bool                      // Controls whether this source should be loaded globally
	arrayStrategy  ArrayStrategy             // Strategy for parsing array values
	arrayDelimiter string                    // Delimiter for array parsing (default: comma)
	environFunc    func() []string           // Optional env override for testing
	transformFunc  func(k, v string) (string, any) // Custom transform callback
}

// IsGlobal implements GlobalConfigSource
func (e *EnvConfigSource) IsGlobal() bool {
	return e.global
}

// NewEnvConfigSource creates a new EnvConfigSource with optional prefix and delimiter.
// The prefix is prepended to environment variable names (e.g. "APP_").
// The delimiter is used to split nested keys (e.g. "_" for "APP_DB_HOST").
type EnvConfigOption func(*EnvConfigSource)

func WithEnvSourceGlobal() EnvConfigOption {
	return func(e *EnvConfigSource) {
		e.global = true
	}
}

// WithEnvSourceArrayStrategy configures how array values are parsed from environment variables.
func WithEnvSourceArrayStrategy(strategy ArrayStrategy, delimiter string) EnvConfigOption {
	return func(e *EnvConfigSource) {
		e.arrayStrategy = strategy
		if delimiter != "" {
			e.arrayDelimiter = delimiter
		} else {
			e.arrayDelimiter = ","
		}
	}
}

// WithEnvEnvironFunc provides a custom environment variable function (for testing).
func WithEnvEnvironFunc(fn func() []string) EnvConfigOption {
	return func(e *EnvConfigSource) {
		e.environFunc = fn
	}
}

// WithEnvTransformFunc provides a custom transform callback.
func WithEnvTransformFunc(fn func(k, v string) (string, any)) EnvConfigOption {
	return func(e *EnvConfigSource) {
		e.transformFunc = fn
	}
}

func NewEnvConfigSource(prefix, delimiter string, opts ...EnvConfigOption) *EnvConfigSource {
	e := &EnvConfigSource{
		prefix:         prefix,
		delimiter:      delimiter,
		arrayDelimiter: ",", // Default delimiter for arrays
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Load loads the configuration from environment variables into the config manager.
func (e *EnvConfigSource) Load(ctx context.Context, cm configManager) error {
	if cm == nil {
		return fmt.Errorf("config manager cannot be nil")
	}

	// Get all environment variables
	environ := e.environFunc
	if environ == nil {
		environ = os.Environ
	}

	// Collect env vars with optional prefix
	allEnvVars := environ()
	
	// Build map of key -> value, filtering by prefix
	envVars := make(map[string]string)
	for _, kv := range allEnvVars {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := parts[1]
		if e.prefix != "" && !strings.HasPrefix(key, e.prefix) {
			continue
		}
		envVars[key] = value
	}

	// Process environment variables
	transform := e.transformFunc
	if transform == nil {
		// Default transform if none provided
		transform = func(k, v string) (string, any) {
			if e.prefix != "" {
				k = strings.TrimPrefix(k, e.prefix)
			}
			k = strings.ToLower(k)
			if e.delimiter != "" {
				k = strings.ReplaceAll(k, e.delimiter, cm.Delim())
			}
			return k, v
		}
	}

	// Collect transformed values
	transformed := make(map[string]any)
	for key, value := range envVars {
		transformedKey, transformedValue := transform(key, value)
		if transformedKey == "" {
			continue
		}

		if strValue, ok := transformedValue.(string); ok {
			transformedValue = e.tryParseArray(transformedKey, strValue)
		}
		transformed[transformedKey] = transformedValue
	}

	// Merge index-based arrays (process after individual vars)
	e.mergeIndexBasedArrays(envVars, transform, transformed)

	// Set values through config manager to trigger validation
	for key, value := range transformed {
		if err := cm.Set(ctx, key, value); err != nil {
			return err
		}
	}

	return nil
}

// Watch watches for changes in the environment variables and triggers the onChange function when a change occurs.
// Environment variables cannot be watched in a cross-platform way, so this is a no-op.
func (e *EnvConfigSource) Watch(_ context.Context, _ configManager, _ WatchOnChangeCallback) error {
	// Environment variables cannot be watched in a cross-platform way
	return nil
}

// tryParseArray attempts to parse a value as an array based on the configured strategy.
func (e *EnvConfigSource) tryParseArray(key, value string) any {
	result, parsed := e.parseAsArray(value)
	if parsed {
		return result
	}
	return value
}

// parseAsArray tries to parse a value as an array using the configured strategies.
// Returns the parsed array (if successful) and whether parsing succeeded.
func (e *EnvConfigSource) parseAsArray(value string) ([]string, bool) {
	// Order of attempts depends on strategy
	var strategies []func(string) ([]string, bool)

	switch e.arrayStrategy {
	case ArrayStrategyAuto:
		strategies = []func(string) ([]string, bool){
			e.tryParseJSONArray,
			e.tryParseDelimitedArray,
		}
	case ArrayStrategyDelimited:
		strategies = []func(string) ([]string, bool){
			e.tryParseDelimitedArray,
		}
	case ArrayStrategyJSON:
		strategies = []func(string) ([]string, bool){
			e.tryParseJSONArray,
		}
	case ArrayStrategyIndex:
		// Handled separately in mergeIndexBasedArrays
		return nil, false
	default:
		return nil, false
	}

	for _, parseFunc := range strategies {
		if result, ok := parseFunc(value); ok {
			return result, true
		}
	}

	return nil, false
}

// tryParseDelimitedArray parses a delimited string into an array.
func (e *EnvConfigSource) tryParseDelimitedArray(value string) ([]string, bool) {
	if value == "" {
		return nil, false
	}

	// Try configured delimiter first, then common alternatives
	// Note: colon ":" is excluded as it causes false positives with URLs and IP addresses
	delimiters := []string{",", " ", "|", ";"}

	// Insert configured delimiter first if not empty and different from defaults
	if e.arrayDelimiter != "" {
		delimiters = append([]string{e.arrayDelimiter}, delimiters...)
	}

	for _, delim := range delimiters {
		if delim == "" {
			continue
		}
		if strings.Contains(value, delim) {
			parts := strings.Split(value, delim)
			if len(parts) > 1 {
				return cleanParts(parts), true
			}
		}
	}

	return nil, false
}

// tryParseJSONArray attempts to parse a JSON array.
func (e *EnvConfigSource) tryParseJSONArray(value string) ([]string, bool) {
	value = strings.TrimSpace(value)
	if !isJSONArray(value) {
		return nil, false
	}

	content := value[1 : len(value)-1]
	if content == "" {
		return []string{}, true
	}

	parts := strings.Split(content, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if unquoted, err := strconv.Unquote(part); err == nil {
			result = append(result, unquoted)
		}
	}

	if len(result) > 0 {
		return result, true
	}

	return nil, false
}

// isJSONArray checks if a string looks like a JSON array.
func isJSONArray(s string) bool {
	return strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]")
}

// cleanParts trims whitespace and removes empty strings.
func cleanParts(parts []string) []string {
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}


// mergeIndexBasedArrays merges index-based environment variables into arrays.
// For example: APP_HOSTS_0=site1, APP_HOSTS_1=site2 → hosts: ["site1", "site2"]
func (e *EnvConfigSource) mergeIndexBasedArrays(envValues map[string]string, transform func(k, v string) (string, any), result map[string]any) {
	if !e.shouldUseIndexStrategy() {
		return
	}

	groups := e.groupByIndex(envValues)

	for baseKey, indexMap := range groups {
		if array := e.indexMapToArray(indexMap); array != nil {
			if transformedKey, ok := e.transformKey(baseKey, transform); ok {
				result[transformedKey] = array
			}
		}
	}
}

// shouldUseIndexStrategy checks if index-based parsing should be used.
func (e *EnvConfigSource) shouldUseIndexStrategy() bool {
	return e.arrayStrategy != ArrayStrategyDelimited && e.arrayStrategy != ArrayStrategyJSON
}

// groupByIndex groups environment variable values by their base key and index.
func (e *EnvConfigSource) groupByIndex(envValues map[string]string) map[string]map[int]string {
	groups := make(map[string]map[int]string)

	for key, value := range envValues {
		baseKey, index, ok := parseIndexSuffix(key)
		if !ok {
			continue
		}
		if index < 0 || index > 1000 {
			continue
		}
		if groups[baseKey] == nil {
			groups[baseKey] = make(map[int]string)
		}
		groups[baseKey][index] = value
	}

	return groups
}

// parseIndexSuffix extracts the base key and numeric suffix from an indexed key.
// Returns baseKey, index, ok. Example: "APP_HOSTS_0" → "APP_HOSTS", 0, true.
func parseIndexSuffix(key string) (string, int, bool) {
	lastUnderscore := strings.LastIndex(key, "_")
	if lastUnderscore == -1 {
		return "", 0, false
	}

	suffix := key[lastUnderscore+1:]
	index, err := strconv.Atoi(suffix)
	if err != nil {
		return "", 0, false
	}

	return key[:lastUnderscore], index, true
}

// indexMapToArray converts an index → value map to a slice in order.
// Returns nil if there are gaps in the sequence.
func (e *EnvConfigSource) indexMapToArray(indexMap map[int]string) []string {
	if len(indexMap) == 0 {
		return nil
	}

	// Find max index
	maxIndex := 0
	for index := range indexMap {
		if index > maxIndex {
			maxIndex = index
		}
	}

	// Verify no gaps
	array := make([]string, maxIndex+1)
	for i := 0; i <= maxIndex; i++ {
		if val, ok := indexMap[i]; ok {
			array[i] = val
		} else {
			return nil // Gap found
		}
	}

	return array
}

// transformKey applies the transform function to a key if possible.
func (e *EnvConfigSource) transformKey(key string, transform func(k, v string) (string, any)) (string, bool) {
	transformedKey, _ := transform(key, "")
	return transformedKey, transformedKey != ""
}
