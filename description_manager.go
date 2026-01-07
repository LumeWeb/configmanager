package configmanager

import "sync"

// DescriptionManager manages descriptions associated with configuration keys
type DescriptionManager interface {
	// SetDescription sets a description for a configuration key
	SetDescription(key string, description string)
	// GetDescription gets a description for a configuration key
	GetDescription(key string) string
	// SetDescriptions sets multiple descriptions at once
	SetDescriptions(descriptions map[string]string)
	// GetAllDescriptions returns all descriptions
	GetAllDescriptions() map[string]string
	// GetDescriptionsForPrefix returns all descriptions matching the given prefix
	GetDescriptionsForPrefix(prefix string) map[string]string
}

// DescriptionManagerDefault is the default implementation of DescriptionManager
type DescriptionManagerDefault struct {
	descriptions map[string]string
	mu           sync.RWMutex
}

// NewDescriptionManager creates a new DescriptionManagerDefault
func NewDescriptionManager() *DescriptionManagerDefault {
	return &DescriptionManagerDefault{
		descriptions: make(map[string]string),
	}
}

// SetDescription sets a description for a configuration key
func (d *DescriptionManagerDefault) SetDescription(key string, description string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.descriptions[key] = description
}

// GetDescription gets a description for a configuration key
func (d *DescriptionManagerDefault) GetDescription(key string) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.descriptions[key]
}

// SetDescriptions sets multiple descriptions at once
func (d *DescriptionManagerDefault) SetDescriptions(descriptions map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, desc := range descriptions {
		d.descriptions[key] = desc
	}
}

// GetAllDescriptions returns all descriptions
func (d *DescriptionManagerDefault) GetAllDescriptions() map[string]string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make(map[string]string, len(d.descriptions))
	for key, desc := range d.descriptions {
		result[key] = desc
	}
	return result
}

// GetDescriptionsForPrefix returns all descriptions matching the given prefix
func (d *DescriptionManagerDefault) GetDescriptionsForPrefix(prefix string) map[string]string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make(map[string]string)
	for key, desc := range d.descriptions {
		if prefix == "" || key == prefix || (len(key) > len(prefix) && key[:len(prefix)] == prefix && key[len(prefix)] == '.') {
			result[key] = desc
		}
	}
	return result
}
