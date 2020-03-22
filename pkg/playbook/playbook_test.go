package playbook

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ResolveMap(t *testing.T) {
	testCases := []struct {
		name       string
		path       string
		attributes map[string]interface{}
		result     map[string]interface{}
		err        error
	}{
		{
			name:       "empty attributes should produce error",
			path:       "x.y.z",
			attributes: map[string]interface{}{},
			result:     nil,
			err:        errors.New("map attribute `x.y.z` not found"),
		},
		{
			name: "empty path should produce error",
			path: "",
			attributes: map[string]interface{}{
				"xyz": "abc",
			},
			result: nil,
			err:    errors.New("map attribute `` not found"),
		},
		{
			name: "nonempty attributes with correct path should produce nonempty map",
			path: "x.y.z",
			attributes: map[string]interface{}{
				"x": map[string]interface{}{
					"y": map[string]interface{}{
						"z": map[string]interface{}{
							"xyz": "abc",
						},
					},
				},
			},
			result: map[string]interface{}{
				"xyz": "abc",
			},
			err: nil,
		},
		{
			name: "nonempty attributes with wrong value at path should produce error",
			path: "x.y.z",
			attributes: map[string]interface{}{
				"x": map[string]interface{}{
					"y": map[string]interface{}{
						"xyz": "abc",
					},
				},
			},
			result: nil,
			err:    errors.New("map attribute `x.y.z` not found"),
		},
		{
			name: "nonempty attributes with incorrect path should produce error",
			path: "a.b.c",
			attributes: map[string]interface{}{
				"x": map[string]interface{}{
					"y": map[string]interface{}{
						"z": map[string]interface{}{
							"xyz": "abc",
						},
					},
				},
			},
			result: nil,
			err:    errors.New("map attribute `a.b.c` not found"),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ResolveMap(tc.attributes, tc.path)
			assert.Equal(t, result, tc.result)
			assert.Equal(t, err, tc.err)
		})
	}
}

func Test_ResolveString(t *testing.T) {
	testCases := []struct {
		name       string
		path       string
		attributes map[string]interface{}
		result     string
		err        error
	}{
		{
			name:       "empty attributes should produce error",
			path:       "x.y.z",
			attributes: map[string]interface{}{},
			result:     "",
			err:        errors.New("string attribute `x.y.z` not found"),
		},
		{
			name: "empty path should produce error",
			path: "",
			attributes: map[string]interface{}{
				"xyz": "abc",
			},
			result: "",
			err:    errors.New("string attribute `` not found"),
		},
		{
			name: "nonempty attributes with correct path should produce nonempty string",
			path: "x.y.z",
			attributes: map[string]interface{}{
				"x": map[string]interface{}{
					"y": map[string]interface{}{
						"z": "abc",
					},
				},
			},
			result: "abc",
			err:    nil,
		},
		{
			name: "nonempty attributes with incorrect path should produce empty string",
			path: "a.b.c",
			attributes: map[string]interface{}{
				"x": map[string]interface{}{
					"y": map[string]interface{}{
						"xyz": "abc",
					},
				},
			},
			result: "",
			err:    errors.New("string attribute `a.b.c` not found"),
		},
		{
			name: "nonempty attributes with correct single length path should produce nonempty string",
			path: "a",
			attributes: map[string]interface{}{
				"a": "xyz",
			},
			result: "xyz",
			err:    nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ResolveString(tc.attributes, tc.path)
			assert.Equal(t, result, tc.result)
			assert.Equal(t, err, tc.err)
		})
	}
}
