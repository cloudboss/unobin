package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_SnakeToPascal(t *testing.T) {
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
			name:   "Only an underscore should result in an empty string",
			input:  "_",
			result: "",
		},
		{
			name:   "Only multiple underscores should result in an empty string",
			input:  "___",
			result: "",
		},
		{
			name:   "Lowercase string should result in a capitalized string",
			input:  "hello",
			result: "Hello",
		},
		{
			name:   "Underscores should be removed with capitalized letters following",
			input:  "go-player_is_really_cool",
			result: "Go-playerIsReallyCool",
		},
		{
			name:   "Existing capital letters should not be affected",
			input:  "go-player_is_reALly_coOl",
			result: "Go-playerIsReALlyCoOl",
		},
		{
			name:   "Spaces should not affect the result",
			input:  "go-player is really cool",
			result: "Go-player is really cool",
		},
		{
			name:   "Strings with only spaces should not be affected",
			input:  "     ",
			result: "     ",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := SnakeToPascal(tc.input)
			assert.Equal(t, result, tc.result)
		})
	}
}
