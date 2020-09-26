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
			input:  "unobin_is_really_cool",
			result: "UnobinIsReallyCool",
		},
		{
			name:   "Existing capital letters should not be affected",
			input:  "unobin_is_reALly_coOl",
			result: "UnobinIsReALlyCoOl",
		},
		{
			name:   "Spaces should not affect the result",
			input:  "unobin is really cool",
			result: "Unobin is really cool",
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
			assert.Equal(t, tc.result, result)
		})
	}
}

func Test_SnakeToCamel(t *testing.T) {
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
			name:   "Lowercase string should result in a lowercase string",
			input:  "hello",
			result: "hello",
		},
		{
			name:   "Underscores should be removed with capitalized letters following",
			input:  "unobin_is_really_cool",
			result: "unobinIsReallyCool",
		},
		{
			name:   "Existing capital letters should not be affected",
			input:  "unobin_is_reALly_coOl",
			result: "unobinIsReALlyCoOl",
		},
		{
			name:   "Spaces should not affect the result",
			input:  "unobin is really cool",
			result: "unobin is really cool",
		},
		{
			name:   "Strings with only spaces should not be affected",
			input:  "     ",
			result: "     ",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := SnakeToCamel(tc.input)
			assert.Equal(t, tc.result, result)
		})
	}
}
