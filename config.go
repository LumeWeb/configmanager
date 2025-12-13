package configmanager

import (
	"context"
	"github.com/Oudwins/zog"
	"go.lumeweb.com/configmanager/source"
	"reflect"
	"time"
)

type Validator interface {
	Validate() error
}

// ConfigSchemaProvider provides schema information for validation
type ConfigSchemaProvider interface {
	Schema() zog.ZogSchema
}

type ConfigChangeCallback func(key string, value any) error

// SubscriptionCallback is the function type for configuration change subscriptions
type SubscriptionCallback func(pattern, key string, value any)

type Manager interface {
	// Core operations
	Load() error
	LoadAll() error
	Shutdown() error
	Validate(keyPrefix ...string) error
	Persist(keyPrefix ...string) error
	EnableValidation()
	DisableValidation()
	ValidationEnabled() bool

	// Flag management
	FlagManager() FlagManager

	// Configuration access
	Get(key string, target ...any) (any, any, error)
	GetString(key string) (string, error)
	GetInt(key string) (int64, error)
	GetBool(key string) (bool, error)
	GetDuration(key string) (time.Duration, error)
	GetStringSlice(key string) ([]string, error)
	All() map[string]any
	IsSet(ctx context.Context, key string) bool
	Exists(key string) bool

	// Configuration modification
	Set(ctx context.Context, key string, value any) error
	BulkSet(ctx context.Context, updates map[string]any) error
	SetAtomic(ctx context.Context, updates map[string]any) error
	BulkSetAtomic(ctx context.Context, updates map[string]any) error
	Delete(key string)

	// Change notifications
	Subscribe(pattern string, callback SubscriptionCallback) func()

	// Key Management
	Keys() []string
	ListKeys(prefix string) []string

	// File paths
	ConfigFile() string
	ConfigDir() string

	// Struct registration
	RegisterStruct(key string, cfg any) error
	GetRegisteredStructs() map[string]reflect.Type
	ValidateRegisteredStructs() error

	// Namespace management
	RegisterNamespace(namespace string, src source.ConfigSource)
	UnregisterNamespace(namespace string) error
	LoadNamespaces() error
	LoadNamespace(namespace string) error

	// Source management
	RegisterSource(src source.ConfigSource)
	LoadSource(src source.ConfigSource, load bool, watch bool) error

	// Sync management
	SetupSync(opts ...ConfigOption) error

	// Delimiter access
	Delim() string

	// Root decodes the entire configuration into the target struct.
	// If target is nil, a new instance of the registered root struct type is created.
	// Returns an error if no root struct is registered or if validation fails.
	Root(target any) (any, error)
}
