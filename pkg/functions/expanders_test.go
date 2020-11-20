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

package functions

import (
	"errors"
	"testing"

	"github.com/cloudboss/unobin/pkg/types"
	"github.com/stretchr/testify/assert"
)

func Test_ExpandArray(t *testing.T) {
	ctx := &types.Context{
		Vars: map[string]interface{}{
			"name":   "Unobin",
			"number": "one",
		},
		State: map[string]interface{}{},
	}
	testCases := []struct {
		name     string
		original []interface{}
		expanded []interface{}
		err      error
	}{
		{
			name:     "an empty array should expand to an empty array",
			original: []interface{}{},
			expanded: []interface{}{},
		},
		{
			name: "an array with an existing variable lookup function should expand",
			original: []interface{}{
				StringVar(ctx, String{"name", nil}),
			},
			expanded: []interface{}{"Unobin"},
		},
		{
			name:     "an array with a nonexistent variable lookup should produce an error",
			original: []interface{}{StringVar(ctx, String{"unobin", nil})},
			err:      errors.New("string attribute `unobin` not found"),
		},
		{
			name: "an array with a mixture of values and lookups should expand",
			original: []interface{}{
				"Unobin",
				StringVar(ctx, String{"name", nil}),
			},
			expanded: []interface{}{"Unobin", "Unobin"},
		},
		{
			name: "an array with nested values should expand",
			original: []interface{}{
				"Unobin",
				StringVar(ctx, String{"name", nil}),
				map[string]interface{}{"rank": String{"one", nil}},
				[]interface{}{
					StringVar(ctx, String{"name", nil}), "is", "number", "one",
				},
			},
			expanded: []interface{}{
				"Unobin",
				"Unobin",
				map[string]interface{}{"rank": "one"},
				[]interface{}{"Unobin", "is", "number", "one"},
			},
		},
		{
			name: "an array with deeply nested values should expand",
			original: []interface{}{
				"Unobin",
				StringVar(ctx, String{"name", nil}),
				map[string]interface{}{"rank": String{"one", nil}},
				[]interface{}{
					StringVar(ctx, String{"name", nil}),
					"is",
					map[string]interface{}{
						"number": StringVar(ctx, String{"number", nil}),
					},
				},
			},
			expanded: []interface{}{
				"Unobin",
				"Unobin",
				map[string]interface{}{"rank": "one"},
				[]interface{}{
					"Unobin",
					"is",
					map[string]interface{}{
						"number": "one",
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandArray(tc.original)
			assert.Equal(t, tc.err, err)
			assert.Equal(t, tc.expanded, expanded)
		})
	}
}

func Test_ExpandObject(t *testing.T) {
	ctx := &types.Context{
		Vars: map[string]interface{}{
			"name":   "Unobin",
			"number": "one",
		},
		State: map[string]interface{}{},
	}
	testCases := []struct {
		name     string
		original map[string]interface{}
		expanded map[string]interface{}
		err      error
	}{
		{
			name:     "an empty map should expand to an empty map",
			original: map[string]interface{}{},
			expanded: map[string]interface{}{},
		},
		{
			name: "a map with an existing variable lookup function should expand",
			original: map[string]interface{}{
				"name": StringVar(ctx, String{"name", nil}),
			},
			expanded: map[string]interface{}{"name": "Unobin"},
		},
		{
			name:     "a map with a nonexistent variable lookup should produce an error",
			original: map[string]interface{}{"name": StringVar(ctx, String{"unobin", nil})},
			err:      errors.New("string attribute `unobin` not found"),
		},
		{
			name: "a map with a mixture of values and lookups should expand",
			original: map[string]interface{}{
				"boss": "Unobin",
				"name": StringVar(ctx, String{"name", nil}),
			},
			expanded: map[string]interface{}{"boss": "Unobin", "name": "Unobin"},
		},
		{
			name: "a map with nested values should expand",
			original: map[string]interface{}{
				"boss": "Unobin",
				"name": StringVar(ctx, String{"name", nil}),
				"map":  map[string]interface{}{"rank": String{"one", nil}},
				"array": []interface{}{
					StringVar(ctx, String{"name", nil}), "is", "number", "one",
				},
			},
			expanded: map[string]interface{}{
				"boss":  "Unobin",
				"name":  "Unobin",
				"map":   map[string]interface{}{"rank": "one"},
				"array": []interface{}{"Unobin", "is", "number", "one"},
			},
		},
		{
			name: "a map with deeply nested values should expand",
			original: map[string]interface{}{
				"boss": "Unobin",
				"name": StringVar(ctx, String{"name", nil}),
				"map":  map[string]interface{}{"rank": String{"one", nil}},
				"array": []interface{}{
					StringVar(ctx, String{"name", nil}),
					"is",
					map[string]interface{}{
						"number": StringVar(ctx, String{"number", nil}),
					},
				},
			},
			expanded: map[string]interface{}{
				"boss": "Unobin",
				"name": "Unobin",
				"map":  map[string]interface{}{"rank": "one"},
				"array": []interface{}{
					"Unobin",
					"is",
					map[string]interface{}{
						"number": "one",
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expanded, err := ExpandObject(tc.original)
			assert.Equal(t, tc.err, err)
			assert.Equal(t, tc.expanded, expanded)
		})
	}
}
