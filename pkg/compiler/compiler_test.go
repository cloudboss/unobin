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
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Load(t *testing.T) {
	testCases := []struct {
		name   string
		file   string
		errStr string
	}{
		{
			"syntatically valid playbook should load",
			"resources/playbook-valid.ub",
			"",
		},
		{
			"syntatically invalid playbook should not load",
			"resources/playbook-invalid.ub",
			"parse error near space (line 5 symbol 9 - line 6 symbol 0)",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			comp := NewCompiler(tc.file)
			err := comp.Load()
			if tc.errStr != "" {
				assert.Contains(t, err.Error(), tc.errStr)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func Test_Validate(t *testing.T) {
	testCases := []struct {
		name   string
		file   string
		errStr string
	}{
		{
			"basic valid playbook should validate",
			"resources/playbook-valid-small.ub",
			"",
		},
		{
			"type of import values must be a string",
			"resources/playbook-invalid-imports.ub",
			"invalid type for import cmd: wanted String but found Array",
		},
		{
			"all required attributes must be present",
			"resources/playbook-invalid-attrs.ub",
			"required attribute imports is not defined",
		},
		{
			"type of input schema must be an object",
			"resources/playbook-invalid-schema-type.ub",
			"invalid type for input-schema: wanted Object but found String",
		},
		{
			"type of input schema properties must be according to spec",
			"resources/playbook-invalid-schema-property.ub",
			"has a primitive type that is NOT VALID -- given: /foolean/",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			comp := NewCompiler(tc.file)
			err := comp.Load()
			assert.Nil(t, err)
			err = comp.Validate()
			if tc.errStr != "" {
				assert.Contains(t, errors.Unwrap(err).Error(), tc.errStr)
			} else {
				fmt.Printf("%s\n", errors.Unwrap(err))
				assert.Nil(t, err)
			}
		})
	}
}

func Test_Compile(t *testing.T) {
	testCases := []struct {
		name   string
		file   string
		errStr string
	}{
		{
			name:   "playbook should compile",
			file:   "resources/playbook-valid-small.ub",
			errStr: "",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			comp := NewCompiler(tc.file)
			err := comp.Load()
			assert.Nil(t, err)
			err = comp.Validate()

			fset := token.NewFileSet()
			astFile := comp.Compile()
			ast.SortImports(fset, astFile)
			format.Node(os.Stdout, fset, astFile)
		})
	}
}
