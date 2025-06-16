// Package configmanager provides a flexible and extensible configuration management solution for Go applications.
//
// It supports loading configuration from various sources such as files, environment variables,
// and etcd, with features like validation, change notifications, and synchronization.
//
// Core Features:
//
//   - Multiple Configuration Sources: Load configuration from various sources, including files,
//     environment variables, etcd, and more.
//
//   - Validation: Validate configuration against Zog schemas to ensure data integrity.
//
//   - Change Notifications: Subscribe to configuration changes using wildcard patterns for real-time updates.
//
//   - Synchronization: Synchronize configuration across a distributed system using etcd for consistency.
//
//   - Struct Mapping: Automatically map configuration values to Go structs for easy access.
//
//   - Namespaces: Organize configuration into namespaces for better management and isolation.
//
// Usage:
//
// The ConfigManager is initialized with a set of ConfigSource implementations, each responsible
// for loading configuration data from a specific source. Configuration values can then be
// accessed using the Get method, with optional type conversions.
//
// Example:
//
//	cm, err := configmanager.NewConfigManager(
//		configmanager.UsingSources(
//			source.NewFileSource("config.yaml"),
//			source.NewEnvConfigSource("APP_", "_"),
//		),
//		configmanager.WithLogger(logger),
//	)
//	if err != nil {
//		log.Fatalf("failed to create config manager: %v", err)
//	}
//
//	if err := cm.Load(); err != nil {
//		log.Fatalf("failed to load config: %v", err)
//	}
//
//	appName, err := cm.GetString("app.name")
//	if err != nil {
//		log.Fatalf("failed to get app name: %v", err)
//	}
//	fmt.Println("App Name:", appName)
//
// Key Helpers:
//
//   - UsingSources(): Provides cleaner syntax for inline source configuration
//   - WithSources(): Traditional option pattern for sources
//   - WithLogger(): Configures logging
//   - WithConfigStruct(): Registers configuration structs
//
// For more detailed information and examples, refer to the individual subpackages
// and the project's README.
package configmanager
