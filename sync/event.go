package sync

import "go.lumeweb.com/event/v2"

// ConfigEvent represents a configuration change event.
type ConfigEvent struct {
	key      string // The full key path that changed
	value    any    // New value
	oldValue any    // Previous value
	aborted  bool   // Whether event processing should stop
	eventKey string // The key that matched the event pattern
}

// NewConfigEvent creates a new ConfigEvent with the old value captured
func NewConfigEvent(key string, newValue any, oldValue any, eventKey string) *ConfigEvent {
	return &ConfigEvent{
		key:      key,
		value:    newValue,
		oldValue: oldValue,
		eventKey: eventKey,
	}
}

// Name returns the event name (the config key that changed)
func (e *ConfigEvent) Name() string {
	return e.key
}

// Data returns the event data (the ConfigEvent itself)
func (e *ConfigEvent) Data() ConfigEvent {
	return *e
}

// SetData updates the event data
func (e *ConfigEvent) SetData(data ConfigEvent) (event.Event[ConfigEvent], error) {
	*e = data
	return e, nil
}

// Abort marks whether event processing should stop
func (e *ConfigEvent) Abort(abort bool) {
	e.aborted = abort
}

// IsAborted returns whether event processing should stop
func (e *ConfigEvent) IsAborted() bool {
	return e.aborted
}

// Get retrieves event properties by key
const (
	EventKeyKey      = "key"
	EventValueKey    = "value"
	EventOldValueKey = "oldValue"
)

func (e *ConfigEvent) Get(key string) any {
	switch key {
	case EventKeyKey:
		return e.key
	case EventValueKey:
		return e.value
	case EventOldValueKey:
		return e.oldValue
	default:
		return nil
	}
}

// Set updates event properties by key
func (e *ConfigEvent) Set(key string, val any) event.Event[ConfigEvent] {
	switch key {
	case EventKeyKey:
		if s, ok := val.(string); ok {
			e.key = s
		}
	case EventValueKey:
		e.value = val
	case EventOldValueKey:
		e.oldValue = val
	}
	return e
}

// OldValue returns the previous value of the config key
func (e *ConfigEvent) OldValue() any {
	return e.oldValue
}

// ConfigEventListener defines the function signature for handling ConfigEvents.
type ConfigEventListener func(event *ConfigEvent)

// PushCallback defines the function signature for handling successful push operations.
type PushCallback func(key string, value any)
