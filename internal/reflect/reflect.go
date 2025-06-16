package reflect

import (
	"reflect"
)

// ImplementsInterface checks if a struct registered under a key implements an interface
func ImplementsInterface(structType reflect.Type, iface reflect.Type) bool {
	return reflect.PointerTo(structType).Implements(iface)
}

// ImplementsValidator checks if a struct implements the Validator interface
func ImplementsValidator(structType reflect.Type) bool {
	return ImplementsInterface(structType, reflect.TypeOf((*Validator)(nil)).Elem())
}

// ImplementsConfigSchemaProvider checks if a struct implements ConfigSchemaProvider
func ImplementsConfigSchemaProvider(structType reflect.Type) bool {
	return ImplementsInterface(structType, reflect.TypeOf((*ConfigSchemaProvider)(nil)).Elem())
}

// ImplementsConfigDefaults checks if a struct implements ConfigDefaults
func ImplementsConfigDefaults(structType reflect.Type) bool {
	return ImplementsInterface(structType, reflect.TypeOf((*ConfigDefaults)(nil)).Elem())
}

// Validator is the interface for configuration validation
type Validator interface {
	Validate() error
}

// ConfigSchemaProvider provides schema information for validation
type ConfigSchemaProvider interface {
	Schema() Schema
}

// Schema represents a validation schema
type Schema any

// ConfigDefaults provides default configuration values
type ConfigDefaults interface {
	Defaults() map[string]any
}
