package compiler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_nothing(t *testing.T) {
	testCases := []struct {
		name   string
		input  string
		result string
	}{
		{
			name:   "Pointless tests should do nothing",
			input:  "",
			result: "",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.input, tc.result)
		})
	}
}
