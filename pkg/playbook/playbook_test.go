package playbook

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_resolveMap(t *testing.T) {
	testCases := []struct {
		name       string
		path       string
		attributes map[string]interface{}
		result     map[string]interface{}
	}{
		{
			name:       "empty attributes should produce nil map",
			path:       "x.y.z",
			attributes: map[string]interface{}{},
			result:     nil,
		},
		{
			name: "empty path should produce nil map",
			path: "",
			attributes: map[string]interface{}{
				"xyz": "abc",
			},
			result: nil,
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
		},
		{
			name: "nonempty attributes with wrong value at path should produce nil map",
			path: "x.y.z",
			attributes: map[string]interface{}{
				"x": map[string]interface{}{
					"y": map[string]interface{}{
						"xyz": "abc",
					},
				},
			},
			result: nil,
		},
		{
			name: "nonempty attributes with incorrect path should produce nil map",
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
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := resolveMap(tc.attributes, tc.path)
			assert.Equal(t, result, tc.result)
		})
	}
}

func Test_resolveString(t *testing.T) {
	testCases := []struct {
		name       string
		path       string
		attributes map[string]interface{}
		result     string
	}{
		{
			name:       "empty attributes should produce empty string",
			path:       "x.y.z",
			attributes: map[string]interface{}{},
			result:     "",
		},
		{
			name: "empty path should produce empty string",
			path: "",
			attributes: map[string]interface{}{
				"xyz": "abc",
			},
			result: "",
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
		},
		{
			name: "nonempty attributes with correct single length path should produce nonempty string",
			path: "a",
			attributes: map[string]interface{}{
				"a": "xyz",
			},
			result: "xyz",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := resolveString(tc.attributes, tc.path)
			assert.Equal(t, result, tc.result)
		})
	}
}
