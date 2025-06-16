package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigEvent(t *testing.T) {
	t.Run("NewConfigEvent", func(t *testing.T) {
		evt := NewConfigEvent("test.key", "new", "old", "test.key")
		assert.Equal(t, "test.key", evt.key)
		assert.Equal(t, "new", evt.value)
		assert.Equal(t, "old", evt.oldValue)
		assert.Equal(t, "test.key", evt.eventKey)
		assert.False(t, evt.aborted)
	})

	t.Run("Name", func(t *testing.T) {
		evt := NewConfigEvent("test.key", nil, nil, "test.key")
		assert.Equal(t, "test.key", evt.Name())
	})

	t.Run("Data", func(t *testing.T) {
		evt := NewConfigEvent("test.key", "value", "old", "test.key")
		data := evt.Data()
		assert.Equal(t, *evt, data)
	})

	t.Run("SetData", func(t *testing.T) {
		evt := NewConfigEvent("test.key", "value", "old", "test.key")
		newData := ConfigEvent{
			key:      "new.key",
			value:    "new",
			oldValue: "newold",
			eventKey: "new.key",
		}
		result, err := evt.SetData(newData)
		assert.NoError(t, err)
		assert.Equal(t, newData, result.Data())
	})

	t.Run("Abort", func(t *testing.T) {
		evt := NewConfigEvent("test.key", nil, nil, "test.key")
		evt.Abort(true)
		assert.True(t, evt.IsAborted())
		evt.Abort(false)
		assert.False(t, evt.IsAborted())
	})

	t.Run("Get", func(t *testing.T) {
		evt := NewConfigEvent("test.key", "value", "old", "test.key")
		
		tests := []struct {
			key      string
			expected any
		}{
			{EventKeyKey, "test.key"},
			{EventValueKey, "value"},
			{EventOldValueKey, "old"},
			{"invalid", nil},
		}

		for _, tt := range tests {
			t.Run(tt.key, func(t *testing.T) {
				assert.Equal(t, tt.expected, evt.Get(tt.key))
			})
		}
	})

	t.Run("Set", func(t *testing.T) {
		tests := []struct {
			name     string
			key      string
			value    any
			expected ConfigEvent
		}{
			{
				"key",
				EventKeyKey, 
				"new.key",
				ConfigEvent{key: "new.key", value: "", oldValue: nil, eventKey: ""},
			},
			{
				"value",
				EventValueKey,
				"newvalue",
				ConfigEvent{key: "", value: "newvalue", oldValue: nil, eventKey: ""},
			},
			{
				"oldValue",
				EventOldValueKey,
				"newold",
				ConfigEvent{key: "", value: nil, oldValue: "newold", eventKey: ""},
			},
			{
				"invalid",
				"invalid",
				"ignored",
				ConfigEvent{key: "", value: nil, oldValue: nil, eventKey: ""},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				evt := NewConfigEvent("test.key", "value", "old", "test.key")
				result := evt.Set(tt.key, tt.value)
				
				// Create expected event by copying original and applying expected changes
				expected := *evt
				switch tt.key {
				case EventKeyKey:
					expected.key = tt.value.(string)
				case EventValueKey:
					expected.value = tt.value
				case EventOldValueKey:
					expected.oldValue = tt.value
				}
				
				assert.Equal(t, expected, result.Data())
			})
		}
	})

	t.Run("OldValue", func(t *testing.T) {
		evt := NewConfigEvent("test.key", "new", "old", "test.key")
		assert.Equal(t, "old", evt.OldValue())
	})
}
