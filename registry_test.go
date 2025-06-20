package configmanager

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.lumeweb.com/configmanager/source"
)

func TestDefaultConfigRegistry(t *testing.T) {
	t.Run("Register and GetSource", func(t *testing.T) {
		reg := NewDefaultConfigRegistry()
		src := source.NewMemoryConfigSource(nil)

		reg.Register("test", src)
		retrieved, ok := reg.GetSource("test")

		assert.True(t, ok)
		assert.Equal(t, src, retrieved)
	})

	t.Run("GetSource non-existent", func(t *testing.T) {
		reg := NewDefaultConfigRegistry()
		_, ok := reg.GetSource("nonexistent")
		assert.False(t, ok)
	})

	t.Run("Unregister", func(t *testing.T) {
		reg := NewDefaultConfigRegistry()
		src := source.NewMemoryConfigSource(nil)

		reg.Register("test", src)
		reg.Unregister("test")

		_, ok := reg.GetSource("test")
		assert.False(t, ok)
	})

	t.Run("ListNamespaces", func(t *testing.T) {
		reg := NewDefaultConfigRegistry()
		src1 := source.NewMemoryConfigSource(nil)
		src2 := source.NewMemoryConfigSource(nil)

		reg.Register("ns1", src1)
		reg.Register("ns2", src2)

		namespaces := reg.ListNamespaces()
		assert.Len(t, namespaces, 2)
		assert.Contains(t, namespaces, "ns1")
		assert.Contains(t, namespaces, "ns2")
	})

	t.Run("FindMostSpecificNamespace", func(t *testing.T) {
		reg := NewDefaultConfigRegistry()
		src1 := source.NewMemoryConfigSource(nil)
		src2 := source.NewMemoryConfigSource(nil)

		reg.Register("parent", src1)
		reg.Register("parent.child", src2)

		tests := []struct {
			key       string
			expected  NamespaceSource
			remainder string
		}{
			{
				"parent.child.grandchild",
				NamespaceSource{"parent.child", src2},
				"grandchild",
			},
			{
				"parent.sibling",
				NamespaceSource{"parent", src1},
				"sibling",
			},
			{
				"other",
				NamespaceSource{},
				"other",
			},
		}

		for _, tt := range tests {
			t.Run(tt.key, func(t *testing.T) {
				ns, remainder := reg.FindMostSpecificNamespace(tt.key, ".")
				assert.Equal(t, tt.expected, ns)
				assert.Equal(t, tt.remainder, remainder)
			})
		}
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		reg := NewDefaultConfigRegistry()
		src := source.NewMemoryConfigSource(nil)
		done := make(chan bool)

		go func() {
			reg.Register("concurrent", src)
			done <- true
		}()

		go func() {
			reg.ListNamespaces()
			done <- true
		}()

		<-done
		<-done
	})
}
