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
	"strings"

	"github.com/cloudboss/unobin/pkg/types"
)

func FilterEmpty(items []string) []string {
	nonEmpty := []string{}
	for _, item := range items {
		if item != "" {
			nonEmpty = append(nonEmpty, item)
		}
	}
	return nonEmpty
}

func Any(bools []bool) bool {
	for _, b := range bools {
		if b {
			return b
		}
	}
	return false
}

func All(bools []bool) bool {
	for _, b := range bools {
		if !b {
			return false
		}
	}
	return true
}

func ErrResult(msg, moduleName string) *types.Result {
	return &types.Result{
		Succeeded: false,
		Changed:   false,
		Error:     msg,
		Module:    moduleName,
	}
}

// snakeToThing converts a "snake_case" string to either a "PascalCase" or a "camelCase" string.
// The firstUpper argument determines if the first letter of the returned string is lower case.
func snakeToThing(s string, firstLower bool) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' })
	capitalizedParts := []string{}
	for i, part := range parts {
		var first string
		if i == 0 && firstLower {
			first = strings.ToLower(string(part[0]))
		} else {
			first = strings.ToUpper(string(part[0]))
		}
		rest := ""
		if len(part) >= 2 {
			rest = string(part[1:])
		}
		capitalizedParts = append(capitalizedParts, first+rest)
	}
	return strings.Join(capitalizedParts, "")
}

// SnakeToPascal converts a "snake_case" string to a "PascalCase" string.
func SnakeToPascal(s string) string {
	return snakeToThing(s, false)
}

// SnakeToCamel converts a "snake_case" string to a "camelCase" string.
func SnakeToCamel(s string) string {
	return snakeToThing(s, true)
}
