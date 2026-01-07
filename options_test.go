package configmanager

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/configmanager/source"
	"go.uber.org/zap"
)

func TestWithTagName(t *testing.T) {
	t.Run("sets custom tag name", func(t *testing.T) {
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithTagName("custom"),
		)
		require.NoError(t, err)
		assert.Equal(t, "custom", cm.tagName)
	})

	t.Run("sets empty tag name", func(t *testing.T) {
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithTagName(""),
		)
		require.NoError(t, err)
		assert.Equal(t, "", cm.tagName)
	})
}

func TestWithFlags(t *testing.T) {
	t.Run("sets flags for multiple keys", func(t *testing.T) {
		flags := map[string][]string{
			"app.name": {"--name", "-n"},
			"app.port": {"--port", "-p"},
		}
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithFlags(flags),
		)
		require.NoError(t, err)

		// Verify flags were set correctly
		flagMgr := cm.flagManager
		assert.Equal(t, flags["app.name"], flagMgr.GetFlags("app.name"))
		assert.Equal(t, flags["app.port"], flagMgr.GetFlags("app.port"))
	})

	t.Run("sets empty flags map", func(t *testing.T) {
		flags := map[string][]string{}
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithFlags(flags),
		)
		require.NoError(t, err)
		assert.NotNil(t, cm)
	})

	t.Run("sets flags with single flag per key", func(t *testing.T) {
		flags := map[string][]string{
			"app.debug": {"--debug"},
		}
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithFlags(flags),
		)
		require.NoError(t, err)

		flagMgr := cm.flagManager
		assert.Equal(t, []string{"--debug"}, flagMgr.GetFlags("app.debug"))
	})
}

func TestWithOptionsDescriptions(t *testing.T) {
	t.Run("sets descriptions for multiple keys", func(t *testing.T) {
		descriptions := map[string]string{
			"app.name": "Application name",
			"app.port": "Application port",
		}
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithDescriptions(descriptions),
		)
		require.NoError(t, err)

		// Verify descriptions were set correctly
		descMgr := cm.descriptionManager
		assert.Equal(t, descriptions["app.name"], descMgr.GetDescription("app.name"))
		assert.Equal(t, descriptions["app.port"], descMgr.GetDescription("app.port"))
	})

	t.Run("sets empty descriptions map", func(t *testing.T) {
		descriptions := map[string]string{}
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithDescriptions(descriptions),
		)
		require.NoError(t, err)
		assert.NotNil(t, cm)
	})

	t.Run("sets descriptions with empty string values", func(t *testing.T) {
		descriptions := map[string]string{
			"app.name": "",
		}
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithDescriptions(descriptions),
		)
		require.NoError(t, err)

		descMgr := cm.descriptionManager
		assert.Equal(t, "", descMgr.GetDescription("app.name"))
	})
}

func TestWithLogger(t *testing.T) {
	t.Run("sets custom logger", func(t *testing.T) {
		logger := zap.NewNop()
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithLogger(logger),
		)
		require.NoError(t, err)
		assert.Equal(t, logger, cm.logger)
	})

	t.Run("sets nil logger", func(t *testing.T) {
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithLogger(nil),
		)
		require.NoError(t, err)
		assert.Nil(t, cm.logger)
	})
}

func TestWithDefaultConfigFile(t *testing.T) {
	t.Run("sets default config file path", func(t *testing.T) {
		configFile := "/etc/config/app.yaml"
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithDefaultConfigFile(configFile),
		)
		require.NoError(t, err)
		assert.Equal(t, configFile, cm.configFile)
	})

	t.Run("uses default when empty string is set", func(t *testing.T) {
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithDefaultConfigFile(""),
		)
		require.NoError(t, err)
		assert.Equal(t, "config.yaml", cm.configFile)
	})

	t.Run("sets config file with relative path", func(t *testing.T) {
		configFile := "./config/app.yaml"
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithDefaultConfigFile(configFile),
		)
		require.NoError(t, err)
		assert.Equal(t, configFile, cm.configFile)
	})
}

func TestWithSyncConfigNamespace(t *testing.T) {
	t.Run("sets sync config namespace", func(t *testing.T) {
		namespace := "sync"
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithSyncConfigNamespace(namespace),
		)
		require.NoError(t, err)
		assert.Equal(t, namespace, cm.syncConfigNS)
	})

	t.Run("sets empty sync config namespace", func(t *testing.T) {
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithSyncConfigNamespace(""),
		)
		require.NoError(t, err)
		assert.Equal(t, "", cm.syncConfigNS)
	})

	t.Run("sets namespace with dot notation", func(t *testing.T) {
		namespace := "app.sync.config"
		cm, err := NewConfigManager(
			[]source.ConfigSource{},
			WithSyncConfigNamespace(namespace),
		)
		require.NoError(t, err)
		assert.Equal(t, namespace, cm.syncConfigNS)
	})
}

func TestWithSources(t *testing.T) {
	t.Run("sets single source", func(t *testing.T) {
		memSource := source.NewMemoryConfigSource(map[string]any{
			"app.name": "TestApp",
		})
		cm, err := NewConfigManager(
			[]source.ConfigSource{}, // No initial sources
			WithSources(memSource),
		)
		require.NoError(t, err)
		assert.Len(t, cm.sources, 1)
		assert.Equal(t, memSource, cm.sources[0])
	})

	t.Run("sets multiple sources", func(t *testing.T) {
		source1 := source.NewMemoryConfigSource(map[string]any{
			"app.name": "TestApp",
		})
		source2 := source.NewMemoryConfigSource(map[string]any{
			"app.port": 8080,
		})
		cm, err := NewConfigManager(
			[]source.ConfigSource{}, // No initial sources
			WithSources(source1, source2),
		)
		require.NoError(t, err)
		assert.Len(t, cm.sources, 2)
		assert.Equal(t, source1, cm.sources[0])
		assert.Equal(t, source2, cm.sources[1])
	})

	t.Run("sets empty sources list", func(t *testing.T) {
		cm, err := NewConfigManager(
			[]source.ConfigSource{}, // No initial sources
			WithSources(),
		)
		require.NoError(t, err)
		assert.Empty(t, cm.sources)
	})
}

func TestUsingSources(t *testing.T) {
	t.Run("returns single source as-is", func(t *testing.T) {
		memSource := source.NewMemoryConfigSource(map[string]any{
			"app.name": "TestApp",
		})
		sources := UsingSources(memSource)
		assert.Len(t, sources, 1)
		assert.Equal(t, memSource, sources[0])
	})

	t.Run("returns multiple sources as-is", func(t *testing.T) {
		source1 := source.NewMemoryConfigSource(map[string]any{
			"app.name": "TestApp",
		})
		source2 := source.NewMemoryConfigSource(map[string]any{
			"app.port": 8080,
		})
		sources := UsingSources(source1, source2)
		assert.Len(t, sources, 2)
		assert.Equal(t, source1, sources[0])
		assert.Equal(t, source2, sources[1])
	})

	t.Run("returns empty list for no sources", func(t *testing.T) {
		sources := UsingSources()
		assert.Empty(t, sources)
	})
}

func TestMultipleOptions(t *testing.T) {
	t.Run("combines multiple ConfigOptions", func(t *testing.T) {
		logger := zap.NewNop()
		memSource := source.NewMemoryConfigSource(map[string]any{
			"app.name": "TestApp",
		})
		flags := map[string][]string{
			"app.name": {"--name", "-n"},
		}
		descriptions := map[string]string{
			"app.name": "Application name",
		}

		cm, err := NewConfigManager(
			[]source.ConfigSource{}, // No initial sources
			WithTagName("custom"),
			WithLogger(logger),
			WithDefaultConfigFile("/etc/config/app.yaml"),
			WithSyncConfigNamespace("sync"),
			WithSources(memSource),
			WithFlags(flags),
			WithDescriptions(descriptions),
		)

		require.NoError(t, err)

		// Verify all options were applied
		assert.Equal(t, "custom", cm.tagName)
		assert.Equal(t, logger, cm.logger)
		assert.Equal(t, "/etc/config/app.yaml", cm.configFile)
		assert.Equal(t, "sync", cm.syncConfigNS)
		assert.Len(t, cm.sources, 1)
		assert.Equal(t, memSource, cm.sources[0])
		assert.Equal(t, flags["app.name"], cm.flagManager.GetFlags("app.name"))
		assert.Equal(t, descriptions["app.name"], cm.descriptionManager.GetDescription("app.name"))
	})
}
