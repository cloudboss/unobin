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

package compiler

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/util"
	"github.com/stretchr/testify/assert"
)

func Test_ValueExprEqual(t *testing.T) {
	testCases := []struct {
		name   string
		value  *ValueExpr
		other  *ValueExpr
		result bool
	}{
		{
			"empty value expressions should compare as equal",
			&ValueExpr{},
			&ValueExpr{},
			true,
		},
		{
			"value expressions with the same bool values should compare as equal",
			&ValueExpr{Bool: util.BoolP(false)},
			&ValueExpr{Bool: util.BoolP(false)},
			true,
		},
		{
			"value expressions with the same integer values should compare as equal",
			&ValueExpr{Number: &NumberExpr{Int: util.IntP(987)}},
			&ValueExpr{Number: &NumberExpr{Int: util.IntP(987)}},
			true,
		},
		{
			"value expressions with the same float values should compare as equal",
			&ValueExpr{Number: &NumberExpr{Float: util.FloatP(987.123)}},
			&ValueExpr{Number: &NumberExpr{Float: util.FloatP(987.123)}},
			true,
		},
		{
			"value expressions with the same string values should compare as equal",
			&ValueExpr{String: util.StringP("stringue")},
			&ValueExpr{String: util.StringP("stringue")},
			true,
		},
		{
			"value expressions with the same object values should compare as equal",
			&ValueExpr{Object: ObjectExpr{"key": &ValueExpr{String: util.StringP("stringue")}}},
			&ValueExpr{Object: ObjectExpr{"key": &ValueExpr{String: util.StringP("stringue")}}},
			true,
		},
		{
			"value expressions with the same array values should compare as equal",
			&ValueExpr{Array: ArrayExpr{&ValueExpr{String: util.StringP("stringue")}}},
			&ValueExpr{Array: ArrayExpr{&ValueExpr{String: util.StringP("stringue")}}},
			true,
		},
		{
			"one empty and one nonempty value expression should not compare as equal",
			&ValueExpr{},
			&ValueExpr{String: util.StringP("stringue")},
			false,
		},
		{
			"value expressions with different bool values should not compare as equal",
			&ValueExpr{Bool: util.BoolP(true)},
			&ValueExpr{Bool: util.BoolP(false)},
			false,
		},
		{
			"value expressions with different integer values should not compare as equal",
			&ValueExpr{Number: &NumberExpr{Int: util.IntP(987)}},
			&ValueExpr{Number: &NumberExpr{Int: util.IntP(789)}},
			false,
		},
		{
			"value expressions with different float values should not compare as equal",
			&ValueExpr{Number: &NumberExpr{Float: util.FloatP(987.123)}},
			&ValueExpr{Number: &NumberExpr{Float: util.FloatP(123.987)}},
			false,
		},
		{
			"value expressions with different string values should not compare as equal",
			&ValueExpr{String: util.StringP("stringue")},
			&ValueExpr{String: util.StringP("eugnirts")},
			false,
		},
		{
			"value expressions with different object values should not compare as equal",
			&ValueExpr{Object: ObjectExpr{"key-1": &ValueExpr{String: util.StringP("stringue")}}},
			&ValueExpr{Object: ObjectExpr{"key-2": &ValueExpr{String: util.StringP("eugnirts")}}},
			false,
		},
		{
			"value expressions with different array values should not compare as equal",
			&ValueExpr{Array: ArrayExpr{&ValueExpr{String: util.StringP("stringue")}}},
			&ValueExpr{Array: ArrayExpr{&ValueExpr{String: util.StringP("eugnirts")}}},
			false,
		},
		{
			"value expressions with different types of values should not compare as equal",
			&ValueExpr{Bool: util.BoolP(true)},
			&ValueExpr{Number: &NumberExpr{Float: util.FloatP(123.987)}},
			false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.value.Equal(tc.other)
			assert.Equal(t, tc.result, result)

		})
	}
}

func Test_ValueExprToGoValue(t *testing.T) {
	testCases := []struct {
		name   string
		value  ValueExpr
		result interface{}
	}{
		{
			"an empty value expression should produce nil",
			ValueExpr{},
			nil,
		},
		{
			"a bool value expression should produce a bool",
			ValueExpr{Bool: util.BoolP(true)},
			true,
		},
		{
			"a function value expression should produce an array with name and args as elements",
			ValueExpr{
				Function: &FunctionExpr{
					Name: "f",
					Args: ArrayExpr{
						&ValueExpr{
							Number: &NumberExpr{Int: util.IntP(1)},
						},
						&ValueExpr{
							Bool: util.BoolP(true),
						},
						&ValueExpr{
							String: util.StringP("three"),
						},
					},
				},
			},
			[]interface{}{"f", int64(1), true, "three"},
		},
		{
			"an int number value expression should produce an int64",
			ValueExpr{Number: &NumberExpr{Int: util.IntP(321)}},
			int64(321),
		},
		{
			"a float number value expression should produce a float",
			ValueExpr{Number: &NumberExpr{Float: util.FloatP(321.123)}},
			321.123,
		},
		{
			"an empty object value expression should produce an empty map",
			ValueExpr{Object: ObjectExpr{}},
			map[string]interface{}{},
		},
		{
			"a nonempty object value expression should produce a nonempty map",
			ValueExpr{Object: ObjectExpr{"key": &ValueExpr{String: util.StringP("hello")}}},
			map[string]interface{}{"key": "hello"},
		},
		{
			"an object value expression with an array value expression should produce an array value",
			ValueExpr{
				Object: ObjectExpr{
					"key": &ValueExpr{
						Array: []*ValueExpr{
							{Bool: util.BoolP(true)},
							{String: util.StringP("hello")},
							{Number: &NumberExpr{Int: util.IntP(1234)}},
							{Number: &NumberExpr{Float: util.FloatP(1234.5678)}},
						},
					},
				},
			},
			map[string]interface{}{
				"key": []interface{}{true, "hello", int64(1234), 1234.5678},
			},
		},
		{
			"an object value expression with nested objects should produce a nested map",
			ValueExpr{
				Object: ObjectExpr{
					"key": &ValueExpr{
						Object: ObjectExpr{
							"key": &ValueExpr{String: util.StringP("hello")},
						},
					},
				},
			},
			map[string]interface{}{
				"key": map[string]interface{}{"key": "hello"},
			},
		},
		{
			"an object value expression with an array of objects should produce an array of maps",
			ValueExpr{
				Object: ObjectExpr{
					"key": &ValueExpr{
						Array: []*ValueExpr{
							{
								Object: ObjectExpr{
									"key-1": &ValueExpr{String: util.StringP("hello")},
								},
							},
							{
								Object: ObjectExpr{
									"key-2": &ValueExpr{String: util.StringP("goodbye")},
								},
							},
						},
					},
				},
			},
			map[string]interface{}{
				"key": []interface{}{
					map[string]interface{}{"key-1": "hello"},
					map[string]interface{}{"key-2": "goodbye"},
				},
			},
		},
		{
			"an object value expression with pritimitive values should produce Go primitive values",
			ValueExpr{
				Object: ObjectExpr{
					"key-1": &ValueExpr{
						Bool: util.BoolP(false),
					},
					"key-2": &ValueExpr{
						String: util.StringP("hello"),
					},
					"key-3": &ValueExpr{
						Number: &NumberExpr{Float: util.FloatP(123.456)},
					},
					"key-4": &ValueExpr{
						Number: &NumberExpr{Int: util.IntP(101)},
					},
				},
			},
			map[string]interface{}{
				"key-1": false,
				"key-2": "hello",
				"key-3": 123.456,
				"key-4": int64(101),
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.value.ToGoValue()
			assert.Equal(t, tc.result, result)

		})
	}
}
