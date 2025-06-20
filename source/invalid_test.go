package source

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInvalidSource(t *testing.T) {
	t.Run("default errors", func(t *testing.T) {
		src := New("", "")
		mgr := &mockManager{}

		err := src.Load(context.Background(), mgr)
		assert.EqualError(t, err, "invalid source: forced load error")

		err = src.Watch(context.Background(), mgr, func([]string, error) {})
		assert.EqualError(t, err, "invalid source: forced watch error")
	})

	t.Run("custom errors", func(t *testing.T) {
		src := New("custom load error", "custom watch error")
		mgr := &mockManager{}

		err := src.Load(context.Background(), mgr)
		assert.EqualError(t, err, "custom load error")

		err = src.Watch(context.Background(), mgr, func([]string, error) {})
		assert.EqualError(t, err, "custom watch error")
	})
}
