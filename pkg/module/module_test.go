package module

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_qualifiedIdentifier(t *testing.T) {
	testCases := []struct {
		name   string
		input  string
		result string
	}{
		{
			name:   "An empty string should result in an empty string",
			input:  "",
			result: "",
		},
		{
			name:   "A URL import should give a result",
			input:  "github.com/cloudboss/go-player/modules/command.Command",
			result: "command.Command",
		},
		{
			name:   "A local import should give a result",
			input:  "command.Command",
			result: "command.Command",
		},
	}
	for _, tc := range testCases {
		result := qualifiedIdentifier(tc.input)
		assert.Equal(t, result, tc.result)
	}
}
