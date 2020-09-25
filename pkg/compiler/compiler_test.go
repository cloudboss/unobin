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
				"name": "i will fail",
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
				"name": "i will fail",
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
