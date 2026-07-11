package ptr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValue(t *testing.T) {
	text := "configured"
	enabled := true

	assert.Equal(t, "configured", Value(&text))
	assert.True(t, Value(&enabled))
	assert.Empty(t, Value[string](nil))
	assert.False(t, Value[bool](nil))
}
