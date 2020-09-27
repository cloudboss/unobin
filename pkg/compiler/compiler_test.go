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
	"testing"

	"github.com/cloudboss/unobin/pkg/module"
	"github.com/stretchr/testify/assert"
)

func Test_validateTask(t *testing.T) {
	testCases := []struct {
		name         string
		task         map[string]interface{}
		imports      map[string]string
		moduleImport *module.ModuleImport
		err          error
	}{
		{
			"empty task should produce a missing attribute error",
			map[string]interface{}{},
			map[string]string{},
			nil,
			errors.New("missing attribute 'name' from task"),
		},
		{
			"task without matching module alias in imports should produce an unkown module error",
			map[string]interface{}{
				"name":     "i will fail",
				"template": map[interface{}]interface{}{},
			},
			map[string]string{
				"cmd": "github.com/cloudboss/unobin/modules/command.Command",
			},
			nil,
			errors.New("unknown module for task"),
		},
		{
			"task with unknown attribute should produce an unknown attribute error",
			map[string]interface{}{
				"name":        "i will fail",
				"cmd":         map[interface{}]interface{}{},
				"whats this?": "an error",
			},
			map[string]string{
				"cmd": "github.com/cloudboss/unobin/modules/command.Command",
			},
			nil,
			errors.New("unknown attributes defined on task"),
		},
		{
			"task with unknown attribute should produce an error",
			map[string]interface{}{
				"name": "i will fail",
				"cmd":  "i should be a map",
			},
			map[string]string{
				"cmd": "github.com/cloudboss/unobin/modules/command.Command",
			},
			nil,
			errors.New("type of module body should be a map"),
		},
		{
			"task with required attributes should produce a *module.ModuleImport",
			map[string]interface{}{
				"name": "i will succeed",
				"cmd":  map[interface{}]interface{}{},
			},
			map[string]string{
				"cmd": "github.com/cloudboss/unobin/modules/command.Command",
			},
			&module.ModuleImport{
				Alias:               "cmd",
				GoImportPath:        "github.com/cloudboss/unobin/modules/command",
				QualifiedIdentifier: "command.Command",
			},
			nil,
		},
		{
			"task with optional attributes should produce a *module.ModuleImport",
			map[string]interface{}{
				"name": "i will also succeed",
				"cmd":  map[interface{}]interface{}{},
				"when": "when_execute(`true`)",
			},
			map[string]string{
				"cmd": "github.com/cloudboss/unobin/modules/command.Command",
			},
			&module.ModuleImport{
				Alias:               "cmd",
				GoImportPath:        "github.com/cloudboss/unobin/modules/command",
				QualifiedIdentifier: "command.Command",
			},
			nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			moduleImport, err := validateTask(tc.task, tc.imports)
			t.Logf("%+v, %+v\n", moduleImport, err)
			assert.Equal(t, moduleImport, tc.moduleImport)
			if tc.err == nil {
				assert.Nil(t, err, tc.err)
			} else {
				assert.Contains(t, err.Error(), tc.err.Error())
			}
		})
	}
}
