package configmanager

import (
	"github.com/samber/lo"
	"reflect"
	"strings"
	"sync"

	"go.lumeweb.com/configmanager/source"
)

// NamespaceSource represents a namespace and its associated ConfigSource
type NamespaceSource struct {
	Namespace string
	Source    source.ConfigSource
}

// ConfigRegistry manages namespaces and their configuration sources
type ConfigRegistry interface {
	// Register associates a ConfigSource with a namespace
	Register(namespace string, src source.ConfigSource)
	// Unregister removes a namespace from the registry
	Unregister(namespace string)
	// GetSource returns the ConfigSource for a namespace
	GetSource(namespace string) (source.ConfigSource, bool)
	// GetNamespace returns the namespace for a ConfigSource
	GetNamespace(src source.ConfigSource) (string, bool)
	// ListNamespaces returns all registered namespaces
	ListNamespaces() []string
	// FindMostSpecificNamespace finds the most specific namespace for a given key
	FindMostSpecificNamespace(key string, delim string) (NamespaceSource, string)
}

// DefaultConfigRegistry is the default implementation of ConfigRegistry
type DefaultConfigRegistry struct {
	namespaces map[string]source.ConfigSource // Key: Namespace, Value: ConfigSource
	mu         sync.RWMutex
}

// NewDefaultConfigRegistry creates a new DefaultConfigRegistry
func NewDefaultConfigRegistry() *DefaultConfigRegistry {
	return &DefaultConfigRegistry{
		namespaces: make(map[string]source.ConfigSource),
	}
}

func (r *DefaultConfigRegistry) Register(namespace string, src source.ConfigSource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.namespaces[namespace] = src
}

func (r *DefaultConfigRegistry) Unregister(namespace string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.namespaces, namespace)
}

func (r *DefaultConfigRegistry) GetSource(namespace string) (source.ConfigSource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src, ok := r.namespaces[namespace]
	return src, ok
}

func (r *DefaultConfigRegistry) ListNamespaces() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	namespaces := make([]string, 0, len(r.namespaces))
	for ns := range r.namespaces {
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

// GetNamespace returns the namespace for a ConfigSource by doing a reverse lookup
func (r *DefaultConfigRegistry) GetNamespace(src source.ConfigSource) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for ns, s := range r.namespaces {
		if s == src {
			return ns, true
		}
	}
	return "", false
}

func (r *DefaultConfigRegistry) FindMostSpecificNamespace(key string, delim string) (NamespaceSource, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// First check for exact match
	if src, ok := r.namespaces[key]; ok {
		return NamespaceSource{
			Namespace: key,
			Source:    src,
		}, ""
	}

	// Then check for partial matches in descending order of specificity
	parts := strings.Split(key, delim)
	result := lo.ReduceRight(parts, func(acc lo.Tuple2[NamespaceSource, string], _ string, i int) lo.Tuple2[NamespaceSource, string] {
		// If we already found a match, return it
		if !reflect.ValueOf(acc.A).IsZero() {
			return acc
		}

		prefix := strings.Join(parts[:i+1], delim)
		if src, ok := r.namespaces[prefix]; ok {
			return lo.T2(
				NamespaceSource{Namespace: prefix, Source: src},
				strings.Join(parts[i+1:], delim),
			)
		}
		return acc
	}, lo.T2(NamespaceSource{}, key))

	return result.A, result.B
}
