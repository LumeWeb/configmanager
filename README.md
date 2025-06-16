# ConfigManager

A Go library for managing application configuration from various sources, with support for validation, change notifications, and synchronization.

## Features

- **Multiple Configuration Sources:** Load configuration from files, environment variables, etcd, and more.
- **Validation:** Validate configuration against Zog schemas.
- **Change Notifications:** Subscribe to configuration changes using wildcard patterns.
- **Synchronization:** Synchronize configuration across a distributed system using etcd.
- **Struct Mapping:** Automatically map configuration values to Go structs.
- **Namespaces:** Organize configuration into namespaces for better management.

## Getting Started

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "go.lumeweb.com/configmanager"
    "go.lumeweb.com/configmanager/source"
    "go.uber.org/zap"
)

func main() {
    // Create a logger
    logger, err := zap.NewDevelopment()
    if err != nil {
        log.Fatalf("failed to create logger: %v", err)
    }

    // Create a config manager with multiple sources
    cm, err := configmanager.NewConfigManager(
        configmanager.UsingSources(
            source.NewFileSource("config.yaml"),
            source.NewEnvConfigSource("APP_", "_"),
        ),
        configmanager.WithLogger(logger),
    )
    if err != nil {
        log.Fatalf("failed to create config manager: %v", err)
    }

    // Load the configuration
    if err := cm.Load(); err != nil {
        log.Fatalf("failed to load config: %v", err)
    }

    // Get a string value
    value, err := cm.GetString("app.name")
    if err != nil {
        log.Fatalf("failed to get config value: %v", err)
    }
    fmt.Println("App name:", value)

    // Subscribe to changes
    unsub := cm.Subscribe("app.*", func(key string) {
        fmt.Println("Config changed:", key)
    })
    defer unsub()

    // Set a new value
    if err := cm.Set(context.Background(), "app.name", "NewAppName"); err != nil {
        log.Fatalf("failed to set config value: %v", err)
    }

    time.Sleep(time.Second)
}
```

## Configuration Sources

The ConfigManager supports loading configuration from various sources:

- **File:** Load configuration from YAML, JSON, or other file formats.
- **Environment Variables:** Load configuration from environment variables.
- **Etcd:** Load configuration from an etcd cluster.
- **Memory:** Load configuration from an in-memory map.
- **Default Values:** Load default configuration values from Go structs.

## Validation

The ConfigManager supports validating configuration values against Zog schemas.

## Change Notifications

The ConfigManager supports subscribing to configuration changes using wildcard patterns.

## Synchronization

The ConfigManager supports synchronizing configuration across a distributed system using etcd.

## Struct Mapping

The ConfigManager supports automatically mapping configuration values to Go structs.

## Namespaces

The ConfigManager supports organizing configuration into namespaces for better management.

## License

MIT License

Copyright (c) 2025 Hammer Technologies LLC

