package lang

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTypeMessage(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "null", in: nil, want: "null"},
		{name: "string", in: "x", want: "a string"},
		{name: "boolean", in: true, want: "a boolean"},
		{name: "int", in: 1, want: "an integer"},
		{name: "int32", in: int32(1), want: "an integer"},
		{name: "int64", in: int64(1), want: "an integer"},
		{name: "float32", in: float32(1), want: "a number"},
		{name: "float64", in: 1.0, want: "a number"},
		{name: "list", in: []any{1, 2}, want: "a list"},
		{name: "object", in: map[string]any{"k": 1}, want: "an object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, TypeMessage(tt.in))
		})
	}
}
