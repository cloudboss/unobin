// Copyright © 2020 Joseph Wright <joseph@cloudboss.co>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

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

func Test_KebabToCamel(t *testing.T) {
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
			name:   "Only a dash should result in an empty string",
			input:  "-",
			result: "",
		},
		{
			name:   "Only multiple dashes should result in an empty string",
			input:  "---",
			result: "",
		},
		{
			name:   "Lowercase string should result in a lowercase string",
			input:  "hello",
			result: "hello",
		},
		{
			name:   "Dashes should be removed with capitalized letters following",
			input:  "unobin-is-really-cool",
			result: "unobinIsReallyCool",
		},
		{
			name:   "Existing capital letters should not be affected",
			input:  "unobin-is-reALly-coOl",
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
			result := KebabToCamel(tc.input)
			assert.Equal(t, tc.result, result)
		})
	}
}

func Test_KebabToPascal(t *testing.T) {
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
			name:   "Only a dash should result in an empty string",
			input:  "-",
			result: "",
		},
		{
			name:   "Only multiple dashes should result in an empty string",
			input:  "---",
			result: "",
		},
		{
			name:   "Lowercase string should result in a capitalized string",
			input:  "hello",
			result: "Hello",
		},
		{
			name:   "Underscores should be removed with capitalized letters following",
			input:  "unobin-is-really-cool",
			result: "UnobinIsReallyCool",
		},
		{
			name:   "Existing capital letters should not be affected",
			input:  "unobin-is-reALly-coOl",
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
			result := KebabToPascal(tc.input)
			assert.Equal(t, tc.result, result)
		})
	}
}
