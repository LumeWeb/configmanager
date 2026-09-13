package source

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/samber/lo"
	"io"
	"os"
	"strconv"
	"strings"
)

// ArrayStrategy defines how environment variable values are parsed as arrays.
type ArrayStrategy int

const (
	// ArrayStrategyAuto automatically detects the best parsing strategy.
	// Tries in order: index-based → JSON arrays → delimited (comma).
	ArrayStrategyAuto ArrayStrategy = iota

	// ArrayStrategyIndex uses index-based environment variables (APP_KEY_0, APP_KEY_1).
	// Clear ordering but requires multiple env vars.
	ArrayStrategyIndex

	// ArrayStrategyDelimited uses a delimiter to split values (e.g., "value1,value2,value3").
	// Delimiter defaults to comma but can be configured.
	ArrayStrategyDelimited

	// ArrayStrategyJSON parses values as JSON arrays or objects.
	// Array elements keep their JSON types: strings, objects (as map[string]any,
	// decodeable into struct fields), numbers and nested arrays.
	// Example: '["a","b"]' → []string{"a", "b"}
	// Example: '[{"k":"v"}]' → []any{map[string]any{"k": "v"}}
	ArrayStrategyJSON
)

// commonDelimiters are the standard delimiters for array parsing.
// Note: colon ":" and space " " are excluded to avoid false positives with URLs, IP addresses, and multi-word strings.
var commonDelimiters = []string{",", "|", ";"}

// maxIndexCount bounds the highest valid index for index-based array elements.
const maxIndexCount = 1000

// EnvConfigSource loads configuration from environment variables.
type EnvConfigSource struct {
	prefix         string
	delimiter      string
	global         bool                            // Controls whether this source should be loaded globally
	arrayStrategy  ArrayStrategy                   // Strategy for parsing array values
	arrayDelimiter string                          // Delimiter for array parsing (default: comma)
	environFunc    func() []string                 // Optional env override for testing
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
	if result, parsed := e.parseAsArray(value); parsed {
		return result
	}
	return value
}

// parseAsArray tries to parse a value as an array using the configured strategies.
// Returns the parsed value (if successful) and whether parsing succeeded.
// String-only arrays are coalesced to []string; arrays containing objects or
// other mixed types remain []any so nested values keep their structure.
func (e *EnvConfigSource) parseAsArray(value string) (any, bool) {
	// Order of attempts depends on strategy
	var strategies []func(string) (any, bool)

	switch e.arrayStrategy {
	case ArrayStrategyAuto:
		strategies = []func(string) (any, bool){
			e.tryParseJSONValue,
			e.tryParseDelimitedArray,
		}
	case ArrayStrategyDelimited:
		strategies = []func(string) (any, bool){
			e.tryParseDelimitedArray,
		}
	case ArrayStrategyJSON:
		strategies = []func(string) (any, bool){
			e.tryParseJSONValue,
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
func (e *EnvConfigSource) tryParseDelimitedArray(value string) (any, bool) {
	if value == "" {
		return nil, false
	}

	// Try configured delimiter first, then common alternatives
	delimiters := commonDelimiters

	// Insert configured delimiter first if not empty
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

// tryParseJSONValue parses the value as JSON (array or object). Arrays are
// coalesced via coalesceJSONArray; a top-level object is returned as
// map[string]any so nested structures survive decoding.
func (e *EnvConfigSource) tryParseJSONValue(value string) (any, bool) {
	result, ok := parseJSONValue(value)
	if !ok {
		return nil, false
	}
	return e.coalesceJSONArray(result), true
}

// coalesceJSONArray converts a JSON-parsed []any into []string when every
// element is a string, keeping the narrower type for consumers that expect it.
// Empty arrays also coalesce so they are indistinguishable from previous output.
func (e *EnvConfigSource) coalesceJSONArray(value any) any {
	arr, ok := value.([]any)
	if !ok {
		return value
	}
	if len(arr) == 0 {
		return []string{}
	}
	return coalesceType[string](arr)
}

// parseJSONValue parses a JSON array or object string, converting JSON numbers
// to int64 (or float64 for fractional values) and objects to map[string]any.
// Returns the parsed value and whether the input was valid JSON.
func parseJSONValue(value string) (any, bool) {
	value = strings.TrimSpace(value)
	if !isJSONValue(value) {
		return nil, false
	}

	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	var parsed any
	if err := decoder.Decode(&parsed); err != nil {
		return nil, false
	}
	// Reject anything after the JSON value (e.g. "[1] trailing")
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}

	return convertJSONNumbers(parsed), true
}

// isJSONValue checks whether the string is wrapped as a JSON array or object.
func isJSONValue(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '[':
		return s[len(s)-1] == ']'
	case '{':
		return s[len(s)-1] == '}'
	default:
		return false
	}
}

// convertJSONNumbers walks a decoded JSON value and converts json.Number to
// int64 when possible, float64 otherwise.
func convertJSONNumbers(value any) any {
	switch v := value.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		f, err := v.Float64()
		if err != nil {
			return v
		}
		return f
	case []any:
		for i, item := range v {
			v[i] = convertJSONNumbers(item)
		}
		return v
	case map[string]any:
		for k, item := range v {
			v[k] = convertJSONNumbers(item)
		}
		return v
	default:
		return value
	}
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
// An element may be a JSON array or object ('{"k":"v"}'); those are parsed into
// Go values so structured elements keep their shape. Mixed element types stay
// as []any; uniform string elements are coalesced to []string.
func (e *EnvConfigSource) mergeIndexBasedArrays(envValues map[string]string, transform func(k, v string) (string, any), result map[string]any) {
	if !e.shouldUseIndexStrategy() {
		return
	}

	groups := e.groupByIndex(envValues)
	mergedBases := make(map[string]bool)

	for baseKey, indexMap := range groups {
		if array := e.indexMapToArray(indexMap); array != nil {
			transformedKey, ok := e.transformKey(baseKey, transform)
			if !ok {
				continue
			}
			mergedBases[baseKey] = true
			transformedValues := lo.Map(array, func(val string, _ int) any {
				_, transformedVal := transform(baseKey, val)
				if str, isStr := transformedVal.(string); isStr {
					if parsed, parsedOk := e.tryParseJSONValue(str); parsedOk {
						transformedVal = parsed
					}
				}
				return transformedVal
			})
			result[transformedKey] = e.coalesceJSONArray(transformedValues)
		}
	}

	// Remove the individual indexed entries that were merged into the arrays.
	// Apply the same index range guard as groupByIndex and compute the deletion
	// key through the same transform(key, value) call Load used to store it.
	for rawKey := range envValues {
		base, index, ok := parseIndexSuffix(rawKey)
		if !ok || index < 0 || index > maxIndexCount || !mergedBases[base] {
			continue
		}
		if key, _ := transform(rawKey, envValues[rawKey]); key != "" {
			delete(result, key)
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
		if index < 0 || index > maxIndexCount {
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

// coalesceType converts a []any slice to a concrete []T if all elements are type T,
// otherwise returns the original slice unchanged. This helper uses generics to preserve
// type information when possible, returning concrete types for type consistency.
func coalesceType[T any](values []any) any {
	if len(values) == 0 {
		return values
	}

	for _, val := range values {
		if _, ok := val.(T); !ok {
			return values // Contains non-T elements
		}
	}

	// All elements are T - convert to []T
	result := make([]T, len(values))
	for i, val := range values {
		result[i] = val.(T)
	}
	return result
}
