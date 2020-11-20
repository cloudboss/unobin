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

// ExpandArray takes an array whose elements may have been generated from
// Unobin function calls and converts the values into ordinary Go values.
func ExpandArray(array []interface{}) ([]interface{}, error) {
	a := make([]interface{}, len(array))
	for i, elem := range array {
		switch elem.(type) {
		case Bool:
			if elem.(Bool).Error != nil {
				return nil, elem.(Bool).Error
			}
			a[i] = elem.(Bool).Value
		case Float:
			if elem.(Float).Error != nil {
				return nil, elem.(Float).Error
			}
			a[i] = elem.(Float).Value
		case Int:
			if elem.(Int).Error != nil {
				return nil, elem.(Int).Error
			}
			a[i] = elem.(Int).Value
		case Interface:
			if elem.(Interface).Error != nil {
				return nil, elem.(Interface).Error
			}
			a[i] = elem.(Interface).Value
		case String:
			if elem.(String).Error != nil {
				return nil, elem.(String).Error
			}
			a[i] = elem.(String).Value
		case Array:
			if elem.(Array).Error != nil {
				return nil, elem.(Array).Error
			}
			expanded, error := ExpandArray(elem.(Array).Value)
			if error != nil {
				return nil, error
			}
			a[i] = expanded
		case Object:
			if elem.(Object).Error != nil {
				return nil, elem.(Object).Error
			}
			expanded, error := ExpandObject(elem.(Object).Value)
			if error != nil {
				return nil, error
			}
			a[i] = expanded
		case []interface{}:
			expanded, error := ExpandArray(elem.([]interface{}))
			if error != nil {
				return nil, error
			}
			a[i] = expanded
		case map[string]interface{}:
			expanded, error := ExpandObject(elem.(map[string]interface{}))
			if error != nil {
				return nil, error
			}
			a[i] = expanded
		default:
			a[i] = elem
		}
	}
	return a, nil
}

// ExpandObject takes a map whose elements may have been generated from
// Unobin function calls and converts the values into ordinary Go values.
func ExpandObject(object map[string]interface{}) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	for k, v := range object {
		switch v.(type) {
		case Bool:
			if v.(Bool).Error != nil {
				return nil, v.(Bool).Error
			}
			m[k] = v.(Bool).Value
		case Float:
			if v.(Float).Error != nil {
				return nil, v.(Float).Error
			}
			m[k] = v.(Float).Value
		case Int:
			if v.(Int).Error != nil {
				return nil, v.(Int).Error
			}
			m[k] = v.(Int).Value
		case Interface:
			if v.(Interface).Error != nil {
				return nil, v.(Interface).Error
			}
			m[k] = v.(Interface).Value
		case String:
			if v.(String).Error != nil {
				return nil, v.(String).Error
			}
			m[k] = v.(String).Value
		case Array:
			if v.(Array).Error != nil {
				return nil, v.(Array).Error
			}
			expanded, error := ExpandArray(v.(Array).Value)
			if error != nil {
				return nil, error
			}
			m[k] = expanded
		case Object:
			if v.(Object).Error != nil {
				return nil, v.(Object).Error
			}
			expanded, error := ExpandObject(v.(Object).Value)
			if error != nil {
				return nil, error
			}
			m[k] = expanded
		case []interface{}:
			expanded, error := ExpandArray(v.([]interface{}))
			if error != nil {
				return nil, error
			}
			m[k] = expanded
		case map[string]interface{}:
			expanded, error := ExpandObject(v.(map[string]interface{}))
			if error != nil {
				return nil, error
			}
			m[k] = expanded
		default:
			m[k] = v
		}
	}
	return m, nil
}
